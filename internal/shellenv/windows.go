//go:build windows

package shellenv

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const userEnvKey = `Environment`

// Windows keeps the configuration in %USERPROFILE%\.lman: bin\ with .cmd shims and
// state.json. PATH and variables are user variables in the registry.
type Windows struct {
	dir string
	mu  sync.Mutex
}

// NewWindows configures the user described by env: USERPROFILE.
func NewWindows(env map[string]string) *Windows {
	return &Windows{dir: filepath.ToSlash(env["USERPROFILE"]) + "/.lman"}
}

func (w *Windows) ShimDir() string {
	return w.dir + "/bin"
}

func (w *Windows) Links(context.Context) (map[string]string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	st, err := w.load()
	if err != nil {
		return nil, err
	}
	return st.Links, nil
}

func (w *Windows) Link(_ context.Context, links map[string]string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	st, err := w.load()
	if err != nil {
		return err
	}
	bin := filepath.FromSlash(w.ShimDir())
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", bin, err)
	}
	for name, target := range links {
		shim := fmt.Sprintf("@\"%s\" %%*\r\n", filepath.FromSlash(target))
		if err := os.WriteFile(filepath.Join(bin, name+".cmd"), []byte(shim), 0o644); err != nil {
			return fmt.Errorf("write shim %s: %w", name, err)
		}
		st.Links[name] = target
	}
	if err := prependUserPath(bin); err != nil {
		return err
	}
	return w.save(st)
}

func (w *Windows) Unlink(_ context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	st, err := w.load()
	if err != nil {
		return err
	}
	for _, name := range names {
		err := os.Remove(filepath.Join(filepath.FromSlash(w.ShimDir()), name+".cmd"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove shim %s: %w", name, err)
		}
		delete(st.Links, name)
	}
	return w.save(st)
}

func (w *Windows) Vars(context.Context) (map[string]string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	st, err := w.load()
	if err != nil {
		return nil, err
	}
	return st.Vars, nil
}

func (w *Windows) SetVar(_ context.Context, name, value string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	st, err := w.load()
	if err != nil {
		return err
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open user environment: %w", err)
	}
	defer k.Close()
	if value == "" {
		if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("delete %s: %w", name, err)
		}
		delete(st.Vars, name)
	} else {
		if err := k.SetStringValue(name, value); err != nil {
			return fmt.Errorf("set %s: %w", name, err)
		}
		st.Vars[name] = value
	}
	broadcastEnvChange()
	return w.save(st)
}

func (w *Windows) load() (state, error) {
	data, err := os.ReadFile(filepath.FromSlash(w.dir + "/state.json"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return state{}, fmt.Errorf("read state: %w", err)
	}
	return decodeState(data)
}

func (w *Windows) save(st state) error {
	if err := os.MkdirAll(filepath.FromSlash(w.dir), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", w.dir, err)
	}
	data, err := st.encode()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.FromSlash(w.dir+"/state.json"), data, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

// prependUserPath puts dir first in the user PATH unless it is already there.
func prependUserPath(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open user environment: %w", err)
	}
	defer k.Close()
	current, _, err := k.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("read user PATH: %w", err)
	}
	for _, entry := range strings.Split(current, ";") {
		if strings.EqualFold(strings.TrimRight(entry, `\`), dir) {
			return nil
		}
	}
	value := dir
	if current != "" {
		value += ";" + current
	}
	if err := k.SetExpandStringValue("Path", value); err != nil {
		return fmt.Errorf("write user PATH: %w", err)
	}
	broadcastEnvChange()
	return nil
}

// broadcastEnvChange tells Explorer to reload the environment for new terminals.
// Delivery is best effort: a missed message only delays the change until the next sign-in.
func broadcastEnvChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001a
		smtoAbortIfHung = 0x0002
	)
	param, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	proc := syscall.NewLazyDLL("user32.dll").NewProc("SendMessageTimeoutW")
	var result uintptr
	proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(param)), smtoAbortIfHung, 1000, uintptr(unsafe.Pointer(&result)))
}
