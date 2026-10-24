package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"lman/internal/app"
	"lman/internal/domain"
)

func TestListShowsActiveVersions(t *testing.T) {
	r := newRunner(t, "")

	code := r.run("list")

	require.Equal(t, exitOK, code, r.errOut.String())
	assert.Contains(t, r.out.String(), "Languages on local:")
	assert.Regexp(t, `Go\s+go\s+go1\.25\.1\s+2`, r.out.String())
	assert.Equal(t, []string{""}, r.servers, "no server means the local computer")
	assert.True(t, r.closed)
}

func TestKnownListsTheCatalog(t *testing.T) {
	r := newRunner(t, "")

	require.Equal(t, exitOK, r.run("known"), r.errOut.String())

	assert.Contains(t, r.out.String(), "C++")
	assert.Contains(t, r.out.String(), "csharp")
	assert.Contains(t, r.out.String(), "Visual Basic")
	assert.Empty(t, r.servers, "the catalog does not depend on a computer")
}

func TestNoArgumentsLists(t *testing.T) {
	r := newRunner(t, "")
	require.Equal(t, exitOK, r.run())
	assert.Contains(t, r.out.String(), "Languages on local:")
}

func TestRemoteOption(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "separate value", args: []string{"--remote", "dev@box", "show", "go"}},
		{name: "joined value", args: []string{"--remote=dev@box", "show", "go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRunner(t, "")
			require.Equal(t, exitOK, r.run(tt.args...), r.errOut.String())
			assert.Equal(t, []string{"dev@box"}, r.servers)
			assert.Contains(t, r.out.String(), "Go on dev@box")
		})
	}
}

func TestUsageErrors(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"--remote"}, want: "--remote needs a server"},
		{args: []string{"--remote", "box", "status"}, want: "status runs on this computer"},
		{args: []string{"fly"}, want: `unknown command "fly"`},
		{args: []string{"show"}, want: "show needs a language"},
		{args: []string{"use", "go"}, want: "use needs a language and a version"},
		{args: []string{"env", "set", "go", "GOPATH"}, want: "env set needs a language, a name and a value"},
		{args: []string{"remove"}, want: "remove needs a language"},
		{args: []string{"theme"}, want: "theme needs light, dark or system"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			r := newRunner(t, "")
			assert.Equal(t, exitUsage, r.run(tt.args...))
			assert.Contains(t, r.errOut.String(), tt.want)
			assert.Contains(t, r.errOut.String(), "Usage: lman")
		})
	}
}

func TestShowPrintsVersionsAndHints(t *testing.T) {
	r := newRunner(t, "")

	require.Equal(t, exitOK, r.run("show", "go"))

	out := r.out.String()
	assert.Regexp(t, `\* go1\.25\.1\s+brew \(go\)\s+2026-01-02\s+/opt/homebrew/bin/go -> /opt/homebrew/Cellar/go/1\.25\.1/libexec/bin/go`, out)
	assert.Regexp(t, `  go1\.24\.0\s+direct\s+-\s+/home/u/sdk/go1\.24\.0/bin/go`, out)
	assert.Contains(t, out, "gofmt")
}

func TestRemoveAsksForConfirmation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		args    []string
		removed []string
		output  string
	}{
		{name: "confirmed", input: "y\n", args: []string{"remove", "go", "1.24"}, removed: []string{"version /home/u/sdk/go1.24.0/bin/go"}, output: "Go go1.24.0 is removed."},
		{name: "declined", input: "n\n", args: []string{"remove", "go", "1.24"}, output: "Cancelled."},
		{name: "no input", input: "", args: []string{"remove", "go", "1.24"}, output: "Cancelled."},
		{name: "yes flag", args: []string{"remove", "-y", "go"}, removed: []string{"language go"}, output: "Go is removed."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRunner(t, tt.input)
			require.Equal(t, exitOK, r.run(tt.args...), r.errOut.String())
			assert.Equal(t, tt.removed, r.manager.removed)
			assert.Contains(t, r.out.String(), tt.output)
		})
	}
}

func TestRemoveActiveVersionFailsWithoutQuestion(t *testing.T) {
	r := newRunner(t, "y\n")

	assert.Equal(t, exitError, r.run("remove", "go", "1.25.1"))

	assert.NotContains(t, r.out.String(), "[y/N]")
	assert.Contains(t, r.errOut.String(), "version is active")
}

func TestUseReportsShadowedActivation(t *testing.T) {
	r := newRunner(t, "")
	r.manager.shadowedBy = "/usr/local/go/bin/go"

	assert.Equal(t, exitError, r.run("use", "go", "1.24"))
	assert.Contains(t, r.errOut.String(), "/usr/local/go/bin/go comes first in PATH")
}

func TestUseAndEnvSet(t *testing.T) {
	r := newRunner(t, "")

	require.Equal(t, exitOK, r.run("use", "go", "1.24"))
	assert.Contains(t, r.out.String(), "Go go1.24.0 is active. Open a new terminal")

	require.Equal(t, exitOK, r.run("env", "set", "go", "GOPATH", "/data/go"))
	assert.Equal(t, map[string]string{"GOPATH": "/data/go"}, r.manager.vars)
}

func TestStatusAndTheme(t *testing.T) {
	r := newRunner(t, "")

	require.Equal(t, exitOK, r.run("status"))
	assert.Regexp(t, `System\s+macOS 15\.6 \(arm64\)`, r.out.String())
	assert.Regexp(t, `Memory\s+16\.0 GiB`, r.out.String())
	assert.Empty(t, r.servers, "status does not connect to a host")

	require.Equal(t, exitOK, r.run("theme", "dark"))
	assert.Equal(t, "dark", r.settings.theme)

	assert.Equal(t, exitError, r.run("theme", "blue"))
	assert.Contains(t, r.errOut.String(), "invalid theme")
}

func TestConnectionErrorIsReported(t *testing.T) {
	r := newRunner(t, "")
	r.openErr = errors.New("connect to box: ssh box: Permission denied (publickey)")

	assert.Equal(t, exitError, r.run("--remote", "box", "list"))
	assert.Equal(t, "lman: connect to box: ssh box: Permission denied (publickey)\n", r.errOut.String())
}

type runner struct {
	t        *testing.T
	input    string
	manager  *stubManager
	settings *stubSettings
	servers  []string
	openErr  error
	closed   bool
	out      bytes.Buffer
	errOut   bytes.Buffer
}

func newRunner(t *testing.T, input string) *runner {
	return &runner{t: t, input: input, manager: newStubManager(), settings: &stubSettings{}}
}

func (r *runner) run(args ...string) int {
	r.out.Reset()
	r.errOut.Reset()
	open := func(_ context.Context, server string) (Manager, func() error, error) {
		r.servers = append(r.servers, server)
		if r.openErr != nil {
			return nil, nil, r.openErr
		}
		r.manager.host = "local"
		if server != "" {
			r.manager.host = server
		}
		return r.manager, func() error { r.closed = true; return nil }, nil
	}
	c := New(open, r.settings, stubStatus{}, strings.NewReader(r.input), &r.out, &r.errOut)
	return c.Run(context.Background(), args)
}

// stubManager serves one Go language with an active brew version and an inactive direct one.
type stubManager struct {
	host       string
	lang       domain.Language
	shadowedBy string
	removed    []string
	vars       map[string]string
}

func newStubManager() *stubManager {
	def := domain.Catalog()[0]
	return &stubManager{
		lang: domain.Language{Definition: def, Versions: []domain.Version{
			{
				Number:      "1.25.1",
				Path:        "/opt/homebrew/bin/go",
				RealPath:    "/opt/homebrew/Cellar/go/1.25.1/libexec/bin/go",
				InstalledAt: time.Date(2026, 1, 2, 12, 0, 0, 0, time.Local),
				Method:      domain.InstallMethod{Kind: domain.MethodManager, Manager: "brew", Package: "go"},
				Active:      true,
			},
			{
				Number:   "1.24.0",
				Path:     "/home/u/sdk/go1.24.0/bin/go",
				RealPath: "/home/u/sdk/go1.24.0/bin/go",
				Method:   domain.InstallMethod{Kind: domain.MethodDirect},
			},
		}},
		vars: map[string]string{},
	}
}

func (m *stubManager) Host() string { return m.host }

func (m *stubManager) Languages(context.Context) ([]domain.Language, error) {
	return []domain.Language{m.lang}, nil
}

func (m *stubManager) Language(_ context.Context, id string) (domain.Language, error) {
	if !m.lang.Matches(id) {
		return domain.Language{}, domain.ErrNotInstalled
	}
	return m.lang, nil
}

func (m *stubManager) Manual(context.Context, string) (domain.Manual, error) {
	return domain.Manual{Text: "Go is a tool.", DocsURL: m.lang.DocsURL}, nil
}

func (m *stubManager) Env(context.Context, string) ([]domain.EnvVar, error) {
	return []domain.EnvVar{{Name: "GOPATH", Value: m.vars["GOPATH"], Managed: m.vars["GOPATH"] != ""}}, nil
}

func (m *stubManager) SetEnv(_ context.Context, _, name, value string) error {
	m.vars[name] = value
	return nil
}

func (m *stubManager) Use(_ context.Context, _, version string) (app.Activation, error) {
	v, err := m.lang.Find(version)
	if err != nil {
		return app.Activation{}, err
	}
	return app.Activation{Version: v, ShadowedBy: m.shadowedBy}, nil
}

func (m *stubManager) RemoveVersion(_ context.Context, _, version string) error {
	v, err := m.lang.Find(version)
	if err != nil {
		return err
	}
	if v.Active {
		return domain.ErrActiveVersion
	}
	m.removed = append(m.removed, "version "+version)
	return nil
}

func (m *stubManager) RemoveLanguage(_ context.Context, id string) error {
	m.removed = append(m.removed, "language "+id)
	return nil
}

type stubSettings struct {
	theme string
}

func (s *stubSettings) SetTheme(_ context.Context, name string) (domain.Theme, error) {
	theme, err := domain.ParseTheme(name)
	if err != nil {
		return "", err
	}
	s.theme = name
	return theme, nil
}

type stubStatus struct{}

func (stubStatus) Get(context.Context) (app.StatusInfo, error) {
	return app.StatusInfo{
		System: app.SystemInfo{
			Hostname: "mbp", OS: "macOS", OSVersion: "15.6", Arch: "arm64",
			CPU: "Apple M2", Cores: 8, MemoryBytes: 16 << 30,
		},
		Version: "dev",
		Time:    time.Date(2026, 10, 24, 9, 0, 0, 0, time.Local),
	}, nil
}
