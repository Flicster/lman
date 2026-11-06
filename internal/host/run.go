package host

import (
	"context"
	"errors"
	"os/exec"
)

// runCmd starts cmd and waits for it. When ctx ends, the process and the
// children it has started are killed: a version probe that forks must not
// keep running after Lman has given up on it.
func runCmd(ctx context.Context, cmd *exec.Cmd) error {
	prepareCmd(cmd)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	stop, release := trackCmd(cmd)
	defer release()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if err := stop(); err != nil && cmd.Process != nil {
			if killErr := cmd.Process.Kill(); killErr != nil {
				<-done
				return errors.Join(ctx.Err(), killErr)
			}
		}
		<-done
		return ctx.Err()
	}
}
