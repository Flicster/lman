package shellenv

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Flicster/lman/internal/domain"
)

func newTestUnix(t *testing.T, files Files, env map[string]string, p domain.Platform) *Unix {
	t.Helper()
	u, err := NewUnix(files, env, p)
	require.NoError(t, err)
	return u
}

func TestUnixLinkWritesShimsEnvAndProfileOnce(t *testing.T) {
	ctx := context.Background()
	files := newMemFiles()
	files.data["/home/u/.zshrc"] = "export PATH=/opt/bin:$PATH"
	u := newTestUnix(t, files, map[string]string{"HOME": "/home/u", "SHELL": "/bin/zsh"}, domain.Linux)

	require.NoError(t, u.Link(ctx, map[string]string{"go": "/sdk/go1.24.0/bin/go"}))
	require.NoError(t, u.Link(ctx, map[string]string{"gofmt": "/sdk/go1.24.0/bin/gofmt"}))

	assert.Equal(t, map[string]string{
		"/home/u/.lman/bin/go":    "/sdk/go1.24.0/bin/go",
		"/home/u/.lman/bin/gofmt": "/sdk/go1.24.0/bin/gofmt",
	}, files.links)
	links, err := u.Links(ctx)
	require.NoError(t, err)
	assert.Len(t, links, 2)

	profile := files.data["/home/u/.zshrc"]
	assert.True(t, strings.HasPrefix(profile, "export PATH=/opt/bin:$PATH\n"), "existing content is kept")
	assert.Equal(t, 1, strings.Count(profile, blockBegin), "the block is added once")
	assert.Contains(t, profile, ". '/home/u/.lman/env.sh'")
	assert.Contains(t, files.data["/home/u/.lman/env.sh"], "export PATH='/home/u/.lman/bin':\"$PATH\"")
	assert.Equal(t, fs.FileMode(0o600), files.modes["/home/u/.lman/env.sh"])
	assert.Equal(t, fs.FileMode(0o600), files.modes["/home/u/.lman/state.json"])
}

func TestUnixUnlink(t *testing.T) {
	ctx := context.Background()
	files := newMemFiles()
	u := newTestUnix(t, files, map[string]string{"HOME": "/home/u", "SHELL": "/bin/bash"}, domain.Linux)
	require.NoError(t, u.Link(ctx, map[string]string{"node": "/n/bin/node", "npm": "/n/bin/npm"}))

	require.NoError(t, u.Unlink(ctx, []string{"npm"}))

	assert.Equal(t, map[string]string{"/home/u/.lman/bin/node": "/n/bin/node"}, files.links)
	links, err := u.Links(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"node": "/n/bin/node"}, links)
	assert.Contains(t, files.data, "/home/u/.bashrc")
}

func TestUnixSetVarQuotesAndUnsets(t *testing.T) {
	ctx := context.Background()
	files := newMemFiles()
	u := newTestUnix(t, files, map[string]string{"HOME": "/home/u"}, domain.Darwin)

	require.NoError(t, u.SetVar(ctx, "GOPATH", "-tags=it's"))
	assert.Contains(t, files.data["/home/u/.lman/env.sh"], `export GOPATH='-tags=it'\''s'`)
	assert.Contains(t, files.data, "/home/u/.profile", "unknown shells read .profile")

	require.NoError(t, u.SetVar(ctx, "GOPATH", ""))
	assert.NotContains(t, files.data["/home/u/.lman/env.sh"], "GOPATH")
	vars, err := u.Vars(ctx)
	require.NoError(t, err)
	assert.Empty(t, vars)
}

func TestShimFileKeepsTheTargetExtension(t *testing.T) {
	assert.Equal(t, "go.exe", shimFile("go", `C:\Go\bin\go.exe`))
	assert.Equal(t, "npm.cmd", shimFile("npm", `C:\nodejs\npm.cmd`))
	assert.Equal(t, "python.exe", shimFile("python", "/opt/python"))
}

func TestProfiles(t *testing.T) {
	assert.Equal(t, []string{"/h/.zshrc"}, profiles("/h", "zsh", domain.Darwin))
	assert.Equal(t, []string{"/h/.bashrc", "/h/.bash_profile"}, profiles("/h", "bash", domain.Darwin))
	assert.Equal(t, []string{"/h/.bashrc"}, profiles("/h", "bash", domain.Linux))
	assert.Equal(t, []string{"/h/.profile"}, profiles("/h", "sh", domain.Linux))
}

func TestUnixReportsBrokenState(t *testing.T) {
	files := newMemFiles()
	files.data["/home/u/.lman/state.json"] = "{"
	u := newTestUnix(t, files, map[string]string{"HOME": "/home/u"}, domain.Linux)

	_, err := u.Links(context.Background())
	require.ErrorContains(t, err, "decode state")
}

// memFiles is an in-memory file system with symlinks.
type memFiles struct {
	data   map[string]string
	links  map[string]string
	dirs   map[string]bool
	modes  map[string]fs.FileMode
	locked []string
}

func newMemFiles() *memFiles {
	return &memFiles{
		data: map[string]string{}, links: map[string]string{}, dirs: map[string]bool{},
		modes: map[string]fs.FileMode{},
	}
}

func (m *memFiles) ReadFile(_ context.Context, name string) ([]byte, error) {
	data, ok := m.data[name]
	if !ok {
		return nil, fmt.Errorf("read %s: %w", name, fs.ErrNotExist)
	}
	return []byte(data), nil
}

func (m *memFiles) WriteFile(_ context.Context, name string, data []byte, perm fs.FileMode) error {
	m.data[name] = string(data)
	m.modes[name] = perm.Perm()
	return nil
}

func (m *memFiles) MkdirAll(_ context.Context, dir string) error {
	m.dirs[dir] = true
	return nil
}

func (m *memFiles) Symlink(_ context.Context, target, link string) error {
	m.links[link] = target
	return nil
}

func (m *memFiles) Remove(_ context.Context, name string) error {
	delete(m.links, name)
	delete(m.data, name)
	return nil
}

func (m *memFiles) Lock(_ context.Context, name string) (func() error, error) {
	m.locked = append(m.locked, name)
	return func() error { return nil }, nil
}

func (m *memFiles) EnsureBlock(_ context.Context, name, marker string, block []byte) error {
	data, exists := m.data[name]
	if strings.Contains(data, marker) {
		return nil
	}
	if data != "" && !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	m.data[name] = data + string(block)
	if !exists {
		m.modes[name] = 0o600
	}
	return nil
}

func TestUnixChangesTakeTheStateLock(t *testing.T) {
	ctx := context.Background()
	files := newMemFiles()
	u := newTestUnix(t, files, map[string]string{"HOME": "/home/u", "SHELL": "/bin/zsh"}, domain.Linux)

	require.NoError(t, u.SetVar(ctx, "GOPATH", "-x"))
	require.NoError(t, u.Link(ctx, map[string]string{"go": "/sdk/go/bin/go"}))
	require.NoError(t, u.Unlink(ctx, []string{"go"}))

	assert.Equal(t, []string{"/home/u/.lman/state.lock", "/home/u/.lman/state.lock", "/home/u/.lman/state.lock"}, files.locked)
}

func TestUnixUnlinkRejectsANameThatLeavesTheShimDir(t *testing.T) {
	ctx := context.Background()
	files := newMemFiles()
	u := newTestUnix(t, files, map[string]string{"HOME": "/home/u", "SHELL": "/bin/zsh"}, domain.Linux)
	require.NoError(t, u.Link(ctx, map[string]string{"go": "/sdk/go/bin/go"}))

	err := u.Unlink(ctx, []string{"../../.zshrc"})

	require.ErrorContains(t, err, "invalid tool name")
	_, gone := files.links["/home/u/.lman/bin/go"]
	assert.True(t, gone, "a rejected name does not unlink the real shims")
}

func TestUnixScriptSkipsLoaderVariables(t *testing.T) {
	u := newTestUnix(t, newMemFiles(), map[string]string{"HOME": "/home/u"}, domain.Linux)
	script := u.script(state{
		Links: map[string]string{},
		Vars: map[string]string{
			"GOPATH":      "/go",
			"LD_PRELOAD":  "/tmp/evil.so",
			"DYLD_INSERT": "/tmp/evil.dylib",
			"BASH_ENV":    "/tmp/evil.sh",
			"GOFLAGS":     "-toolexec=evil",
			"GOROOT":      "/tmp/evil-root",
			"GOENV":       "/tmp/evil.env",
			"PYTHONPATH":  "/tmp/evil",
			"PATH":        "/tmp/evil",
			"bad name":    "x",
		},
	})

	assert.Contains(t, script, "export GOPATH='/go'")
	assert.NotContains(t, script, "LD_PRELOAD")
	assert.NotContains(t, script, "DYLD_")
	assert.NotContains(t, script, "BASH_ENV")
	assert.NotContains(t, script, "GOFLAGS")
	assert.NotContains(t, script, "GOROOT")
	assert.NotContains(t, script, "GOENV")
	assert.NotContains(t, script, "PYTHONPATH")
	assert.NotContains(t, script, "/tmp/evil")
	assert.NotContains(t, script, "bad name")
}

func TestUnixRejectsARelativeHome(t *testing.T) {
	_, err := NewUnix(newMemFiles(), map[string]string{"HOME": ""}, domain.Linux)
	require.ErrorContains(t, err, "home directory")
	_, err = NewUnix(newMemFiles(), map[string]string{"HOME": "home/u"}, domain.Linux)
	require.ErrorContains(t, err, "home directory")
	_, err = NewUnix(newMemFiles(), map[string]string{"HOME": "/tmp/../etc"}, domain.Linux)
	require.ErrorContains(t, err, "home directory")
}
