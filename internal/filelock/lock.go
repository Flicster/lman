// Package filelock makes one lman process at a time change the shared state of a computer:
// the CLI and the desktop application both write ~/.lman and the shell profile.
package filelock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const retryEvery = 50 * time.Millisecond

// Acquire waits until it holds an exclusive lock on path, creating the file if needed,
// or until ctx ends. The lock belongs to the open file, so the operating system releases
// it when the process exits, even after a crash. The returned function releases the lock.
func Acquire(ctx context.Context, path string) (func() error, error) {
	if path == "" || !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return nil, fmt.Errorf("lock path must be absolute")
	}
	// #nosec G304 -- this file is the lock lman creates for itself, mode 0600; its contents are never executed or included
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		ok, err := tryLock(f)
		if err != nil {
			return nil, errors.Join(err, f.Close())
		}
		if ok {
			return func() error { return unlockAndClose(f) }, nil
		}
		select {
		case <-ctx.Done():
			return nil, errors.Join(ctx.Err(), f.Close())
		case <-time.After(retryEvery):
		}
	}
}
