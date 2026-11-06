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

	"github.com/Flicster/lman/internal/app"
	"github.com/Flicster/lman/internal/filelock"
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

// Save writes through a new temporary file, so a crash never leaves a half-written file.
// The name is random and the file is created exclusively: a symlink left at a fixed
// ".tmp" path is not followed. A lock keeps the CLI and the window from losing updates.
func (s *Store) Save(ctx context.Context, p app.Preferences) (err error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preferences: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	release, err := filelock.Acquire(ctx, s.path+".lock")
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, release())
	}()
	tmp, err := os.CreateTemp(dir, ".preferences-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	done := false
	defer func() {
		if done {
			return
		}
		err = errors.Join(err, os.Remove(tmpName))
	}()
	if _, err = tmp.Write(append(data, '\n')); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	// The file names servers the user connects to, which is not for other users.
	if err = os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if err = os.Rename(tmpName, s.path); err != nil {
		return err
	}
	done = true
	return nil
}
