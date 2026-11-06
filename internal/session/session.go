// Package session wires the language service for the local computer or a server.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Flicster/lman/internal/app"
	"github.com/Flicster/lman/internal/host"
	"github.com/Flicster/lman/internal/shellenv"
)

// Session is a language service bound to one host.
type Session struct {
	*app.Service
	close func() error
}

// Open connects to the local computer when server is empty, otherwise to server over SSH.
func Open(ctx context.Context, server string) (*Session, error) {
	if server == "" {
		return openLocal(ctx)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find home directory: %w", err)
	}
	ssh, err := host.DialSSH(ctx, server, filepath.Join(home, ".lman", "ssh"))
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", server, err)
	}
	env, err := ssh.Env(ctx)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("read environment of %s: %w", server, err), ssh.Close())
	}
	shell, err := shellenv.NewUnix(ssh, env, ssh.Platform())
	if err != nil {
		return nil, errors.Join(err, ssh.Close())
	}
	return &Session{
		Service: app.NewService(ssh, shell),
		close:   ssh.Close,
	}, nil
}

func openLocal(ctx context.Context) (*Session, error) {
	local := host.NewLocal()
	env, err := local.Env(ctx)
	if err != nil {
		return nil, fmt.Errorf("read environment: %w", err)
	}
	shell, err := localEnvironment(local, env)
	if err != nil {
		return nil, err
	}
	return &Session{
		Service: app.NewService(local, shell),
		close:   func() error { return nil },
	}, nil
}

// Close releases the connection to the host.
func (s *Session) Close() error {
	return s.close()
}
