package host

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"lman/internal/app"
	"lman/internal/domain"
)

// waitDelay bounds the wait for output pipes after a started program exits.
const waitDelay = time.Second

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
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", p, err)
		}
		st, err := os.Stat(real)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", real, err)
		}
		if st.IsDir() {
			continue
		}
		out = append(out, app.FileInfo{Path: p, RealPath: filepath.ToSlash(real), InstalledAt: createdAt(st)})
	}
	return out, nil
}

func (l *Local) Run(ctx context.Context, c app.Command) ([]byte, error) {
	cmd := exec.CommandContext(ctx, filepath.FromSlash(c.Path), c.Args...)
	if c.Env != nil {
		cmd.Env = flatten(c.Env)
	}
	cmd.WaitDelay = waitDelay
	hideWindow(cmd)
	return cmd.CombinedOutput()
}

func (l *Local) RemoveAll(_ context.Context, dir string) error {
	return os.RemoveAll(filepath.FromSlash(dir))
}

func (l *Local) ReadFile(_ context.Context, name string) ([]byte, error) {
	return os.ReadFile(filepath.FromSlash(name))
}

func (l *Local) WriteFile(_ context.Context, name string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(filepath.FromSlash(name), data, perm)
}

func (l *Local) MkdirAll(_ context.Context, dir string) error {
	return os.MkdirAll(filepath.FromSlash(dir), 0o755)
}

func (l *Local) Symlink(ctx context.Context, target, link string) error {
	if err := l.Remove(ctx, link); err != nil {
		return err
	}
	return os.Symlink(filepath.FromSlash(target), filepath.FromSlash(link))
}

func (l *Local) Remove(_ context.Context, name string) error {
	err := os.Remove(filepath.FromSlash(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
