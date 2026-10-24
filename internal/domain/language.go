package domain

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// proxies are directories whose executables forward to a version chosen elsewhere.
// They tell which version is active but are not versions themselves.
var proxies = []string{
	"/.cargo/bin/",
	"/.pyenv/shims/",
	"/.rbenv/shims/",
	"/.nodenv/shims/",
	"/.goenv/shims/",
	"/.jenv/shims/",
	"/.asdf/shims/",
	"/mise/shims/",
	"/.volta/bin/",
	"/snap/bin/",
}

// Label is the version as users see it, e.g. go1.25.1 or v22.3.0.
func (d Definition) Label(number string) string {
	return d.VersionPrefix + number
}

// Matches reports whether query names this language by id, name or alias.
func (d Definition) Matches(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == string(d.ID) || q == strings.ToLower(d.Name) {
		return true
	}
	return slices.Contains(d.Aliases, q)
}

// OwnsVar reports whether name is one of the language environment variables.
func (d Definition) OwnsVar(name string) bool {
	return slices.Contains(d.EnvVars, name)
}

// Locations expands Search for a platform. home is the user home directory,
// env provides Windows folders. Patterns of other platforms are dropped.
func (d Definition) Locations(p Platform, home string, env map[string]string) []string {
	var out []string
	for _, pattern := range d.Search {
		for _, expanded := range expand(pattern, p, home, env) {
			if p == Windows && !strings.HasSuffix(expanded, ".exe") && !strings.HasSuffix(expanded, ".dll") {
				expanded += ".exe"
			}
			out = append(out, expanded)
		}
	}
	return out
}

func expand(pattern string, p Platform, home string, env map[string]string) []string {
	windowsOnly := strings.HasPrefix(pattern, "{programfiles}") ||
		strings.HasPrefix(pattern, "{localappdata}") ||
		strings.HasPrefix(pattern, "{appdata}")
	switch {
	case windowsOnly && p != Windows:
		return nil
	case strings.HasPrefix(pattern, "/") && p == Windows:
		return nil
	case strings.HasPrefix(pattern, "{brew}"):
		var out []string
		for _, prefix := range BrewPrefixes(p) {
			out = append(out, prefix+strings.TrimPrefix(pattern, "{brew}"))
		}
		return out
	case strings.HasPrefix(pattern, "~"):
		if home == "" {
			return nil
		}
		return []string{home + strings.TrimPrefix(pattern, "~")}
	}
	tokens := map[string]string{
		"{programfiles}": env["ProgramFiles"],
		"{localappdata}": env["LOCALAPPDATA"],
		"{appdata}":      env["APPDATA"],
	}
	for token, dir := range tokens {
		if strings.HasPrefix(pattern, token) {
			if dir == "" {
				return nil
			}
			return []string{toSlash(dir) + strings.TrimPrefix(pattern, token)}
		}
	}
	return []string{pattern}
}

// BrewPrefixes are the Homebrew installation prefixes of a platform.
func BrewPrefixes(p Platform) []string {
	switch p {
	case Darwin:
		return []string{"/opt/homebrew", "/usr/local"}
	case Linux:
		return []string{"/home/linuxbrew/.linuxbrew"}
	}
	return nil
}

// NameGlobs are the file name patterns searched in PATH.
func (d Definition) NameGlobs() []string {
	out := make([]string, 0, 1+len(d.Globs))
	if d.BinaryGlob != "" {
		out = append(out, d.BinaryGlob)
	}
	return append(out, d.Globs...)
}

// IsBinary reports whether an executable file name belongs to the language.
// The ".exe" suffix is ignored.
func (d Definition) IsBinary(name string) bool {
	return d.Binary.MatchString(strings.TrimSuffix(strings.ToLower(name), ".exe"))
}

// IsStub reports whether file is a macOS placeholder that must not be executed.
func (d Definition) IsStub(p Platform, file string) bool {
	return p == Darwin && slices.Contains(d.DarwinStubs, file)
}

// ParseVersion extracts the version number from the output of VersionArgs.
// The first non-empty capture is the number.
func (d Definition) ParseVersion(output string) (string, bool) {
	m := d.VersionPattern.FindStringSubmatch(output)
	for _, g := range m[1:] {
		if g != "" {
			return g, true
		}
	}
	return "", false
}

// NumberFromPath extracts the version from an install path when PathVersion is set.
func (d Definition) NumberFromPath(file string) (string, bool) {
	if d.PathVersion == nil {
		return "", false
	}
	m := d.PathVersion.FindStringSubmatch(file)
	if len(m) < 2 || m[1] == "" {
		return "", false
	}
	return m[1], true
}

// IsProxy reports whether file forwards to a version chosen by a version manager.
func IsProxy(file string) bool {
	for _, dir := range proxies {
		if strings.Contains(file, dir) {
			return true
		}
	}
	return false
}

// ValidateVarName checks that name can be exported as an environment variable.
func ValidateVarName(name string) error {
	if !varName.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidVarName, name)
	}
	return nil
}

// Active returns the version that runs in the terminal.
func (l Language) Active() (Version, bool) {
	for _, v := range l.Versions {
		if v.Active {
			return v, true
		}
	}
	return Version{}, false
}

// Find selects a version by number, label, unique number prefix or path.
func (l Language) Find(query string) (Version, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return Version{}, fmt.Errorf("%w: empty version", ErrVersionNotFound)
	}
	for _, v := range l.Versions {
		if q == v.Path || q == v.RealPath {
			return v, nil
		}
	}
	number := strings.TrimPrefix(strings.TrimPrefix(q, l.VersionPrefix), "v")
	exact := l.filter(func(v Version) bool { return v.Number == number })
	if len(exact) == 0 {
		exact = l.filter(func(v Version) bool { return strings.HasPrefix(v.Number, number+".") })
	}
	switch len(exact) {
	case 0:
		return Version{}, fmt.Errorf("%w: %s %s", ErrVersionNotFound, l.Name, q)
	case 1:
		return exact[0], nil
	}
	paths := make([]string, 0, len(exact))
	for _, v := range exact {
		paths = append(paths, v.Path)
	}
	return Version{}, fmt.Errorf("%w: %s, pass the path: %s", ErrAmbiguousVersion, q, strings.Join(paths, ", "))
}

func (l Language) filter(keep func(Version) bool) []Version {
	var out []Version
	for _, v := range l.Versions {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}

// BinDir is the directory with the executables of the version.
func (v Version) BinDir() string {
	return path.Dir(v.RealPath)
}

// CompareVersions orders version numbers numerically: "1.10" is newer than "1.9".
func CompareVersions(a, b string) int {
	pa, pb := numericParts(a), numericParts(b)
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x - y
		}
	}
	return strings.Compare(a, b)
}

func numericParts(s string) []int {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r < '0' || r > '9' })
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

func toSlash(p string) string {
	return strings.ReplaceAll(p, `\`, "/")
}
