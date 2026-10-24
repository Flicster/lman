package app

import (
	"runtime/debug"
	"strings"
)

// AppVersion is the Lman version shown in the window and by lman status.
// linked is set with -ldflags "-X main.version=...".
// An empty value and "dev" use the module version recorded by go install.
func AppVersion(linked string) string {
	if v := plainVersion(linked); v != "" {
		return v
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := plainVersion(info.Main.Version); v != "" {
		return v
	}
	return "dev"
}

// plainVersion drops a single leading "v", as in the module version v1.2.3.
func plainVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	switch v {
	case "", "dev", "(devel)":
		return ""
	default:
		return v
	}
}
