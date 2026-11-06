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

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/Flicster/lman/internal/filelock"
)

const userEnvKey = `Environment`

// Windows keeps the configuration in %USERPROFILE%\.lman: bin\ with links to the
// real programs and state.json. PATH and variables are user variables in the registry.
// Links are symlinks, or hard links when symlinks are not allowed. A .cmd wrapper is not
// used: cmd.exe would re-parse arguments and expand %variables% in the target path.
type Windows struct {
	dir string
	mu  sync.Mutex
}

// NewWindows configures the user described by env: USERPROFILE.
// USERPROFILE has to be absolute. An empty value would otherwise point Lman at /.lman.
func NewWindows(env map[string]string) (*Windows, error) {
	home := env["USERPROFILE"]
	if home == "" || strings.ContainsRune(home, 0) || !filepath.IsAbs(home) {
		return nil, fmt.Errorf("home directory %q is not an absolute path", home)
	}
	return &Windows{dir: filepath.ToSlash(home) + "/.lman"}, nil
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

func (w *Windows) Link(ctx context.Context, links map[string]string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.locked(ctx, func() error {
		st, err := w.load()
		if err != nil {
			return err
		}
		bin := filepath.FromSlash(w.ShimDir())
		if err := os.MkdirAll(bin, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", bin, err)
		}
		for name, target := range links {
			if err := validLinkName(name); err != nil {
				return err
			}
			if err := installShim(bin, name, filepath.FromSlash(target)); err != nil {
				return err
			}
			st.Links[name] = target
		}
		if err := prependUserPath(bin); err != nil {
			return err
		}
		return w.save(st)
	})
}

func (w *Windows) Unlink(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.locked(ctx, func() error {
		st, err := w.load()
		if err != nil {
			return err
		}
		bin := filepath.FromSlash(w.ShimDir())
		for _, name := range names {
			if err := validLinkName(name); err != nil {
				return err
			}
			if err := removeShim(bin, name); err != nil {
				return err
			}
			delete(st.Links, name)
		}
		return w.save(st)
	})
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

func (w *Windows) SetVar(ctx context.Context, name, value string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.locked(ctx, func() error {
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
	})
}

// locked runs change while no other lman process of the user changes the state.
// Callers hold w.mu, which serializes the goroutines of this process.
func (w *Windows) locked(ctx context.Context, change func() error) error {
	if err := os.MkdirAll(filepath.FromSlash(w.dir), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", w.dir, err)
	}
	release, err := filelock.Acquire(ctx, filepath.FromSlash(w.dir+"/state.lock"))
	if err != nil {
		return fmt.Errorf("lock %s: %w", w.dir, err)
	}
	return errors.Join(change(), release())
}

func (w *Windows) load() (state, error) {
	data, err := os.ReadFile(filepath.FromSlash(w.dir + "/state.json"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return state{}, fmt.Errorf("read state: %w", err)
	}
	return decodeState(data)
}

func (w *Windows) save(st state) error {
	if err := os.MkdirAll(filepath.FromSlash(w.dir), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", w.dir, err)
	}
	data, err := st.encode()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.FromSlash(w.dir+"/state.json"), data, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

// installShim points name at target with a symlink, so the toolchain that is
// installed stays the program that runs. cmd.exe never parses the arguments.
func installShim(bin, name, target string) error {
	destName := shimFile(name, target)
	dest := filepath.Join(bin, destName)
	if err := os.Remove(dest); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("link %s: %w", name, err)
	}
	// A hard link would keep a private copy after the toolchain is upgraded and
	// would load DLLs from ~/.lman/bin instead of the install directory.
	if err := os.Symlink(target, dest); err != nil {
		if removeErr := os.Remove(dest); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			return fmt.Errorf("link %s: %w", name, errors.Join(err, removeErr))
		}
		return fmt.Errorf("link %s: a symlink is required (%w)", name, err)
	}
	for _, ext := range []string{".exe", ".cmd", ".bat", ".com"} {
		other := name + ext
		if other == destName {
			continue
		}
		err := os.Remove(filepath.Join(bin, other))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove shim %s: %w", other, err)
		}
	}
	return nil
}

func removeShim(bin, name string) error {
	for _, ext := range []string{".exe", ".cmd", ".bat", ".com"} {
		err := os.Remove(filepath.Join(bin, name+ext))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove shim %s: %w", name, err)
		}
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
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	var result uintptr
	proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(param)), smtoAbortIfHung, 1000, uintptr(unsafe.Pointer(&result)))
}
