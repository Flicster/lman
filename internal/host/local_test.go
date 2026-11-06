//go:build !windows

package host

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Flicster/lman/internal/app"
)

func TestLocalGlobInspectRun(t *testing.T) {
	ctx := context.Background()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	bin := filepath.Join(dir, "v1", "bin")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	tool := filepath.Join(bin, "tool")
	require.NoError(t, os.WriteFile(tool, []byte("#!/bin/sh\necho \"tool $1\"\n"), 0o755))
	link := filepath.Join(dir, "tool")
	require.NoError(t, os.Symlink(tool, link))

	l := NewLocal()

	found, err := l.Glob(ctx, []string{dir + "/*/bin/tool", dir + "/missing*"})
	require.NoError(t, err)
	assert.Equal(t, [][]string{{tool}, nil}, found)

	infos, err := l.Inspect(ctx, []string{link, filepath.Join(dir, "gone"), bin})
	require.NoError(t, err)
	require.Len(t, infos, 1, "missing files and directories are left out")
	assert.Equal(t, link, infos[0].Path)
	assert.Equal(t, tool, infos[0].RealPath)
	assert.False(t, infos[0].InstalledAt.IsZero())

	out, err := l.Run(ctx, app.Command{Path: link, Args: []string{"--version"}})
	require.NoError(t, err)
	assert.Equal(t, "tool --version\n", string(out))
}

func TestLocalFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l := NewLocal()

	_, err := l.ReadFile(ctx, dir+"/none")
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, l.Remove(ctx, dir+"/none"), "removing a missing file succeeds")

	require.NoError(t, l.Symlink(ctx, "/a", dir+"/link"))
	require.NoError(t, l.Symlink(ctx, "/b", dir+"/link"), "an existing link is replaced")
	target, err := os.Readlink(dir + "/link")
	require.NoError(t, err)
	assert.Equal(t, "/b", target)
}

func TestWriteFileKeepsModeAndFollowsSymlink(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l := NewLocal()
	require.NoError(t, l.MkdirAll(ctx, dir+"/lman"))
	st, err := os.Stat(dir + "/lman")
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o700), st.Mode().Perm())

	profile := dir + "/profile"
	require.NoError(t, os.WriteFile(profile, []byte("old"), 0o600))
	link := dir + "/.zshrc"
	require.NoError(t, os.Symlink(profile, link))

	require.NoError(t, l.WriteFile(ctx, link, []byte("new"), 0o644))

	got, err := os.ReadFile(profile)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
	st, err = os.Stat(profile)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), st.Mode().Perm())
	linked, err := os.Readlink(link)
	require.NoError(t, err)
	assert.Equal(t, profile, linked, "the symlink is not replaced by a regular file")
}

func TestEnsureBlockCreatesAPrivateProfile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), ".zshrc")
	err := NewLocal().EnsureBlock(ctx, filepath.ToSlash(path), "# >>> lman >>>", []byte("\n# >>> lman >>>\n"))
	require.NoError(t, err)
	st, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), st.Mode().Perm())

	err = NewLocal().EnsureBlock(ctx, "home/u/.zshrc", "m", []byte("x"))
	require.Error(t, err)
	err = NewLocal().EnsureBlock(ctx, filepath.ToSlash(filepath.Dir(path))+"/../x", "m", []byte("x"))
	require.Error(t, err)
}

func TestInspectSkipsAPathThatCannotBeResolved(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	require.NoError(t, os.Symlink("b", filepath.Join(dir, "a")))
	require.NoError(t, os.Symlink("a", filepath.Join(dir, "b")))
	good := filepath.Join(dir, "tool")
	require.NoError(t, os.WriteFile(good, []byte("x"), 0o755))

	infos, err := NewLocal().Inspect(ctx, []string{filepath.ToSlash(filepath.Join(dir, "a")), filepath.ToSlash(good)})
	require.NoError(t, err)
	require.Len(t, infos, 1, "a symlink loop must not fail the scan")
	assert.Equal(t, filepath.ToSlash(good), infos[0].Path)
}

func TestRunDoesNotUseTheProcessDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o644))
	t.Chdir(dir)

	out, err := NewLocal().Run(context.Background(), app.Command{
		Path: "/bin/sh",
		Args: []string{"-c", "test ! -f marker && pwd"},
	})
	require.NoError(t, err)
	assert.Equal(t, "/\n", string(out))
}

func TestRunRejectsAPathThatIsNotAbsolute(t *testing.T) {
	_, err := NewLocal().Run(context.Background(), app.Command{Path: "dir/tool"})
	require.Error(t, err)
	_, err = NewLocal().Run(context.Background(), app.Command{Path: "/bin/echo", Args: []string{"a\x00b"}})
	require.Error(t, err)
}

func TestLoginEnvIgnoresARelativeShell(t *testing.T) {
	t.Setenv("SHELL", "sh")
	env, err := loginEnv(context.Background())
	require.NoError(t, err)
	assert.Equal(t, os.Getenv("PATH"), env["PATH"])
}

func TestLoginEnvIgnoresAShellPathWithANewline(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh\nignored")
	env, err := loginEnv(context.Background())
	require.NoError(t, err)
	assert.Equal(t, os.Getenv("PATH"), env["PATH"])
}

func TestRunStopsDescendants(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	script := "sleep 30 & echo $! > '" + strings.ReplaceAll(pidFile, "'", `'\''`) + "'\nwait\n"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := NewLocal().Run(ctx, app.Command{Path: "/bin/sh", Args: []string{"-c", script}})
	require.Error(t, err)

	raw, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("child %d is still running", pid)
}
