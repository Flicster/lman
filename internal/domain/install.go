package domain

import (
	"path"
	"regexp"
	"strings"
)

// layout is a directory a version manager keeps its installs in. A file belongs to a
// manager only when its path starts with that directory. A path that merely contains
// the same segments, such as ~/work/.nvm/... or /tmp/x/Cellar/..., is not managed.
type layout struct {
	manager string
	// root is relative to the home directory, or to / when abs is set.
	root string
	abs  bool
	// brew roots sit under each Homebrew prefix of the platform.
	brew bool
	// depth is how many segments below root form the version root.
	depth int
	// withPackage takes the first segment below root as the package name.
	withPackage bool
}

var layouts = []layout{
	{manager: "brew", root: "Cellar", brew: true, depth: 2, withPackage: true},
	{manager: "nvm", root: ".nvm/versions/node", depth: 1},
	{manager: "nvm", root: "AppData/Roaming/nvm", depth: 1},
	{manager: "pyenv", root: ".pyenv/versions", depth: 1},
	{manager: "rustup", root: ".rustup/toolchains", depth: 1, withPackage: true},
	{manager: "rbenv", root: ".rbenv/versions", depth: 1},
	{manager: "sdkman", root: ".sdkman/candidates", depth: 2},
	{manager: "asdf", root: ".asdf/installs", depth: 2},
	{manager: "mise", root: ".local/share/mise/installs", depth: 2},
	{manager: "fnm", root: ".local/share/fnm/node-versions", depth: 1},
	{manager: "fnm", root: "Library/Application Support/fnm/node-versions", depth: 1},
	{manager: "volta", root: ".volta/tools/image", depth: 2},
	{manager: "uv", root: ".local/share/uv/python", depth: 1},
	{manager: "scoop", root: "scoop/apps", depth: 2, withPackage: true},
	{manager: "snap", root: "snap", abs: true, depth: 2, withPackage: true},
}

// homeManagers keep every version in its own directory under the home directory,
// so deleting that directory removes the version.
var homeManagers = map[string]bool{
	"nvm": true, "pyenv": true, "rbenv": true, "sdkman": true, "asdf": true,
	"mise": true, "fnm": true, "volta": true, "uv": true,
}

// versionDirPrefixes are toolchain names that may precede a version in a directory
// name: go1.24.0, jdk-21, node-v20.1.0. The version has to be the whole remainder
// of the name. A substring is not enough, so "21" does not own "client-21".
var versionDirPrefixes = []string{
	"go", "golang",
	"python", "python3",
	"node", "nodejs",
	"jdk", "jre", "openjdk", "java", "temurin",
	"ruby", "php",
	"rust", "rustc",
	"dotnet",
	"gcc", "clang",
}

// jdkBundle is a macOS JDK bundle: the directory up to ".jdk" is the installation.
var jdkBundle = regexp.MustCompile(`^(.*\.jdk)/Contents/Home/bin/`)

// packageName is a name the package manager accepts. A leading "-" would be read as an
// option, and shell syntax has no place in a name that is printed as a command.
var packageName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._+-]{0,127}$`)

// Classify derives the install method from the resolved executable path. home is the
// user's home directory, empty when unknown. For system files the package manager stays
// empty: only the system package database knows it.
func Classify(realPath string, p Platform, home string) InstallMethod {
	for _, l := range layouts {
		for _, base := range l.bases(p, home) {
			prefix := base + "/"
			if !strings.HasPrefix(realPath, prefix) {
				continue
			}
			rest := strings.Split(realPath[len(prefix):], "/")
			if len(rest) <= l.depth {
				continue
			}
			m := InstallMethod{
				Kind:    MethodManager,
				Manager: l.manager,
				Root:    prefix + strings.Join(rest[:l.depth], "/"),
			}
			if l.withPackage {
				m.Package = rest[0]
			}
			return m
		}
	}
	if isSystemPath(realPath, p) {
		kind := MethodDirect
		if p == Linux {
			kind = MethodManager
		}
		return InstallMethod{Kind: kind, System: true}
	}
	return InstallMethod{Kind: MethodDirect, Root: directRoot(realPath)}
}

// bases are the directories a layout can live in on a platform.
func (l layout) bases(p Platform, home string) []string {
	if l.brew {
		var out []string
		for _, prefix := range BrewPrefixes(p) {
			out = append(out, prefix+"/"+l.root)
		}
		return out
	}
	if l.abs {
		return []string{"/" + l.root}
	}
	if home == "" {
		return nil
	}
	return []string{home + "/" + l.root}
}

// String names the method for people: "brew (go)", "apt (golang-go)", "system" or "direct".
func (m InstallMethod) String() string {
	switch {
	case m.Manager != "" && m.Package != "":
		return m.Manager + " (" + m.Package + ")"
	case m.Manager != "":
		return m.Manager
	case m.System:
		return "system"
	}
	return string(MethodDirect)
}

func isSystemPath(file string, p Platform) bool {
	switch p {
	case Linux:
		return strings.HasPrefix(file, "/usr/") && !strings.HasPrefix(file, "/usr/local/") ||
			strings.HasPrefix(file, "/bin/")
	case Darwin:
		return strings.HasPrefix(file, "/usr/bin/") ||
			strings.HasPrefix(file, "/Library/Developer/CommandLineTools/") ||
			strings.HasPrefix(file, "/Applications/Xcode")
	case Windows:
		return strings.HasPrefix(strings.ToLower(file), "c:/windows/")
	}
	return false
}

// directRoot is the installation directory around bin/, e.g. /usr/local/go or the .jdk bundle.
func directRoot(file string) string {
	if m := jdkBundle.FindStringSubmatch(file); m != nil {
		return m[1]
	}
	dir := path.Dir(file)
	if path.Base(dir) != "bin" {
		return ""
	}
	return path.Dir(dir)
}

// packageCommands are the system package managers Lman only describes. It never runs them.
var packageCommands = map[string]string{
	"apt":    "sudo apt remove ",
	"dnf":    "sudo dnf remove ",
	"pacman": "sudo pacman -R ",
	"snap":   "sudo snap remove ",
	"scoop":  "scoop uninstall ",
}

// PlanRemoval decides how to delete a version. Lman deletes only what belongs to the user:
// one Homebrew keg, a manager command that names one toolchain, and version directories
// inside the home directory.
// A directory counts only when a home version manager owns it, or its name is that version.
// Shared prefixes (/usr/local, ~/.local, the home directory) are never deleted and never
// suggested as an rm target. Paths and names in a manual command are quoted for the shell.
func PlanRemoval(v Version, p Platform, home string) (Removal, error) {
	m := v.Method
	if home != "" {
		home = path.Clean(home)
	}
	switch m.Manager {
	case "brew":
		// `brew uninstall <formula>` removes the linked keg, or every keg of the
		// formula. That is a different version from the one the user asked for
		// whenever the rack holds more than one. The keg directory is that version.
		prefix, ok := brewPrefixOf(v.RealPath, p)
		if !ok || !packageName.MatchString(m.Package) {
			return Removal{}, &ManualRemovalError{}
		}
		keg, ok := brewKeg(v.RealPath, prefix, m.Package)
		if !ok {
			return Removal{}, &ManualRemovalError{}
		}
		return Removal{Dir: keg}, nil
	case "rustup":
		if home == "" || !packageName.MatchString(m.Package) {
			return Removal{}, &ManualRemovalError{}
		}
		return Removal{Command: []string{home + "/.cargo/bin/" + executable("rustup", p), "toolchain", "uninstall", m.Package}}, nil
	case "apt", "dnf", "pacman", "snap", "scoop":
		if !packageName.MatchString(m.Package) {
			return Removal{}, &ManualRemovalError{}
		}
		return Removal{}, &ManualRemovalError{Hint: packageCommands[m.Manager] + m.Package}
	}
	if m.System || m.Root == "" {
		return Removal{}, &ManualRemovalError{}
	}
	root := path.Clean(m.Root)
	if sharedPrefix(root, home, p) {
		return Removal{}, &ManualRemovalError{}
	}
	// A home version manager owns its directory. A direct install is deleted only when
	// it is a version-named directory under ~/sdk, which is where `go install` of a
	// toolchain and a manually unpacked JDK land. ~/projects/go1.24.0 is a project
	// that happens to contain a binary, so it is neither deleted nor suggested as rm.
	named := versionOwnsDir(path.Base(root), v.Number)
	owned := homeManagers[m.Manager] || (named && sdkInstall(root, home, p))
	if owned && insideHome(root, home) {
		return Removal{Dir: root}, nil
	}
	if named && !insideHome(root, home) {
		return Removal{}, &ManualRemovalError{Hint: manualRemoveHint(root, p)}
	}
	return Removal{}, &ManualRemovalError{}
}

// sdkInstall reports whether root is one version directory inside the home sdk folder.
func sdkInstall(root, home string, p Platform) bool {
	if home == "" || !strings.HasPrefix(root, home+"/") {
		return false
	}
	parent := path.Base(path.Dir(root))
	if p == Windows {
		return strings.EqualFold(parent, "sdk")
	}
	return parent == "sdk"
}

// kegVersion is the directory name of one Homebrew keg. ".." must not be a keg.
var kegVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// brewKeg is prefix/Cellar/<formula>/<version>, the directory of this install.
// The rack (Cellar/<formula>) holds every version, so a path that stops there is refused.
func brewKeg(realPath, prefix, pkg string) (string, bool) {
	rest, ok := strings.CutPrefix(realPath, prefix+"/Cellar/"+pkg+"/")
	if !ok || rest == "" {
		return "", false
	}
	version, _, found := strings.Cut(rest, "/")
	if !found || !kegVersion.MatchString(version) {
		return "", false
	}
	return prefix + "/Cellar/" + pkg + "/" + version, true
}

// brewPrefixOf returns the Homebrew prefix a Cellar path belongs to. A Cellar somewhere
// else has no brew beside it, so it must not run that brew.
func brewPrefixOf(realPath string, p Platform) (string, bool) {
	for _, prefix := range BrewPrefixes(p) {
		if strings.HasPrefix(realPath, prefix+"/Cellar/") {
			return prefix, true
		}
	}
	return "", false
}

// versionOwnsDir reports whether base is the directory of number.
// A dotted version may follow the toolchain name directly (go1.24.0). A version
// without a dot needs a separator (jdk-21), so a short number cannot match a project name.
func versionOwnsDir(base, number string) bool {
	if base == "" || base == "." || base == ".." || number == "" {
		return false
	}
	if base == number {
		return true
	}
	seps := []string{"-", "_"}
	if strings.Contains(number, ".") {
		seps = append([]string{""}, seps...)
	}
	for _, prefix := range versionDirPrefixes {
		for _, sep := range seps {
			if base == prefix+sep+number {
				return true
			}
			if sep != "" && (base == prefix+sep+"v"+number || base == prefix+sep+"V"+number) {
				return true
			}
		}
	}
	return false
}

// sharedPrefix is a directory that holds more than one installation.
// Removing it, or telling the user to, deletes unrelated files.
func sharedPrefix(dir, home string, p Platform) bool {
	if dir == "/" || dir == "." {
		return true
	}
	// /usr, /opt, /etc and the other top-level directories are never one installation.
	if strings.HasPrefix(dir, "/") && !strings.Contains(strings.Trim(dir, "/"), "/") {
		return true
	}
	for _, s := range []string{
		"/usr/bin", "/usr/sbin", "/usr/local", "/usr/local/bin",
		"/opt/local", "/opt/homebrew", "/opt/homebrew/bin",
		"/home/linuxbrew/.linuxbrew",
	} {
		if dir == s {
			return true
		}
	}
	if home != "" && pathEqual(dir, home, p) {
		return true
	}
	if home != "" {
		for _, rel := range []string{
			"bin", ".local", ".local/bin", ".local/share", "go",
			"Library", "Applications",
			"AppData", "AppData/Local", "AppData/Roaming",
		} {
			if pathEqual(dir, home+"/"+rel, p) {
				return true
			}
		}
	}
	if p == Windows {
		lower := strings.ToLower(dir)
		if (len(lower) == 2 && lower[1] == ':') || (len(lower) == 3 && lower[1] == ':' && lower[2] == '/') {
			return true
		}
		switch lower {
		case "c:/windows", "c:/program files", "c:/program files (x86)", "c:/users":
			return true
		}
	}
	return false
}

func pathEqual(a, b string, p Platform) bool {
	if p == Windows {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func manualRemoveHint(dir string, p Platform) string {
	if p == Windows {
		return "Remove-Item -Recurse -LiteralPath " + powershellQuote(dir) + " as administrator"
	}
	return "sudo rm -rf " + posixQuote(dir)
}

// posixQuote is one literal word for a POSIX shell.
func posixQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// powershellQuote is one literal argument. Single quotes do not expand $(...).
func powershellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// insideHome requires at least two levels below home, so home itself
// and its top-level folders such as ~/go are never deleted.
func insideHome(dir, home string) bool {
	if home == "" || !strings.HasPrefix(dir, home+"/") {
		return false
	}
	return strings.Count(strings.Trim(strings.TrimPrefix(dir, home), "/"), "/") >= 1
}

func executable(name string, p Platform) string {
	if p == Windows {
		return name + ".exe"
	}
	return name
}
