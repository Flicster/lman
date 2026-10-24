// Command lman-desktop is the window of Lman. Build it with the Wails CLI
// or `go build -tags desktop,production`.
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"

	"lman/internal/app"
	"lman/internal/config"
	"lman/internal/desktop"
	"lman/internal/session"
	"lman/internal/sysinfo"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is set with -ldflags "-X main.version=...".
// Empty and "dev" use the module version recorded by go install.
var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return fmt.Errorf("open assets: %w", err)
	}
	path, err := config.DefaultPath()
	if err != nil {
		return err
	}
	open := func(ctx context.Context, server string) (desktop.Manager, func() error, error) {
		s, err := session.Open(ctx, server)
		if err != nil {
			return nil, nil, err
		}
		return s, s.Close, nil
	}
	a := desktop.New(open, app.NewSettings(config.NewStore(path)), app.NewStatus(sysinfo.New(), app.AppVersion(version)))
	return desktop.Run(a, dist)
}
