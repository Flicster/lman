// Package cli is the terminal interface of Lman.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"lman/internal/app"
	"lman/internal/domain"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

const usage = `Usage: lman [--remote <server>] <command>

Languages:
  lman, lman list                          installed languages
  lman known                               languages Lman can detect
  lman show <language>                     versions, paths, install methods and tool hints
  lman manual <language>                   built-in manual of the active version
  lman env <language>                      environment variables of the language
  lman env set <language> <name> <value>   set a variable for new terminals ("" unsets it)
  lman use <language> <version>            make a version active in new terminals
  lman remove [-y] <language> [<version>]  remove a version, or the language with all versions

This computer:
  lman status                              time, system characteristics and Lman version
  lman theme light|dark|system             theme of the desktop application
  lman help                                this list

--remote <server> runs a language command on a server over SSH: user@host or an alias
from ~/.ssh/config. The server must accept a key, password prompts are disabled.
Languages are named by id or alias: go, python, javascript (js, node), java, rust, ruby, php,
c (gcc, clang), cpp, csharp (dotnet), vb.
`

// Manager is the language service of one host.
type Manager interface {
	Host() string
	Languages(ctx context.Context) ([]domain.Language, error)
	Language(ctx context.Context, id string) (domain.Language, error)
	Manual(ctx context.Context, id string) (domain.Manual, error)
	Env(ctx context.Context, id string) ([]domain.EnvVar, error)
	SetEnv(ctx context.Context, id, name, value string) error
	Use(ctx context.Context, id, version string) (app.Activation, error)
	RemoveVersion(ctx context.Context, id, version string) error
	RemoveLanguage(ctx context.Context, id string) error
}

// Opener connects to the local computer for an empty server, otherwise to the server.
type Opener func(ctx context.Context, server string) (Manager, func() error, error)

// Settings changes preferences of the desktop application.
type Settings interface {
	SetTheme(ctx context.Context, name string) (domain.Theme, error)
}

// Status reports the application context.
type Status interface {
	Get(ctx context.Context) (app.StatusInfo, error)
}

// CLI runs one command per call.
type CLI struct {
	open     Opener
	settings Settings
	status   Status
	in       *bufio.Reader
	out      io.Writer
	errOut   io.Writer
}

// New reads confirmations from in and writes results to out and problems to errOut.
func New(open Opener, settings Settings, status Status, in io.Reader, out, errOut io.Writer) *CLI {
	return &CLI{open: open, settings: settings, status: status, in: bufio.NewReader(in), out: out, errOut: errOut}
}

// usageError is a mistake in the command line rather than a failed action.
type usageError struct {
	msg string
}

func (e *usageError) Error() string {
	return e.msg
}

func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

// Run executes args without the program name and returns the exit code.
func (c *CLI) Run(ctx context.Context, args []string) int {
	err := c.run(ctx, args)
	var ue *usageError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &ue):
		fmt.Fprintf(c.errOut, "lman: %s\n\n%s", ue.msg, usage)
		return exitUsage
	default:
		fmt.Fprintf(c.errOut, "lman: %s\n", err)
		return exitError
	}
}

func (c *CLI) run(ctx context.Context, args []string) error {
	server, args, err := splitRemote(args)
	if err != nil {
		return err
	}
	command := "list"
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	switch command {
	case "help", "-h", "--help":
		fmt.Fprint(c.out, usage)
		return nil
	case "known":
		return c.known(args)
	case "status", "theme":
		if server != "" {
			return usagef("%s runs on this computer, drop --remote", command)
		}
		if command == "status" {
			return c.showStatus(ctx, args)
		}
		return c.setTheme(ctx, args)
	case "list", "show", "manual", "env", "use", "remove":
		return c.withManager(ctx, server, func(m Manager) error {
			return c.language(ctx, m, command, args)
		})
	}
	return usagef("unknown command %q", command)
}

func splitRemote(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", args, nil
	}
	if value, ok := strings.CutPrefix(args[0], "--remote="); ok {
		if value == "" {
			return "", nil, usagef("--remote needs a server")
		}
		return value, args[1:], nil
	}
	if args[0] != "--remote" {
		return "", args, nil
	}
	if len(args) < 2 || args[1] == "" {
		return "", nil, usagef("--remote needs a server")
	}
	return args[1], args[2:], nil
}

func (c *CLI) withManager(ctx context.Context, server string, fn func(Manager) error) (err error) {
	m, closeFn, err := c.open(ctx, server)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, closeFn())
	}()
	return fn(m)
}

func (c *CLI) language(ctx context.Context, m Manager, command string, args []string) error {
	switch command {
	case "list":
		if len(args) != 0 {
			return usagef("list takes no arguments")
		}
		return c.list(ctx, m)
	case "show":
		if len(args) != 1 {
			return usagef("show needs a language")
		}
		return c.show(ctx, m, args[0])
	case "manual":
		if len(args) != 1 {
			return usagef("manual needs a language")
		}
		return c.manual(ctx, m, args[0])
	case "env":
		if len(args) > 0 && args[0] == "set" {
			if len(args) != 4 {
				return usagef("env set needs a language, a name and a value")
			}
			return c.setEnv(ctx, m, args[1], args[2], args[3])
		}
		if len(args) != 1 {
			return usagef("env needs a language")
		}
		return c.env(ctx, m, args[0])
	case "use":
		if len(args) != 2 {
			return usagef("use needs a language and a version")
		}
		return c.use(ctx, m, args[0], args[1])
	case "remove":
		return c.remove(ctx, m, args)
	}
	return usagef("unknown command %q", command)
}
