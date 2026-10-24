package app

import (
	"time"

	"lman/internal/domain"
)

// FileInfo describes an existing executable on a host.
type FileInfo struct {
	Path     string
	RealPath string
	// InstalledAt is the file creation time, or the inode change time where creation is unknown.
	InstalledAt time.Time
}

// Command is a program started on a host. Path may be absolute or a name looked up in PATH.
type Command struct {
	Path string
	Args []string
	Env  map[string]string
}

// Activation is the result of making a version active.
type Activation struct {
	Version domain.Version
	// ShadowedBy is the executable that still comes first in PATH, if the switch did not take effect.
	ShadowedBy string
	// NotInPath means new terminals do not get the shim directory, e.g. a shell Lman does not configure.
	NotInPath bool
}

// SystemInfo lists the main characteristics of the computer.
type SystemInfo struct {
	Hostname    string
	OS          string
	OSVersion   string
	Arch        string
	CPU         string
	Cores       int
	MemoryBytes uint64
}

// StatusInfo is the application context shown next to the languages.
type StatusInfo struct {
	System  SystemInfo
	Version string
	Time    time.Time
}

// Preferences are the settings of the user stored on the local computer.
type Preferences struct {
	Theme   domain.Theme `json:"theme"`
	Servers []string     `json:"servers"`
}
