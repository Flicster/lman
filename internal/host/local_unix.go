//go:build !windows

package host

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

const loginShellTimeout = 10 * time.Second

// loginEnv asks the user shell for the environment of a new terminal.
// Desktop applications on macOS start with a minimal PATH, so the process
// environment is used only when the shell does not answer.
func loginEnv(ctx context.Context) (map[string]string, error) {
	shellCtx, cancel := context.WithTimeout(ctx, loginShellTimeout)
	defer cancel()
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(shellCtx, shell, "-ilc", envScript)
	cmd.WaitDelay = waitDelay
	if out, err := cmd.Output(); err == nil {
		if env := parseEnv(string(out)); env["PATH"] != "" {
			return env, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return processEnv(), nil
}

func processEnv() map[string]string {
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

func hideWindow(*exec.Cmd) {}
