package app

import (
	"context"
	"errors"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"lman/internal/domain"
)

const (
	home    = "/home/u"
	shimDir = home + "/.lman/bin"
	stable  = home + "/.rustup/toolchains/stable-x86_64-unknown-linux-gnu/bin/rustc"
	nightly = home + "/.rustup/toolchains/nightly-x86_64-unknown-linux-gnu/bin/rustc"
)

type ServiceSuite struct {
	suite.Suite
	ctx  context.Context
	host *fakeHost
	env  *fakeEnv
	svc  *Service
}

func TestService(t *testing.T) {
	suite.Run(t, new(ServiceSuite))
}

// SetupTest builds a Linux host with two Go versions, apt Python and two rustup toolchains.
func (s *ServiceSuite) SetupTest() {
	s.ctx = context.Background()
	s.host = newFakeHost(domain.Linux, map[string]string{
		"HOME": home,
		"PATH": shimDir + ":" + home + "/.cargo/bin:/usr/local/go/bin:/usr/bin",
	})
	s.host.executable("/usr/local/go/bin/go", "", "go version go1.25.1 linux/amd64")
	s.host.executable("/usr/local/go/bin/gofmt", "", "")
	s.host.executable(home+"/sdk/go1.24.0/bin/go", "", "go version go1.24.0 linux/amd64")
	s.host.executable(home+"/sdk/go1.24.0/bin/gofmt", "", "")
	s.host.executable("/usr/bin/python3", "/usr/bin/python3.12", "Python 3.12.3")
	s.host.executable("/usr/bin/python3.12", "", "Python 3.12.3")
	s.host.executable(home+"/.cargo/bin/rustc", home+"/.cargo/bin/rustup", "rustc 1.80.0 (051478957 2024-07-21)")
	s.host.executable(stable, "", "rustc 1.80.0 (051478957 2024-07-21)")
	s.host.executable(nightly, "", "rustc 1.82.0-nightly (2024-08-01)")
	s.host.output("dpkg -S /usr/bin/python3.12", "python3.12-minimal: /usr/bin/python3.12\n")
	s.env = newFakeEnv(s.host)
	s.svc = NewService(s.host, s.env)
}

func (s *ServiceSuite) TestLanguagesDetectsVersionsAndActive() {
	langs, err := s.svc.Languages(s.ctx)
	s.Require().NoError(err)

	s.Require().Equal([]domain.LanguageID{"go", "python", "rust"}, ids(langs))

	golang := langs[0]
	s.Require().Len(golang.Versions, 2)
	s.Equal("1.25.1", golang.Versions[0].Number, "newest first")
	s.True(golang.Versions[0].Active, "first go in PATH")
	s.Equal("1.24.0", golang.Versions[1].Number)
	s.Equal(home+"/sdk/go1.24.0", golang.Versions[1].Method.Root)
	s.Equal(s.host.created, golang.Versions[0].InstalledAt)

	python := langs[1]
	s.Require().Len(python.Versions, 1, "python3 and python3.12 are one file")
	s.Equal("/usr/bin/python3", python.Versions[0].Path, "the PATH name is kept")
	s.Equal("apt", python.Versions[0].Method.Manager)
	s.Equal("python3.12-minimal", python.Versions[0].Method.Package)

	rust := langs[2]
	s.Require().Len(rust.Versions, 2, "the cargo proxy is not a version")
	active, ok := rust.Active()
	s.Require().True(ok)
	s.Equal(stable, active.Path, "the proxy answers with the stable version")
}

func (s *ServiceSuite) TestLanguagesAreCachedUntilRefresh() {
	_, err := s.svc.Languages(s.ctx)
	s.Require().NoError(err)
	runs := s.host.runCount()

	_, err = s.svc.Languages(s.ctx)
	s.Require().NoError(err)
	s.Equal(runs, s.host.runCount())

	_, err = s.svc.Refresh(s.ctx)
	s.Require().NoError(err)
	s.Greater(s.host.runCount(), runs)
}

func (s *ServiceSuite) TestLanguageErrors() {
	_, err := s.svc.Language(s.ctx, "cobol")
	s.Require().ErrorIs(err, domain.ErrUnknownLanguage)

	_, err = s.svc.Language(s.ctx, "node")
	s.Require().ErrorIs(err, domain.ErrNotInstalled)

	l, err := s.svc.Language(s.ctx, "Golang")
	s.Require().NoError(err)
	s.Equal(domain.LanguageID("go"), l.ID)
}

func (s *ServiceSuite) TestUseLinksToolsAndActivates() {
	act, err := s.svc.Use(s.ctx, "go", "go1.24")
	s.Require().NoError(err)

	s.Empty(act.ShadowedBy)
	s.Equal("1.24.0", act.Version.Number)
	s.True(act.Version.Active)
	s.Equal(map[string]string{
		"go":    home + "/sdk/go1.24.0/bin/go",
		"gofmt": home + "/sdk/go1.24.0/bin/gofmt",
	}, s.env.links)

	l, err := s.svc.Language(s.ctx, "go")
	s.Require().NoError(err)
	s.Len(l.Versions, 2, "the shim is not listed as a version")
}

func (s *ServiceSuite) TestUseRemovesStaleLinks() {
	s.env.links["gofmt"] = "/usr/local/go/bin/gofmt"
	s.host.executable(home+"/sdk/go1.23.0/bin/go", "", "go version go1.23.0 linux/amd64")

	_, err := s.svc.Use(s.ctx, "go", "1.23.0")
	s.Require().NoError(err)

	s.Equal(map[string]string{"go": home + "/sdk/go1.23.0/bin/go"}, s.env.links)
}

func (s *ServiceSuite) TestUseReportsShadowingPathEntry() {
	s.host.env["PATH"] = "/usr/local/go/bin:" + shimDir

	act, err := s.svc.Use(s.ctx, "go", "1.24.0")
	s.Require().NoError(err)

	s.Equal("/usr/local/go/bin/go", act.ShadowedBy)
}

func (s *ServiceSuite) TestUseReportsShimDirectoryOutsidePath() {
	s.host.env["PATH"] = "/usr/bin"

	act, err := s.svc.Use(s.ctx, "go", "1.24.0")
	s.Require().NoError(err)

	s.True(act.NotInPath)
	s.Empty(act.ShadowedBy)
}

func (s *ServiceSuite) TestUseUnknownVersion() {
	_, err := s.svc.Use(s.ctx, "go", "1.19")
	s.Require().ErrorIs(err, domain.ErrVersionNotFound)
	s.Empty(s.env.links)
}

func (s *ServiceSuite) TestRemoveVersionRefusesActive() {
	err := s.svc.RemoveVersion(s.ctx, "go", "1.25.1")
	s.Require().ErrorIs(err, domain.ErrActiveVersion)
	s.Empty(s.host.removed)
}

func (s *ServiceSuite) TestRemoveVersionDeletesHomeDirectory() {
	s.env.links["go"] = home + "/sdk/go1.24.0/bin/go-old"

	err := s.svc.RemoveVersion(s.ctx, "go", "1.24.0")
	s.Require().NoError(err)

	s.Equal([]string{home + "/sdk/go1.24.0"}, s.host.removed)
	s.NotContains(s.env.links, "go", "links into the removed version are dropped")
}

func (s *ServiceSuite) TestRemoveVersionRunsManagerCommand() {
	s.host.output(home+"/.cargo/bin/rustup toolchain uninstall nightly-x86_64-unknown-linux-gnu", "removed")

	err := s.svc.RemoveVersion(s.ctx, "rust", nightly)
	s.Require().NoError(err)

	s.Contains(s.host.commands(), home+"/.cargo/bin/rustup toolchain uninstall nightly-x86_64-unknown-linux-gnu")
}

func (s *ServiceSuite) TestRemoveVersionReportsCommandFailure() {
	err := s.svc.RemoveVersion(s.ctx, "rust", nightly)
	s.Require().Error(err)
	s.Contains(err.Error(), "toolchain uninstall")
}

func (s *ServiceSuite) TestRemoveLanguageJoinsManualRemovals() {
	s.env.links["python3"] = "/usr/bin/python3.12"

	err := s.svc.RemoveLanguage(s.ctx, "python")

	var manual *domain.ManualRemovalError
	s.Require().ErrorAs(err, &manual)
	s.Equal("sudo apt remove python3.12-minimal", manual.Hint)
	s.Empty(s.env.links, "shims are dropped even when a version stays")
}

func (s *ServiceSuite) TestRemoveLanguageRemovesEveryVersion() {
	err := s.svc.RemoveLanguage(s.ctx, "go")

	var manual *domain.ManualRemovalError
	s.Require().ErrorAs(err, &manual, "/usr/local/go needs sudo")
	s.Equal("sudo rm -rf '/usr/local/go'", manual.Hint)
	s.Equal([]string{home + "/sdk/go1.24.0"}, s.host.removed)
}

func (s *ServiceSuite) TestSetEnvStoresLanguageVariable() {
	s.host.env["GOPATH"] = home + "/go"

	vars, err := s.svc.Env(s.ctx, "go")
	s.Require().NoError(err)
	s.Equal(domain.EnvVar{Name: "GOPATH", Value: home + "/go"}, vars[1])

	s.Require().NoError(s.svc.SetEnv(s.ctx, "go", "GOPATH", "/data/go"))

	vars, err = s.svc.Env(s.ctx, "go")
	s.Require().NoError(err)
	s.Equal(domain.EnvVar{Name: "GOPATH", Value: "/data/go", Managed: true}, vars[1])
}

func (s *ServiceSuite) TestGoEnvUsesToolchainValues() {
	s.host.env["GOPATH"] = home + "/go"
	s.host.output("/usr/local/go/bin/go env -json", `{
		"GOARCH": "amd64",
		"GOBIN": "",
		"GOPATH": "`+home+`/go",
		"GOPROXY": "https://proxy.golang.org,direct",
		"GOROOT": "/usr/local/go"
	}`)

	vars, err := s.svc.Env(s.ctx, "go")
	s.Require().NoError(err)
	got := map[string]domain.EnvVar{}
	for _, v := range vars {
		got[v.Name] = v
	}
	s.Equal("/usr/local/go", got["GOROOT"].Value)
	s.True(got["GOROOT"].Default, "Go fills GOROOT when it is not exported")
	s.Equal(home+"/go", got["GOPATH"].Value)
	s.False(got["GOPATH"].Default, "GOPATH is exported in the terminal")
	s.Equal("https://proxy.golang.org,direct", got["GOPROXY"].Value)
	s.True(got["GOPROXY"].Default)
	s.Empty(got["GOBIN"].Value)
	s.Contains(slices.Collect(maps.Keys(got)), "GOARCH")

	s.Require().NoError(s.svc.SetEnv(s.ctx, "go", "GOARCH", "arm64"))
	s.Equal("arm64", s.env.vars["GOARCH"])
}

func (s *ServiceSuite) TestSetEnvRejectsInvalidNames() {
	s.Require().ErrorIs(s.svc.SetEnv(s.ctx, "go", "PATH", "/x"), domain.ErrForeignVar)
	s.Require().ErrorIs(s.svc.SetEnv(s.ctx, "go", "GO PATH", "/x"), domain.ErrInvalidVarName)
	s.Empty(s.env.vars)
}

func (s *ServiceSuite) TestManualUsesActiveVersion() {
	s.host.output("/usr/local/go/bin/go help", "Go is a tool for managing Go source code.")

	m, err := s.svc.Manual(s.ctx, "go")
	s.Require().NoError(err)

	s.Equal("Go is a tool for managing Go source code.", m.Text)
	s.Equal("https://go.dev/doc/", m.DocsURL)
}

func (s *ServiceSuite) TestScanFailsWithoutEnvironment() {
	s.host.envErr = errors.New("ssh: connection refused")

	_, err := s.svc.Languages(s.ctx)
	s.Require().ErrorContains(err, "read environment: ssh: connection refused")
}

func TestDarwinStubIsNeverExecuted(t *testing.T) {
	h := newFakeHost(domain.Darwin, map[string]string{"HOME": "/Users/u", "PATH": "/usr/bin"})
	h.executable("/usr/bin/java", "", "")
	jdk := "/Library/Java/JavaVirtualMachines/temurin-21.jdk/Contents/Home/bin/java"
	h.executable(jdk, "", `openjdk version "21.0.2" 2024-01-16`)
	svc := NewService(h, newFakeEnv(h))

	langs, err := svc.Languages(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(langs) != 1 || len(langs[0].Versions) != 1 || langs[0].Versions[0].Path != jdk {
		t.Fatalf("want only the JDK, got %+v", langs)
	}
	if langs[0].Versions[0].Active {
		t.Fatal("the stub comes first in PATH, so no version runs in the terminal")
	}
	for _, c := range h.commands() {
		if strings.HasPrefix(c, "/usr/bin/java") {
			t.Fatalf("the stub was executed: %s", c)
		}
	}
}

func ids(langs []domain.Language) []domain.LanguageID {
	out := make([]domain.LanguageID, 0, len(langs))
	for _, l := range langs {
		out = append(out, l.ID)
	}
	return out
}

// fakeHost is an in-memory host: executables with resolved paths and canned outputs.
type fakeHost struct {
	platform domain.Platform
	env      map[string]string
	envErr   error
	created  time.Time

	mu      sync.Mutex
	real    map[string]string
	outputs map[string]string
	runs    []string
	removed []string
}

func newFakeHost(p domain.Platform, env map[string]string) *fakeHost {
	return &fakeHost{
		platform: p,
		env:      env,
		created:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		real:     map[string]string{},
		outputs:  map[string]string{},
	}
}

// executable adds file resolving to real (itself when empty) that prints version for any version flag.
func (h *fakeHost) executable(file, real, version string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if real == "" {
		real = file
	}
	h.real[file] = real
	if real != file {
		h.real[real] = real
	}
	if version != "" {
		for _, flag := range []string{"version", "--version", "-version"} {
			h.outputs[file+" "+flag] = version
		}
	}
}

func (h *fakeHost) output(command, out string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.outputs[command] = out
}

func (h *fakeHost) runCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.runs)
}

func (h *fakeHost) commands() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.runs)
}

func (h *fakeHost) Name() string              { return "test" }
func (h *fakeHost) Platform() domain.Platform { return h.platform }
func (h *fakeHost) RemoveAll(_ context.Context, dir string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removed = append(h.removed, dir)
	return nil
}

func (h *fakeHost) Env(context.Context) (map[string]string, error) {
	if h.envErr != nil {
		return nil, h.envErr
	}
	return maps.Clone(h.env), nil
}

func (h *fakeHost) Glob(_ context.Context, patterns []string) ([][]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	files := slices.Sorted(maps.Keys(h.real))
	out := make([][]string, len(patterns))
	for i, p := range patterns {
		for _, f := range files {
			if ok, err := path.Match(p, f); err == nil && ok {
				out[i] = append(out[i], f)
			}
		}
	}
	return out, nil
}

func (h *fakeHost) Inspect(_ context.Context, paths []string) ([]FileInfo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []FileInfo
	for _, p := range paths {
		if real, ok := h.real[p]; ok {
			out = append(out, FileInfo{Path: p, RealPath: real, InstalledAt: h.created})
		}
	}
	return out, nil
}

func (h *fakeHost) Run(_ context.Context, c Command) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	command := strings.Join(append([]string{c.Path}, c.Args...), " ")
	h.runs = append(h.runs, command)
	out, ok := h.outputs[command]
	if !ok {
		return nil, errors.New("exit status 1")
	}
	return []byte(out), nil
}

// fakeEnv keeps links and variables in memory. Links appear as symlinks on the host,
// and variables reach the environment of new terminals.
type fakeEnv struct {
	host  *fakeHost
	links map[string]string
	vars  map[string]string
}

func newFakeEnv(h *fakeHost) *fakeEnv {
	return &fakeEnv{host: h, links: map[string]string{}, vars: map[string]string{}}
}

func (e *fakeEnv) ShimDir() string { return shimDir }

func (e *fakeEnv) Links(context.Context) (map[string]string, error) {
	return maps.Clone(e.links), nil
}

func (e *fakeEnv) Link(_ context.Context, links map[string]string) error {
	e.host.mu.Lock()
	defer e.host.mu.Unlock()
	for name, target := range links {
		e.links[name] = target
		e.host.real[shimDir+"/"+name] = e.host.real[target]
	}
	return nil
}

func (e *fakeEnv) Unlink(_ context.Context, names []string) error {
	e.host.mu.Lock()
	defer e.host.mu.Unlock()
	for _, name := range names {
		delete(e.links, name)
		delete(e.host.real, shimDir+"/"+name)
	}
	return nil
}

func (e *fakeEnv) Vars(context.Context) (map[string]string, error) {
	return maps.Clone(e.vars), nil
}

func (e *fakeEnv) SetVar(_ context.Context, name, value string) error {
	if value == "" {
		delete(e.vars, name)
		return nil
	}
	e.vars[name] = value
	return nil
}
