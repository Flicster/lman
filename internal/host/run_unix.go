//go:build !windows

package host

import (
	"errors"
	"os/exec"
	"syscall"
)

func prepareCmd(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// A new session has no controlling terminal. Setpgid alone leaves the
	// terminal in place, and the login shell then stops on SIGTTOU the moment
	// it writes a prompt. setsid makes this process the leader of a new
	// process group, which is what kill(-pid) reaches, including children
	// that did not leave the group. Job control stays off without a terminal,
	// so a shell does not move its children elsewhere.
	cmd.SysProcAttr.Setsid = true
	cmd.WaitDelay = waitDelay
}

func trackCmd(cmd *exec.Cmd) (stop func() error, release func()) {
	return func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}, func() {}
}
