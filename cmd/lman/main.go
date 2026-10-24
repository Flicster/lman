// Command lman manages programming languages from the terminal.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"lman/internal/app"
	"lman/internal/cli"
	"lman/internal/config"
	"lman/internal/session"
	"lman/internal/sysinfo"
)

// version is set with -ldflags "-X main.version=...".
// Empty and "dev" use the module version recorded by go install.
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	path, err := config.DefaultPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lman: %s\n", err)
		return 1
	}
	open := func(ctx context.Context, server string) (cli.Manager, func() error, error) {
		s, err := session.Open(ctx, server)
		if err != nil {
			return nil, nil, err
		}
		return s, s.Close, nil
	}
	c := cli.New(open, app.NewSettings(config.NewStore(path)), app.NewStatus(sysinfo.New(), app.AppVersion(version)),
		os.Stdin, os.Stdout, os.Stderr)
	return c.Run(ctx, os.Args[1:])
}
