// Package config stores the preferences of the user as a JSON file.
package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"lman/internal/app"
)

// Store keeps preferences in one file.
type Store struct {
	path string
}

// NewStore keeps preferences in path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// DefaultPath is preferences.json in the user configuration directory.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find config directory: %w", err)
	}
	return filepath.Join(dir, "lman", "preferences.json"), nil
}

// Load returns empty preferences when the file does not exist yet.
func (s *Store) Load(context.Context) (app.Preferences, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return app.Preferences{}, nil
	}
	if err != nil {
		return app.Preferences{}, err
	}
	var p app.Preferences
	if err := json.Unmarshal(data, &p); err != nil {
		return app.Preferences{}, fmt.Errorf("decode %s: %w", s.path, err)
	}
	return p, nil
}

// Save writes through a temporary file, so a crash never leaves a half-written file.
func (s *Store) Save(_ context.Context, p app.Preferences) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preferences: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
