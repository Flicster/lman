package shellenv

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"lman/internal/domain"
)

func TestUnixLinkWritesShimsEnvAndProfileOnce(t *testing.T) {
	ctx := context.Background()
	files := newMemFiles()
	files.data["/home/u/.zshrc"] = "export PATH=/opt/bin:$PATH"
	u := NewUnix(files, map[string]string{"HOME": "/home/u", "SHELL": "/bin/zsh"}, domain.Linux)

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
}

func TestUnixUnlink(t *testing.T) {
	ctx := context.Background()
	files := newMemFiles()
	u := NewUnix(files, map[string]string{"HOME": "/home/u", "SHELL": "/bin/bash"}, domain.Linux)
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
	u := NewUnix(files, map[string]string{"HOME": "/home/u"}, domain.Darwin)

	require.NoError(t, u.SetVar(ctx, "GOFLAGS", "-tags=it's"))
	assert.Contains(t, files.data["/home/u/.lman/env.sh"], `export GOFLAGS='-tags=it'\''s'`)
	assert.Contains(t, files.data, "/home/u/.profile", "unknown shells read .profile")

	require.NoError(t, u.SetVar(ctx, "GOFLAGS", ""))
	assert.NotContains(t, files.data["/home/u/.lman/env.sh"], "GOFLAGS")
	vars, err := u.Vars(ctx)
	require.NoError(t, err)
	assert.Empty(t, vars)
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
	u := NewUnix(files, map[string]string{"HOME": "/home/u"}, domain.Linux)

	_, err := u.Links(context.Background())
	require.ErrorContains(t, err, "decode state")
}

// memFiles is an in-memory file system with symlinks.
type memFiles struct {
	data  map[string]string
	links map[string]string
	dirs  map[string]bool
}

func newMemFiles() *memFiles {
	return &memFiles{data: map[string]string{}, links: map[string]string{}, dirs: map[string]bool{}}
}

func (m *memFiles) ReadFile(_ context.Context, name string) ([]byte, error) {
	data, ok := m.data[name]
	if !ok {
		return nil, fmt.Errorf("read %s: %w", name, fs.ErrNotExist)
	}
	return []byte(data), nil
}

func (m *memFiles) WriteFile(_ context.Context, name string, data []byte, _ fs.FileMode) error {
	m.data[name] = string(data)
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
