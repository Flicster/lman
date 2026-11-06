//go:build !windows

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLock takes the lock without waiting. It reports false when another process holds it.
func tryLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EINTR) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func unlockAndClose(f *os.File) error {
	err := unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return errors.Join(err, f.Close())
}
