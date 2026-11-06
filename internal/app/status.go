package app

import (
	"context"
	"fmt"
	"time"
)

// Status reports the application context: time, computer characteristics and the Lman version.
type Status struct {
	probe   SystemProbe
	version string
	now     func() time.Time
}

// NewStatus reports version as the installed Lman version.
func NewStatus(probe SystemProbe, version string) *Status {
	return &Status{probe: probe, version: version, now: time.Now}
}

// Get collects the current status.
func (s *Status) Get(ctx context.Context) (StatusInfo, error) {
	info, err := s.probe.Probe(ctx)
	if err != nil {
		return StatusInfo{}, fmt.Errorf("probe system: %w", err)
	}
	return StatusInfo{System: info, Version: s.version, Time: s.now()}, nil
}
