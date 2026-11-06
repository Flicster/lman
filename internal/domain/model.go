package domain

import (
	"regexp"
	"time"
)

// Platform is an operating system family as reported by Go: darwin, linux or windows.
type Platform string

const (
	Darwin  Platform = "darwin"
	Linux   Platform = "linux"
	Windows Platform = "windows"
)

// LanguageID identifies a programming language in the catalog, e.g. "go" or "python".
type LanguageID string

// Definition describes how Lman finds, inspects and manages one programming language.
type Definition struct {
	ID      LanguageID
	Name    string
	Aliases []string
	// Primaries are the executables a user types in the terminal, in lookup order.
	// The first of them found in PATH decides the active version.
	Primaries []string
	// BinaryGlob and Globs are file name patterns searched in every PATH directory.
	// Binary decides which of the found names belong to the language.
	BinaryGlob string
	Globs      []string
	Binary     *regexp.Regexp
	// PathVersion extracts the version from the file path, for compilers that are not
	// executables themselves, such as vbc.dll inside a .NET SDK directory.
	PathVersion *regexp.Regexp
	// VersionArgs make the executable print its version; VersionPattern captures the number.
	VersionArgs    []string
	VersionPattern *regexp.Regexp
	// VersionPrefix is prepended to the number for display: "go" gives go1.25.1.
	VersionPrefix string
	// Tools are executables linked into the shim directory when a version becomes active.
	Tools []string
	// Search lists install locations outside PATH. Tokens: "~" home directory,
	// "{brew}" every Homebrew prefix, "{programfiles}", "{localappdata}", "{appdata}" on Windows.
	Search []string
	// DarwinStubs are macOS placeholders that open an installer dialog when executed.
	DarwinStubs []string
	Hints       []Hint
	// EnvVars are the variables Lman can set for new terminals.
	EnvVars []string
	// EnvArgs is a command of the toolchain that prints its effective environment
	// as a JSON object of strings. Go uses "env -json", which includes defaults
	// the toolchain fills in when the variable is not exported.
	EnvArgs    []string
	ManualArgs []string
	DocsURL    string
}

// Hint explains a tool of the language ecosystem, e.g. uvx for Python.
type Hint struct {
	Tool    string
	Purpose string
}

// MethodKind tells whether a version was installed directly or by a manager.
type MethodKind string

const (
	MethodDirect  MethodKind = "direct"
	MethodManager MethodKind = "manager"
)

// InstallMethod is how a version got onto the computer.
type InstallMethod struct {
	Kind MethodKind
	// Manager is the package or version manager: brew, apt, nvm, pyenv and so on.
	Manager string
	// Package is the formula, package or toolchain name when the manager knows one.
	Package string
	// Root is the installation directory of the version when it can be determined.
	Root string
	// System marks files owned by the operating system or its package database.
	System bool
}

// Version is one installed version of a language.
type Version struct {
	Number string
	// Path is where the executable was found: a PATH entry or a known install location.
	Path string
	// RealPath is Path with symbolic links resolved.
	RealPath    string
	InstalledAt time.Time
	Method      InstallMethod
	Active      bool
}

// Language is an installed language with its versions, newest first.
type Language struct {
	Definition
	Versions []Version
}

// EnvVar is an environment variable of a language.
type EnvVar struct {
	Name  string
	Value string
	// Managed means Lman sets the value in new terminals.
	Managed bool
	// Default means the toolchain reported the value and the variable is not exported.
	Default bool
}

// Manual is the reference text of a language.
type Manual struct {
	Text    string
	DocsURL string
}

// Removal is a plan to delete one version: run Command or delete Dir.
type Removal struct {
	Command []string
	Dir     string
}

// Theme is the color theme of the desktop application.
type Theme string

const (
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
	ThemeSystem Theme = "system"
)
