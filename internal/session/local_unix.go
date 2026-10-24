//go:build !windows

package session

import (
	"lman/internal/app"
	"lman/internal/host"
	"lman/internal/shellenv"
)

func localEnvironment(local *host.Local, env map[string]string) app.Environment {
	return shellenv.NewUnix(local, env, local.Platform())
}
