package session

import (
	"github.com/Flicster/lman/internal/app"
	"github.com/Flicster/lman/internal/host"
	"github.com/Flicster/lman/internal/shellenv"
)

func localEnvironment(_ *host.Local, env map[string]string) (app.Environment, error) {
	return shellenv.NewWindows(env)
}
