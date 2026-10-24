// Package sysinfo reads the characteristics of the local computer.
package sysinfo

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"

	"lman/internal/app"
)

// Probe reads system information through gopsutil.
type Probe struct{}

// New returns a Probe of the local computer.
func New() *Probe {
	return &Probe{}
}

func (p *Probe) Probe(ctx context.Context) (app.SystemInfo, error) {
	h, err := host.InfoWithContext(ctx)
	if err != nil {
		return app.SystemInfo{}, fmt.Errorf("read host info: %w", err)
	}
	m, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return app.SystemInfo{}, fmt.Errorf("read memory: %w", err)
	}
	cores, err := cpu.CountsWithContext(ctx, true)
	if err != nil {
		return app.SystemInfo{}, fmt.Errorf("count cores: %w", err)
	}
	cpus, err := cpu.InfoWithContext(ctx)
	if err != nil {
		return app.SystemInfo{}, fmt.Errorf("read cpu: %w", err)
	}
	info := app.SystemInfo{
		Hostname:    h.Hostname,
		OS:          osName(h.OS, h.Platform),
		OSVersion:   h.PlatformVersion,
		Arch:        runtime.GOARCH,
		Cores:       cores,
		MemoryBytes: m.Total,
	}
	if len(cpus) > 0 {
		info.CPU = strings.TrimSpace(cpus[0].ModelName)
	}
	return info, nil
}

func osName(goos, platform string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "windows":
		if platform != "" {
			return platform
		}
		return "Windows"
	}
	if platform == "" {
		return goos
	}
	return strings.ToUpper(platform[:1]) + platform[1:]
}
