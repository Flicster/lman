package host

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

const (
	machineEnvKey = `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
	userEnvKey    = `Environment`
)

// loginEnv is the environment a new terminal gets: the process environment
// updated with the current machine and user variables from the registry.
func loginEnv(context.Context) (map[string]string, error) {
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			env[k] = v
		}
	}
	machine := readEnvKey(registry.LOCAL_MACHINE, machineEnvKey)
	user := readEnvKey(registry.CURRENT_USER, userEnvKey)
	for k, v := range user {
		if !strings.EqualFold(k, "path") {
			env[k] = v
		}
	}
	paths := []string{lookupFold(machine, "Path"), lookupFold(user, "Path")}
	if joined := strings.Trim(strings.Join(paths, ";"), ";"); joined != "" {
		for k := range env {
			if strings.EqualFold(k, "path") {
				delete(env, k)
			}
		}
		env["PATH"] = joined
	}
	return env, nil
}

// readEnvKey returns expanded variables of a registry key; a missing key gives none.
func readEnvKey(root registry.Key, path string) map[string]string {
	out := make(map[string]string)
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return out
	}
	defer k.Close()
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return out
	}
	for _, name := range names {
		v, _, err := k.GetStringValue(name)
		if err != nil {
			continue
		}
		if expanded, err := registry.ExpandString(v); err == nil {
			v = expanded
		}
		out[name] = v
	}
	return out
}

func lookupFold(env map[string]string, key string) string {
	for k, v := range env {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// createdAt is the file creation time kept by NTFS.
func createdAt(st fs.FileInfo) time.Time {
	sys, ok := st.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return st.ModTime()
	}
	return time.Unix(0, sys.CreationTime.Nanoseconds())
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
