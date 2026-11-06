package filelock

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAcquireWaitsForTheHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")

	release, err := Acquire(context.Background(), path)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = Acquire(ctx, path)
	require.ErrorIs(t, err, context.DeadlineExceeded, "a second holder waits while the first one holds the lock")

	require.NoError(t, release())

	again, err := Acquire(context.Background(), path)
	require.NoError(t, err)
	require.NoError(t, again())
}

func TestAcquireRejectsARelativePath(t *testing.T) {
	_, err := Acquire(context.Background(), "state.lock")
	require.Error(t, err)
}
