package domain

import (
	"path"
	"strings"
)

// layout is a directory structure a manager uses for installed versions:
// marker, then depth segments that form the version root.
type layout struct {
	manager string
	marker  string
	depth   int
	// withPackage takes the first segment after marker as the package name.
	withPackage bool
}

var layouts = []layout{
	{manager: "brew", marker: "/Cellar/", depth: 2, withPackage: true},
	{manager: "nvm", marker: "/.nvm/versions/node/", depth: 1},
	{manager: "nvm", marker: "/AppData/Roaming/nvm/", depth: 1},
	{manager: "pyenv", marker: "/.pyenv/versions/", depth: 1},
	{manager: "rustup", marker: "/.rustup/toolchains/", depth: 1, withPackage: true},
	{manager: "rbenv", marker: "/.rbenv/versions/", depth: 1},
	{manager: "sdkman", marker: "/.sdkman/candidates/", depth: 2},
	{manager: "asdf", marker: "/.asdf/installs/", depth: 2},
	{manager: "mise", marker: "/mise/installs/", depth: 2},
	{manager: "fnm", marker: "/fnm/node-versions/", depth: 1},
	{manager: "volta", marker: "/.volta/tools/image/", depth: 2},
	{manager: "uv", marker: "/uv/python/", depth: 1},
	{manager: "scoop", marker: "/scoop/apps/", depth: 2, withPackage: true},
	{manager: "snap", marker: "/snap/", depth: 2, withPackage: true},
}

// homeManagers keep every version in its own directory under the home directory,
// so deleting that directory removes the version.
var homeManagers = map[string]bool{
	"nvm": true, "pyenv": true, "rbenv": true, "sdkman": true, "asdf": true,
	"mise": true, "fnm": true, "volta": true, "uv": true,
}

// Classify derives the install method from the resolved executable path.
// For system files the package manager stays empty: only the system package database knows it.
func Classify(realPath string, p Platform) InstallMethod {
	for _, l := range layouts {
		i := strings.Index(realPath, l.marker)
		if i < 0 {
			continue
		}
		rest := strings.Split(realPath[i+len(l.marker):], "/")
		if len(rest) <= l.depth {
			continue
		}
		m := InstallMethod{
			Kind:    MethodManager,
			Manager: l.manager,
			Root:    realPath[:i+len(l.marker)] + strings.Join(rest[:l.depth], "/"),
		}
		if l.withPackage {
			m.Package = rest[0]
		}
		return m
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
	if i := strings.Index(file, ".jdk/"); i >= 0 {
		return file[:i+len(".jdk")]
	}
	dir := path.Dir(file)
	if path.Base(dir) != "bin" {
		return ""
	}
	return path.Dir(dir)
}

// PlanRemoval decides how to delete a version. Lman deletes only what belongs to the user:
// manager commands and version directories inside the home directory.
// Everything else returns ManualRemovalError with the command to run.
func PlanRemoval(v Version, p Platform, home string) (Removal, error) {
	m := v.Method
	switch m.Manager {
	case "brew":
		i := strings.Index(v.RealPath, "/Cellar/")
		return Removal{Command: []string{v.RealPath[:i] + "/bin/brew", "uninstall", m.Package}}, nil
	case "rustup":
		return Removal{Command: []string{home + "/.cargo/bin/" + executable("rustup", p), "toolchain", "uninstall", m.Package}}, nil
	case "apt":
		return Removal{}, &ManualRemovalError{Hint: "sudo apt remove " + m.Package}
	case "dnf":
		return Removal{}, &ManualRemovalError{Hint: "sudo dnf remove " + m.Package}
	case "pacman":
		return Removal{}, &ManualRemovalError{Hint: "sudo pacman -R " + m.Package}
	case "snap":
		return Removal{}, &ManualRemovalError{Hint: "sudo snap remove " + m.Package}
	case "scoop":
		return Removal{}, &ManualRemovalError{Hint: "scoop uninstall " + m.Package}
	}
	if m.System || m.Root == "" {
		return Removal{}, &ManualRemovalError{}
	}
	ownDir := homeManagers[m.Manager] || strings.Contains(path.Base(m.Root), v.Number)
	if ownDir && insideHome(m.Root, home) {
		return Removal{Dir: m.Root}, nil
	}
	if p == Windows {
		return Removal{}, &ManualRemovalError{Hint: `Remove-Item -Recurse "` + m.Root + `" as administrator`}
	}
	return Removal{}, &ManualRemovalError{Hint: "sudo rm -rf '" + m.Root + "'"}
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
