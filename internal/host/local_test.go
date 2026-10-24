//go:build !windows

package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"lman/internal/app"
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
