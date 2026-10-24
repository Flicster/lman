package desktop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"lman/internal/app"
	"lman/internal/domain"
)

func TestConnectSwitchesHostAndClosesPrevious(t *testing.T) {
	o := &opener{}
	a := New(o.open, &stubSettings{}, stubStatus{})

	overview, err := a.Overview()
	require.NoError(t, err)
	assert.Equal(t, "local", overview.Host)
	assert.False(t, overview.Remote)

	overview, err = a.Connect("dev@box")
	require.NoError(t, err)
	assert.Equal(t, "dev@box", overview.Host)
	assert.True(t, overview.Remote)
	assert.Equal(t, []string{"dev@box"}, overview.Servers)
	assert.Equal(t, []string{"local"}, o.closed)

	require.NoError(t, a.close())
	assert.Equal(t, []string{"local", "dev@box"}, o.closed)
}

func TestConnectReturnsToLocal(t *testing.T) {
	o := &opener{}
	a := New(o.open, &stubSettings{}, stubStatus{})

	overview, err := a.Connect("dev@box")
	require.NoError(t, err)
	assert.True(t, overview.Remote)

	o.closeErr = errors.New("control socket")
	overview, err = a.Connect("")
	require.NoError(t, err)
	assert.Equal(t, "local", overview.Host)
	assert.False(t, overview.Remote)
	assert.Equal(t, []string{"dev@box"}, overview.Servers)
	assert.Equal(t, []string{"dev@box"}, o.closed)
}

func TestConnectFailureKeepsCurrentHost(t *testing.T) {
	o := &opener{}
	a := New(o.open, &stubSettings{}, stubStatus{})
	_, err := a.Overview()
	require.NoError(t, err)

	o.err = errors.New("permission denied")
	_, err = a.Connect("box")
	require.ErrorContains(t, err, "permission denied")

	o.err = nil
	overview, err := a.Overview()
	require.NoError(t, err)
	assert.Equal(t, "local", overview.Host)
	assert.Empty(t, o.closed)
}

func TestKnownListsEveryCatalogLanguage(t *testing.T) {
	names := map[string]bool{}
	for _, l := range New((&opener{}).open, &stubSettings{}, stubStatus{}).Known() {
		names[l.Name] = true
		assert.NotEmpty(t, l.ID)
	}
	for _, name := range []string{"Go", "C", "C++", "C#", "Visual Basic"} {
		assert.True(t, names[name], name)
	}
}

func TestLanguageDetail(t *testing.T) {
	a := New((&opener{}).open, &stubSettings{}, stubStatus{})

	cards, err := a.Languages(false)
	require.NoError(t, err)
	assert.Equal(t, []LanguageCard{{ID: "go", Name: "Go", Active: "go1.25.1", Versions: 1}}, cards)

	d, err := a.Language("go")
	require.NoError(t, err)
	require.Len(t, d.Versions, 1)
	assert.Equal(t, VersionDTO{
		Label:     "go1.25.1",
		Path:      "/usr/local/go/bin/go",
		RealPath:  "/usr/local/go/bin/go",
		Method:    "direct",
		Installed: "2026-01-02",
		Active:    true,
	}, d.Versions[0])
	assert.Equal(t, []EnvVarDTO{{Name: "GOPATH", Value: "/go"}}, d.Env)
	assert.NotEmpty(t, d.Hints)
}

type opener struct {
	err      error
	closeErr error
	closed   []string
}

func (o *opener) open(_ context.Context, server string) (Manager, func() error, error) {
	if o.err != nil {
		return nil, nil, o.err
	}
	name := server
	if name == "" {
		name = "local"
	}
	return stubManager{host: name}, func() error {
		o.closed = append(o.closed, name)
		return o.closeErr
	}, nil
}

type stubManager struct {
	host string
}

func (m stubManager) Host() string { return m.host }

func (m stubManager) Languages(context.Context) ([]domain.Language, error) {
	l, err := m.Language(context.Background(), "go")
	return []domain.Language{l}, err
}

func (m stubManager) Refresh(ctx context.Context) ([]domain.Language, error) {
	return m.Languages(ctx)
}

func (m stubManager) Language(context.Context, string) (domain.Language, error) {
	return domain.Language{Definition: domain.Catalog()[0], Versions: []domain.Version{{
		Number:      "1.25.1",
		Path:        "/usr/local/go/bin/go",
		RealPath:    "/usr/local/go/bin/go",
		InstalledAt: time.Date(2026, 1, 2, 3, 0, 0, 0, time.Local),
		Method:      domain.InstallMethod{Kind: domain.MethodDirect},
		Active:      true,
	}}}, nil
}

func (m stubManager) Manual(context.Context, string) (domain.Manual, error) {
	return domain.Manual{}, nil
}

func (m stubManager) Env(context.Context, string) ([]domain.EnvVar, error) {
	return []domain.EnvVar{{Name: "GOPATH", Value: "/go"}}, nil
}

func (m stubManager) SetEnv(context.Context, string, string, string) error { return nil }

func (m stubManager) Use(context.Context, string, string) (app.Activation, error) {
	return app.Activation{}, nil
}

func (m stubManager) RemoveVersion(context.Context, string, string) error { return nil }

func (m stubManager) RemoveLanguage(context.Context, string) error { return nil }

type stubSettings struct {
	servers []string
}

func (s *stubSettings) Preferences(context.Context) (app.Preferences, error) {
	return app.Preferences{Theme: domain.ThemeSystem, Servers: s.servers}, nil
}

func (s *stubSettings) SetTheme(_ context.Context, name string) (domain.Theme, error) {
	return domain.ParseTheme(name)
}

func (s *stubSettings) RememberServer(_ context.Context, server string) error {
	if server != "" {
		s.servers = append([]string{server}, s.servers...)
	}
	return nil
}

type stubStatus struct{}

func (stubStatus) Get(context.Context) (app.StatusInfo, error) {
	return app.StatusInfo{Version: "dev"}, nil
}
