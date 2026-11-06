package app

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Flicster/lman/internal/domain"
)

// probeTimeout bounds one version probe. The scan runs many programs, and one
// of them must not wait until the user cancels the whole command.
const probeTimeout = 30 * time.Second

const versionWorkers = 8

// windowsExts are the executable suffixes the Windows terminal tries, in order.
var windowsExts = []string{".exe", ".cmd", ".bat"}

type packageQuery struct {
	manager string
	command []string
	parse   func(output string) string
}

// packageQueries ask the system package database which package owns a file.
var packageQueries = []packageQuery{
	{manager: "apt", command: []string{"dpkg", "-S"}, parse: func(out string) string {
		line, _, _ := strings.Cut(out, "\n")
		pkg, _, ok := strings.Cut(line, ": ")
		if !ok {
			return ""
		}
		return pkg
	}},
	{manager: "dnf", command: []string{"rpm", "-qf", "--qf", "%{NAME}"}, parse: strings.TrimSpace},
	{manager: "pacman", command: []string{"pacman", "-Qqo"}, parse: strings.TrimSpace},
}

// origin tells which definition a glob pattern belongs to and whether it is a PATH directory.
type origin struct {
	def    int
	inPath bool
}

// scan finds installed versions of every catalog language on the host.
func (s *Service) scan(ctx context.Context) (*snapshot, error) {
	env, err := s.host.Env(ctx)
	if err != nil {
		return nil, fmt.Errorf("read environment: %w", err)
	}
	links, err := s.env.Links(ctx)
	if err != nil {
		return nil, fmt.Errorf("read links: %w", err)
	}
	p := s.host.Platform()
	home := homeDir(env, p)
	dirs := pathDirs(env["PATH"], p)

	var patterns []string
	var origins []origin
	for i, d := range s.catalog {
		for _, dir := range dirs {
			for _, g := range d.NameGlobs() {
				patterns = append(patterns, dir+"/"+g)
				origins = append(origins, origin{def: i, inPath: true})
			}
		}
		for _, loc := range d.Locations(p, home, env) {
			patterns = append(patterns, loc)
			origins = append(origins, origin{def: i})
		}
	}
	found, err := s.host.Glob(ctx, patterns)
	if err != nil {
		return nil, fmt.Errorf("find executables: %w", err)
	}

	inPath := make(map[string]bool)
	candidates := make([][]string, len(s.catalog))
	seen := make(map[string]bool)
	for k, files := range found {
		o := origins[k]
		d := s.catalog[o.def]
		for _, f := range files {
			if o.inPath {
				inPath[f] = true
			}
			key := string(d.ID) + "\x00" + f
			if seen[key] || !d.IsBinary(path.Base(f)) || d.IsStub(p, f) {
				continue
			}
			seen[key] = true
			candidates[o.def] = append(candidates[o.def], f)
		}
	}

	infos, err := s.inspect(ctx, candidates, links)
	if err != nil {
		return nil, err
	}

	langs := make([]domain.Language, len(s.catalog))
	for i, d := range s.catalog {
		langs[i].Definition = d
		langs[i].Versions = s.versions(candidates[i], infos, p, home)
	}
	if err := s.readNumbers(ctx, langs, env, p); err != nil {
		return nil, err
	}

	installed := make([]domain.Language, 0, len(langs))
	for i := range langs {
		if len(langs[i].Versions) == 0 {
			continue
		}
		slices.SortStableFunc(langs[i].Versions, func(a, b domain.Version) int {
			return domain.CompareVersions(b.Number, a.Number)
		})
		s.markActive(ctx, &langs[i], activeProbe{dirs: dirs, inPath: inPath, links: links, infos: infos, env: env})
		installed = append(installed, langs[i])
	}
	return &snapshot{env: env, home: home, links: links, languages: installed}, nil
}

func (s *Service) inspect(ctx context.Context, candidates [][]string, links map[string]string) (map[string]FileInfo, error) {
	var files []string
	for _, c := range candidates {
		files = append(files, c...)
	}
	for _, target := range links {
		files = append(files, target)
	}
	list, err := s.host.Inspect(ctx, files)
	if err != nil {
		return nil, fmt.Errorf("inspect executables: %w", err)
	}
	infos := make(map[string]FileInfo, len(list))
	for _, info := range list {
		infos[info.Path] = info
	}
	return infos, nil
}

// versions turns candidate files into versions: one per resolved file, without proxies and shims.
func (s *Service) versions(files []string, infos map[string]FileInfo, p domain.Platform, home string) []domain.Version {
	shim := s.env.ShimDir()
	resolved := make(map[string]bool)
	var out []domain.Version
	for _, f := range files {
		info, ok := infos[f]
		if !ok || path.Dir(f) == shim || domain.IsProxy(f) || domain.IsProxy(info.RealPath) {
			continue
		}
		if resolved[info.RealPath] {
			continue
		}
		resolved[info.RealPath] = true
		out = append(out, domain.Version{
			Path:        f,
			RealPath:    info.RealPath,
			InstalledAt: info.InstalledAt,
			Method:      domain.Classify(info.RealPath, p, home),
		})
	}
	return out
}

// readNumbers runs every candidate to learn its version and drops those that do not answer.
func (s *Service) readNumbers(ctx context.Context, langs []domain.Language, env map[string]string, p domain.Platform) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(versionWorkers)
	for i := range langs {
		d := langs[i].Definition
		for j := range langs[i].Versions {
			v := &langs[i].Versions[j]
			g.Go(func() error {
				number, ok := s.readNumber(gctx, d, v.Path, env)
				if !ok {
					return nil
				}
				v.Number = number
				if v.Method.System && p == domain.Linux {
					v.Method = s.systemPackage(gctx, v.RealPath, env, v.Method)
				}
				return nil
			})
		}
	}
	if err := g.Wait(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for i := range langs {
		langs[i].Versions = slices.DeleteFunc(langs[i].Versions, func(v domain.Version) bool { return v.Number == "" })
	}
	return nil
}

// readNumber trusts the output over the exit code: some interpreters print the version and exit non-zero.
func (s *Service) readNumber(ctx context.Context, d domain.Definition, file string, env map[string]string) (string, bool) {
	if number, ok := d.NumberFromPath(file); ok {
		return number, true
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := s.host.Run(ctx, Command{Path: file, Args: d.VersionArgs, Env: probeEnv(env)})
	number, ok := d.ParseVersion(string(out))
	if err != nil && !ok {
		return "", false
	}
	return number, ok
}

func (s *Service) systemPackage(ctx context.Context, file string, env map[string]string, m domain.InstallMethod) domain.InstallMethod {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	env = probeEnv(env)
	for _, q := range packageQueries {
		args := append(slices.Clone(q.command[1:]), file)
		out, err := s.host.Run(ctx, Command{Path: q.command[0], Args: args, Env: env})
		if err != nil {
			continue
		}
		if pkg := q.parse(string(out)); pkg != "" {
			m.Manager = q.manager
			m.Package = pkg
			return m
		}
	}
	return m
}

type activeProbe struct {
	dirs   []string
	inPath map[string]bool
	links  map[string]string
	infos  map[string]FileInfo
	env    map[string]string
}

// markActive marks the version the terminal starts: the first primary executable in PATH.
// A proxy of a version manager is asked for its version, which is then matched by number.
func (s *Service) markActive(ctx context.Context, l *domain.Language, probe activeProbe) {
	hit := s.firstInPath(l.Primaries, probe)
	if hit == "" || l.IsStub(s.host.Platform(), hit) {
		return
	}
	if path.Dir(hit) == s.env.ShimDir() {
		if target, ok := probe.links[toolName(path.Base(hit))]; ok {
			hit = target
		}
	}
	if info, ok := probe.infos[hit]; ok {
		for j := range l.Versions {
			if l.Versions[j].RealPath == info.RealPath {
				l.Versions[j].Active = true
				return
			}
		}
	}
	number, ok := s.readNumber(ctx, l.Definition, hit, probe.env)
	if !ok {
		return
	}
	// Two installs can print the same number. Marking one of them active would
	// let the other be deleted, and that other one may be what PATH runs.
	for j := range l.Versions {
		if l.Versions[j].Number == number {
			l.Versions[j].Active = true
		}
	}
}

func (s *Service) firstInPath(primaries []string, probe activeProbe) string {
	exts := []string{""}
	if s.host.Platform() == domain.Windows {
		exts = windowsExts
	}
	for _, name := range primaries {
		for _, dir := range probe.dirs {
			for _, ext := range exts {
				if f := dir + "/" + name + ext; probe.inPath[f] {
					return f
				}
			}
		}
	}
	return ""
}

func toolName(file string) string {
	for _, ext := range windowsExts {
		if strings.HasSuffix(strings.ToLower(file), ext) {
			return file[:len(file)-len(ext)]
		}
	}
	return file
}

// probeEnv is the environment a version probe may see. Secrets that happen to
// be in the login environment, such as a cloud token, are not passed on.
func probeEnv(env map[string]string) map[string]string {
	allow := map[string]bool{}
	for _, key := range []string{
		"PATH", "PATHEXT", "HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH",
		"SYSTEMROOT", "WINDIR", "SYSTEMDRIVE", "COMSPEC", "TMP", "TEMP", "TMPDIR",
		"LANG", "LC_ALL", "LC_CTYPE", "LC_MESSAGES",
		"PYENV_ROOT", "RBENV_ROOT", "NVM_DIR", "ASDF_DIR", "ASDF_DATA_DIR",
		"MISE_DATA_DIR", "SDKMAN_DIR", "CARGO_HOME", "RUSTUP_HOME", "VOLTA_HOME", "FNM_DIR",
	} {
		allow[key] = true
	}
	out := make(map[string]string)
	for key, value := range env {
		// PATH is allowed on purpose: the probe has to find helper programs.
		// It is still refused as a variable Lman exports into the shell.
		if allow[strings.ToUpper(key)] {
			out[key] = value
		}
	}
	// Unset means "auto": the go command then reads go.mod in the working
	// directory and its parents and may download that toolchain.
	out["GOTOOLCHAIN"] = "local"
	return out
}

// toolchainEnv is the environment of a toolchain command such as `go env`.
// Language variables that are already exported are visible, so the reported
// values match the terminal. Loader variables and anything else, including
// cloud tokens, are not passed on.
func toolchainEnv(env map[string]string, l domain.Language) map[string]string {
	out := probeEnv(env)
	for key, value := range env {
		if l.ReportsVar(key) {
			out[key] = value
		}
	}
	return out
}

func homeDir(env map[string]string, p domain.Platform) string {
	if p == domain.Windows {
		return strings.ReplaceAll(env["USERPROFILE"], `\`, "/")
	}
	return env["HOME"]
}

func pathDirs(value string, p domain.Platform) []string {
	sep := ":"
	if p == domain.Windows {
		sep = ";"
	}
	var dirs []string
	seen := make(map[string]bool)
	for _, dir := range strings.Split(value, sep) {
		dir = strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(dir), `\`, "/"), "/")
		if dir == "" || seen[dir] || !absDir(dir, p) {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	return dirs
}

// absDir reports whether dir is an absolute directory. A relative PATH entry such as
// "." or "node_modules/.bin" is ignored, so a scan does not run programs from the
// working directory.
func absDir(dir string, p domain.Platform) bool {
	if p == domain.Windows {
		if strings.HasPrefix(dir, "//") {
			return true
		}
		return len(dir) >= 3 && dir[1] == ':' && dir[2] == '/'
	}
	return strings.HasPrefix(dir, "/")
}
