package config

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Flicster/lman/internal/app"
	"github.com/Flicster/lman/internal/domain"
)

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := NewStore(filepath.Join(t.TempDir(), "lman", "preferences.json"))

	p, err := s.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, app.Preferences{}, p, "a missing file gives empty preferences")

	want := app.Preferences{Theme: domain.ThemeDark, Servers: []string{"dev@box"}}
	require.NoError(t, s.Save(ctx, want))

	got, err := s.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestStoreReportsBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	require.NoError(t, os.WriteFile(path, []byte("{"), 0o644))

	_, err := NewStore(path).Load(context.Background())
	require.ErrorContains(t, err, "decode")
}

func TestSaveDoesNotFollowATempSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a file symlink needs extra privileges")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "preferences.json")
	outside := filepath.Join(dir, "outside")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	require.NoError(t, os.Symlink(outside, path+".tmp"))

	require.NoError(t, NewStore(path).Save(context.Background(), app.Preferences{Servers: []string{"box"}}))

	got, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, "secret", string(got), "a leftover temporary symlink is not the file that is written")
	stored, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(stored), "box")
}

func TestSettingsUseStore(t *testing.T) {
	ctx := context.Background()
	settings := app.NewSettings(NewStore(filepath.Join(t.TempDir(), "preferences.json")))

	p, err := settings.Preferences(ctx)
	require.NoError(t, err)
	assert.Equal(t, domain.ThemeSystem, p.Theme, "the system theme is the default")

	_, err = settings.SetTheme(ctx, "light")
	require.NoError(t, err)
	for _, server := range []string{"a", "b", "a"} {
		require.NoError(t, settings.RememberServer(ctx, server))
	}

	p, err = settings.Preferences(ctx)
	require.NoError(t, err)
	assert.Equal(t, domain.ThemeLight, p.Theme)
	assert.Equal(t, []string{"a", "b"}, p.Servers, "the last used server comes first, without duplicates")
}
