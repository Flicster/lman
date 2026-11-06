package shellenv

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/Flicster/lman/internal/domain"
)

const (
	blockBegin = "# >>> lman >>>"
	blockEnd   = "# <<< lman <<<"
)

// Files are the file operations of a Unix host. Paths use forward slashes.
type Files interface {
	// ReadFile returns an error wrapping fs.ErrNotExist for a missing file.
	ReadFile(ctx context.Context, name string) ([]byte, error)
	WriteFile(ctx context.Context, name string, data []byte, perm fs.FileMode) error
	MkdirAll(ctx context.Context, dir string) error
	// Symlink replaces link if it exists.
	Symlink(ctx context.Context, target, link string) error
	// Remove succeeds for a missing file.
	Remove(ctx context.Context, name string) error
	// Lock excludes other lman processes on the host from name until the returned
	// function runs. It waits until the lock is free or ctx ends.
	Lock(ctx context.Context, name string) (func() error, error)
	// EnsureBlock appends block when marker is not already in the file.
	// It does not read the file back and does not rewrite bytes that are already there.
	EnsureBlock(ctx context.Context, name, marker string, block []byte) error
}

// Unix keeps the configuration in ~/.lman: bin/ with symlinks, env.sh with exports
// and state.json. Shell startup files source env.sh through a marked block.
type Unix struct {
	files    Files
	dir      string
	profiles []string

	mu sync.Mutex
}

// NewUnix configures the shell of the user described by env: HOME and SHELL.
// HOME has to be absolute. An empty value would otherwise point Lman at /.lman.
func NewUnix(files Files, env map[string]string, p domain.Platform) (*Unix, error) {
	home := env["HOME"]
	if !validUnixHome(home) {
		return nil, fmt.Errorf("home directory %q is not an absolute path", home)
	}
	return &Unix{
		files:    files,
		dir:      home + "/.lman",
		profiles: profiles(home, path.Base(env["SHELL"]), p),
	}, nil
}

func validUnixHome(home string) bool {
	if home == "" || home == "/" || strings.ContainsRune(home, 0) || strings.ContainsAny(home, "\r\n") {
		return false
	}
	if !strings.HasPrefix(home, "/") {
		return false
	}
	// Clean collapses ".." and trailing slashes. A home that changes under Clean
	// is not the directory it looks like.
	return path.Clean(home) == home
}

// profiles are the startup files an interactive terminal of the shell reads.
// macOS Terminal starts bash as a login shell, which skips .bashrc.
func profiles(home, shell string, p domain.Platform) []string {
	switch shell {
	case "zsh":
		return []string{home + "/.zshrc"}
	case "bash":
		if p == domain.Darwin {
			return []string{home + "/.bashrc", home + "/.bash_profile"}
		}
		return []string{home + "/.bashrc"}
	}
	return []string{home + "/.profile"}
}

func (u *Unix) ShimDir() string {
	return u.dir + "/bin"
}

func (u *Unix) Links(ctx context.Context) (map[string]string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	st, err := u.load(ctx)
	if err != nil {
		return nil, err
	}
	return st.Links, nil
}

func (u *Unix) Link(ctx context.Context, links map[string]string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.locked(ctx, func() error {
		st, err := u.load(ctx)
		if err != nil {
			return err
		}
		if err := u.files.MkdirAll(ctx, u.ShimDir()); err != nil {
			return fmt.Errorf("create %s: %w", u.ShimDir(), err)
		}
		for _, name := range slices.Sorted(maps.Keys(links)) {
			if err := validLinkName(name); err != nil {
				return err
			}
			if err := u.files.Symlink(ctx, links[name], u.ShimDir()+"/"+name); err != nil {
				return fmt.Errorf("link %s: %w", name, err)
			}
			st.Links[name] = links[name]
		}
		return u.save(ctx, st)
	})
}

func (u *Unix) Unlink(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.locked(ctx, func() error {
		st, err := u.load(ctx)
		if err != nil {
			return err
		}
		for _, name := range names {
			if err := validLinkName(name); err != nil {
				return err
			}
			if err := u.files.Remove(ctx, u.ShimDir()+"/"+name); err != nil {
				return fmt.Errorf("unlink %s: %w", name, err)
			}
			delete(st.Links, name)
		}
		return u.save(ctx, st)
	})
}

func (u *Unix) Vars(ctx context.Context) (map[string]string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	st, err := u.load(ctx)
	if err != nil {
		return nil, err
	}
	return st.Vars, nil
}

func (u *Unix) SetVar(ctx context.Context, name, value string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.locked(ctx, func() error {
		st, err := u.load(ctx)
		if err != nil {
			return err
		}
		if value == "" {
			delete(st.Vars, name)
		} else {
			st.Vars[name] = value
		}
		return u.save(ctx, st)
	})
}

// locked runs change while no other lman process on the host changes the state.
// Each change reads, modifies and writes the state, so the whole sequence is one step.
func (u *Unix) locked(ctx context.Context, change func() error) error {
	if err := u.files.MkdirAll(ctx, u.dir); err != nil {
		return fmt.Errorf("create %s: %w", u.dir, err)
	}
	release, err := u.files.Lock(ctx, u.dir+"/state.lock")
	if err != nil {
		return fmt.Errorf("lock %s: %w", u.dir, err)
	}
	return errors.Join(change(), release())
}

func (u *Unix) load(ctx context.Context) (state, error) {
	data, err := u.files.ReadFile(ctx, u.dir+"/state.json")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return state{}, fmt.Errorf("read state: %w", err)
	}
	return decodeState(data)
}

func (u *Unix) save(ctx context.Context, st state) error {
	if err := u.files.MkdirAll(ctx, u.dir); err != nil {
		return fmt.Errorf("create %s: %w", u.dir, err)
	}
	data, err := st.encode()
	if err != nil {
		return err
	}
	// The state and the script hold values that may carry credentials, such as a
	// registry URL with a token, so only the user can read them.
	if err := u.files.WriteFile(ctx, u.dir+"/state.json", data, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := u.files.WriteFile(ctx, u.envFile(), []byte(u.script(st)), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", u.envFile(), err)
	}
	for _, profile := range u.profiles {
		if err := u.hook(ctx, profile); err != nil {
			return err
		}
	}
	return nil
}

func (u *Unix) envFile() string {
	return u.dir + "/env.sh"
}

// script is env.sh: the shim directory first in PATH, then the variables.
// A name that is not a valid variable, or that changes program loading, is skipped even
// when state.json holds it, so an edited state file cannot inject a loader variable.
func (u *Unix) script(st state) string {
	var b strings.Builder
	b.WriteString("# Generated by lman. Changes are overwritten.\n")
	fmt.Fprintf(&b, "case \":$PATH:\" in\n  *:%[1]s:*) ;;\n  *) export PATH=%[1]s:\"$PATH\" ;;\nesac\n", quote(u.ShimDir()))
	for _, name := range slices.Sorted(maps.Keys(st.Vars)) {
		if domain.ValidateVarName(name) != nil || domain.LoaderVar(name) {
			continue
		}
		fmt.Fprintf(&b, "export %s=%s\n", name, quote(st.Vars[name]))
	}
	return b.String()
}

// hook appends the block that sources env.sh, once. Placed last, it wins over
// PATH changes made earlier in the file. The profile itself is not rewritten.
func (u *Unix) hook(ctx context.Context, profile string) error {
	block := fmt.Sprintf("\n%s\n[ -f %[2]s ] && . %[2]s\n%s\n", blockBegin, quote(u.envFile()), blockEnd)
	if err := u.files.EnsureBlock(ctx, profile, blockBegin, []byte(block)); err != nil {
		return fmt.Errorf("write %s: %w", profile, err)
	}
	return nil
}

func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
