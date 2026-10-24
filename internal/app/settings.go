package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"lman/internal/domain"
)

const maxServers = 10

// Settings keeps the preferences of the user: the theme and recently used servers.
type Settings struct {
	store PreferenceStore
	mu    sync.Mutex
}

// NewSettings reads and writes preferences in store.
func NewSettings(store PreferenceStore) *Settings {
	return &Settings{store: store}
}

// Preferences returns the stored preferences; the theme defaults to the system one.
func (s *Settings) Preferences(ctx context.Context) (Preferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(ctx)
}

// SetTheme stores the theme by name.
func (s *Settings) SetTheme(ctx context.Context, name string) (domain.Theme, error) {
	theme, err := domain.ParseTheme(name)
	if err != nil {
		return "", err
	}
	return theme, s.update(ctx, func(p *Preferences) { p.Theme = theme })
}

// RememberServer puts server first in the list of recently used servers.
func (s *Settings) RememberServer(ctx context.Context, server string) error {
	server = strings.TrimSpace(server)
	if server == "" {
		return nil
	}
	return s.update(ctx, func(p *Preferences) {
		p.Servers = slices.DeleteFunc(p.Servers, func(v string) bool { return v == server })
		p.Servers = append([]string{server}, p.Servers...)
		if len(p.Servers) > maxServers {
			p.Servers = p.Servers[:maxServers]
		}
	})
}

func (s *Settings) update(ctx context.Context, change func(*Preferences)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.load(ctx)
	if err != nil {
		return err
	}
	change(&p)
	if err := s.store.Save(ctx, p); err != nil {
		return fmt.Errorf("save preferences: %w", err)
	}
	return nil
}

func (s *Settings) load(ctx context.Context) (Preferences, error) {
	p, err := s.store.Load(ctx)
	if err != nil {
		return Preferences{}, fmt.Errorf("load preferences: %w", err)
	}
	if p.Theme == "" {
		p.Theme = domain.ThemeSystem
	}
	return p, nil
}
