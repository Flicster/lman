//go:build !windows

package host

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// loginEnv asks the user shell for the environment of a new terminal.
// Desktop applications on macOS start with a minimal PATH, so the process
// environment is used only when the shell does not answer.
func loginEnv(ctx context.Context) (map[string]string, error) {
	shellCtx, cancel := context.WithTimeout(ctx, loginShellTimeout)
	defer cancel()
	shell, ok := loginShell()
	if !ok {
		log.Printf("lman: SHELL is not an absolute path; using the process environment")
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return processEnv(), nil
	}
	// #nosec G204 G702 -- loginShell is an absolute path; envScript is a fixed program, not joined into the command line
	cmd := exec.Command(shell, "-ilc", envScript)
	var out boundedBuffer
	cmd.Stdout = &out
	// Stderr stays off the terminal. An inherited tty is what stops a shell
	// that is not in the foreground process group.
	if devNull, openErr := os.OpenFile(os.DevNull, os.O_WRONLY, 0); openErr == nil {
		defer func() {
			if err := devNull.Close(); err != nil {
				log.Printf("lman: close %s: %v", os.DevNull, err)
			}
		}()
		cmd.Stderr = devNull
	}
	if err := runCmd(shellCtx, cmd); err == nil {
		if data, overflow := out.result(); overflow == nil {
			if env := parseEnv(string(data)); env["PATH"] != "" {
				return env, nil
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// #nosec G706 -- loginShell rejected control characters; %q escapes the path
	log.Printf("lman: login shell %q did not provide PATH; using the process environment", shell)
	return processEnv(), nil
}

// loginShell is the user's login shell, or /bin/sh when SHELL is unset.
// A relative path or a control character is refused: SHELL is taken from the
// environment, and exec would run whatever file it names.
func loginShell() (string, bool) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return "/bin/sh", true
	}
	if !filepath.IsAbs(shell) {
		return "", false
	}
	for _, r := range shell {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return shell, true
}

// confirmRealDir refuses a path whose last component is a symbolic link.
// O_NOFOLLOW fails with ELOOP in that case, including on macOS.
func confirmRealDir(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		if errors.Is(err, unix.ELOOP) {
			return fmt.Errorf("refusing to remove %s: it is reached through a symbolic link", path)
		}
		if errors.Is(err, unix.ENOTDIR) {
			return fmt.Errorf("refusing to remove %s: it is not a directory", path)
		}
		return err
	}
	return unix.Close(fd)
}

func processEnv() map[string]string {
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

func replaceFile(tmp, dest string) error {
	return os.Rename(tmp, dest)
}
