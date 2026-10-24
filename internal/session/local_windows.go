package session

import (
	"lman/internal/app"
	"lman/internal/host"
	"lman/internal/shellenv"
)

func localEnvironment(_ *host.Local, env map[string]string) app.Environment {
	return shellenv.NewWindows(env)
}
