//go:build !windows

package session

import (
	"github.com/Flicster/lman/internal/app"
	"github.com/Flicster/lman/internal/host"
	"github.com/Flicster/lman/internal/shellenv"
)

func localEnvironment(local *host.Local, env map[string]string) (app.Environment, error) {
	return shellenv.NewUnix(local, env, local.Platform())
}
