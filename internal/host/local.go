package host

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Flicster/lman/internal/app"
	"github.com/Flicster/lman/internal/domain"
	"github.com/Flicster/lman/internal/filelock"
)

// waitDelay bounds the wait for output pipes after a started program exits.
const waitDelay = time.Second

// loginShellTimeout bounds asking the login shell for its environment.
// The same limit applies to a server: a profile that never returns must not
// hold the command open until the user interrupts it.
const loginShellTimeout = 10 * time.Second

// goosWindows is the runtime.GOOS name of Windows.
const goosWindows = "windows"

// Local is the computer Lman runs on.
type Local struct{}

// NewLocal returns the local computer.
func NewLocal() *Local {
	return &Local{}
}

func (l *Local) Name() string {
	return "local"
}

func (l *Local) Platform() domain.Platform {
	return domain.Platform(runtime.GOOS)
}

func (l *Local) Env(ctx context.Context) (map[string]string, error) {
	return loginEnv(ctx)
}

func (l *Local) Glob(_ context.Context, patterns []string) ([][]string, error) {
	out := make([][]string, len(patterns))
	for i, p := range patterns {
		files, err := filepath.Glob(filepath.FromSlash(p))
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", p, err)
		}
		for _, f := range files {
			out[i] = append(out[i], filepath.ToSlash(f))
		}
	}
	return out, nil
}

func (l *Local) Inspect(_ context.Context, paths []string) ([]app.FileInfo, error) {
	out := make([]app.FileInfo, 0, len(paths))
	for _, p := range paths {
		real, err := filepath.EvalSymlinks(filepath.FromSlash(p))
		if err != nil {
			// One broken link or an unreadable directory must not hide every language.
			if !errors.Is(err, fs.ErrNotExist) {
				log.Printf("lman: skip %s: %v", p, err)
			}
			continue
		}
		st, err := os.Stat(real)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				log.Printf("lman: skip %s: %v", real, err)
			}
			continue
		}
		if st.IsDir() {
			continue
		}
		out = append(out, app.FileInfo{Path: p, RealPath: filepath.ToSlash(real), InstalledAt: createdAt(st)})
	}
	return out, nil
}

func (l *Local) Run(ctx context.Context, c app.Command) ([]byte, error) {
	if err := runnable(c); err != nil {
		return nil, err
	}
	// #nosec G204 -- runnable accepts an absolute path or a bare name; arguments are not passed through a shell
	cmd := exec.Command(filepath.FromSlash(c.Path), c.Args...)
	// A project in the process directory can redirect a version probe: go.mod
	// downloads a toolchain, rust-toolchain.toml and global.json can run a program
	// that lives in that project. The filesystem root has no parent to walk into.
	cmd.Dir = neutralWorkDir()
	if c.Env != nil {
		cmd.Env = flatten(c.Env)
	}
	var out boundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := runCmd(ctx, cmd)
	data, overflow := out.result()
	if overflow != nil {
		return data, fmt.Errorf("run %s: %w", c.Path, overflow)
	}
	return data, err
}

// Lock excludes other lman processes from name until the returned function runs.
func (l *Local) Lock(ctx context.Context, name string) (func() error, error) {
	return filelock.Acquire(ctx, filepath.FromSlash(name))
}

// RemoveAll deletes dir. The plan named the real directory, so a path that is now a
// symbolic link, or leads through one, is refused rather than followed.
func (l *Local) RemoveAll(_ context.Context, dir string) error {
	path := filepath.FromSlash(dir)
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if !sameDir(real, path) {
		return fmt.Errorf("refusing to remove %s: it is reached through a symbolic link", dir)
	}
	// Check again immediately before the delete. EvalSymlinks and RemoveAll are
	// separate calls, so a symlink swapped in between is refused here too.
	if err := confirmRealDir(path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	// macOS and Windows compare paths without regard to case.
	if runtime.GOOS == goosWindows || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (l *Local) ReadFile(_ context.Context, name string) ([]byte, error) {
	return os.ReadFile(filepath.FromSlash(name))
}

func (l *Local) WriteFile(_ context.Context, name string, data []byte, perm fs.FileMode) error {
	return writeAtomic(filepath.FromSlash(name), data, perm.Perm())
}

// EnsureBlock appends block when marker is not already in the file.
// Existing bytes stay where they are: the shell profile is not rewritten.
func (l *Local) EnsureBlock(_ context.Context, name, marker string, block []byte) (err error) {
	path, err := profilePath(name)
	if err != nil {
		return err
	}
	// #nosec G304 -- profilePath allows only an absolute cleaned path; this is the user's shell startup file, not an included path
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if strings.Contains(string(data), marker) {
		return nil
	}
	// 0600 applies only when the file is created. An existing profile keeps its mode.
	// #nosec G304 -- same path as the read above
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		if _, err = f.Write([]byte("\n")); err != nil {
			return err
		}
	}
	_, err = f.Write(block)
	return err
}

// profilePath is the absolute path of a shell startup file. A relative path or a
// ".." component would append the hook somewhere other than the named profile.
func profilePath(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) || strings.ContainsAny(name, "\r\n") {
		return "", fmt.Errorf("write %s: invalid profile path", name)
	}
	raw := filepath.FromSlash(name)
	path := filepath.Clean(raw)
	if path != raw || !filepath.IsAbs(path) {
		return "", fmt.Errorf("write %s: profile path must be absolute", name)
	}
	return path, nil
}

// writeAtomic replaces path only after the new contents are on disk.
// An existing file, including the target of a symlink, keeps its mode.
func writeAtomic(path string, data []byte, perm fs.FileMode) (err error) {
	dest, mode, exists, err := writeDest(path)
	if err != nil {
		return err
	}
	if !exists {
		mode = perm
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".lman-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	done := false
	defer func() {
		if !done {
			err = errors.Join(err, os.Remove(tmpName))
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		err = errors.Join(err, tmp.Close())
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmpName, mode); err != nil {
		return err
	}
	if err = replaceFile(tmpName, dest); err != nil {
		return err
	}
	done = true
	return nil
}

// writeDest is the file that should be replaced. A symlink is followed, so the
// link itself stays in place.
func writeDest(path string) (dest string, mode fs.FileMode, exists bool, err error) {
	st, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return path, 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", 0, false, err
		}
		st, err = os.Stat(resolved)
		if err != nil {
			return "", 0, false, err
		}
		return resolved, st.Mode().Perm(), true, nil
	}
	return path, st.Mode().Perm(), true, nil
}

func (l *Local) MkdirAll(_ context.Context, dir string) error {
	path := filepath.FromSlash(dir)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	// MkdirAll leaves an existing directory alone. ~/.lman must not be world-readable.
	// #nosec G302 -- a directory needs the search bit; 0600 would make ~/.lman unreachable
	return os.Chmod(path, 0o700)
}

// runnable rejects a program that is not a bare name (dpkg, rpm) or an absolute path.
// A relative path with a slash would run whatever file sits next to the working directory.
func runnable(c app.Command) error {
	path := c.Path
	if path == "" || strings.ContainsRune(path, 0) || strings.ContainsAny(path, "\r\n") {
		return fmt.Errorf("run: invalid program path")
	}
	if strings.ContainsAny(path, `/\`) {
		cleaned := filepath.Clean(filepath.FromSlash(path))
		if !filepath.IsAbs(cleaned) {
			return fmt.Errorf("run %s: path must be absolute", path)
		}
	}
	for _, arg := range c.Args {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("run %s: invalid argument", path)
		}
	}
	return nil
}

func (l *Local) Symlink(ctx context.Context, target, link string) error {
	if err := l.Remove(ctx, link); err != nil {
		return err
	}
	return os.Symlink(filepath.FromSlash(target), filepath.FromSlash(link))
}

// neutralWorkDir is the working directory of every program Lman starts.
func neutralWorkDir() string {
	if runtime.GOOS != goosWindows {
		return "/"
	}
	drive := os.Getenv("SystemDrive")
	if len(drive) != 2 || drive[1] != ':' {
		return `C:\`
	}
	return drive + `\`
}

func (l *Local) Remove(_ context.Context, name string) error {
	err := os.Remove(filepath.FromSlash(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
