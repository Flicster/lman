package app

import (
	"context"

	"lman/internal/domain"
)

// Host is the computer whose languages are managed: the local one or a server.
// Paths use forward slashes on every platform.
type Host interface {
	Name() string
	Platform() domain.Platform
	// Env is the environment of a new terminal on the host.
	Env(ctx context.Context) (map[string]string, error)
	// Glob returns existing files for every pattern, in pattern order.
	Glob(ctx context.Context, patterns []string) ([][]string, error)
	// Inspect resolves files; missing files are left out.
	Inspect(ctx context.Context, paths []string) ([]FileInfo, error)
	// Run returns combined stdout and stderr; a non-zero exit is an error.
	Run(ctx context.Context, cmd Command) ([]byte, error)
	RemoveAll(ctx context.Context, dir string) error
}

// Environment is the shell configuration Lman owns on a host: a directory of links
// placed first in PATH and environment variables exported to new terminals.
type Environment interface {
	ShimDir() string
	// Links maps a tool name to the executable its shim starts.
	Links(ctx context.Context) (map[string]string, error)
	Link(ctx context.Context, links map[string]string) error
	Unlink(ctx context.Context, names []string) error
	Vars(ctx context.Context) (map[string]string, error)
	// SetVar exports a variable; an empty value stops exporting it.
	SetVar(ctx context.Context, name, value string) error
}

// SystemProbe reads the characteristics of the local computer.
type SystemProbe interface {
	Probe(ctx context.Context) (SystemInfo, error)
}

// PreferenceStore keeps Preferences between runs.
type PreferenceStore interface {
	Load(ctx context.Context) (Preferences, error)
	Save(ctx context.Context, p Preferences) error
}
