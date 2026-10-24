package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"lman/internal/app"
	"lman/internal/domain"
)

const (
	connectTimeout = "10"
	// sshFailure is the exit status of the ssh client itself, as opposed to the remote command.
	sshFailure = 255
	// missingExit tells that a remote file does not exist.
	missingExit = 3
)

// SSH is a Linux or macOS server reached with the system ssh client, so
// ~/.ssh/config, keys and the agent work as in the terminal. Password prompts
// are disabled: the server must accept a key.
type SSH struct {
	target     string
	controlDir string
	platform   domain.Platform
}

// DialSSH checks the connection to target (user@host or an alias from ~/.ssh/config).
// Connections are shared through a control socket in controlDir where the client supports it.
func DialSSH(ctx context.Context, target, controlDir string) (*SSH, error) {
	target = strings.TrimSpace(target)
	if target == "" || strings.HasPrefix(target, "-") {
		return nil, fmt.Errorf("invalid server %q", target)
	}
	s := &SSH{target: target}
	if runtime.GOOS != "windows" {
		if err := os.MkdirAll(controlDir, 0o700); err != nil {
			return nil, fmt.Errorf("create ssh control directory: %w", err)
		}
		s.controlDir = controlDir
	}
	out, err := s.script(ctx, "uname -s")
	if err != nil {
		return nil, err
	}
	switch strings.TrimSpace(string(out)) {
	case "Linux":
		s.platform = domain.Linux
	case "Darwin":
		s.platform = domain.Darwin
	default:
		return nil, fmt.Errorf("server %s: unsupported system %q", target, strings.TrimSpace(string(out)))
	}
	return s, nil
}

// Close ends the shared connection.
func (s *SSH) Close() error {
	if s.controlDir == "" {
		return nil
	}
	cmd := exec.Command("ssh", append(s.options(), "-O", "exit", s.target)...)
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		// No shared connection is running: nothing to close.
		return nil
	}
	return err
}

func (s *SSH) Name() string {
	return s.target
}

func (s *SSH) Platform() domain.Platform {
	return s.platform
}

func (s *SSH) Env(ctx context.Context) (map[string]string, error) {
	out, err := s.script(ctx, `exec "${SHELL:-/bin/sh}" -ilc `+quote(envScript)+` </dev/null 2>/dev/null`)
	if err != nil {
		return nil, err
	}
	return parseEnv(string(out)), nil
}

func (s *SSH) Glob(ctx context.Context, patterns []string) ([][]string, error) {
	out, err := s.script(ctx, globScript(patterns))
	if err != nil {
		return nil, err
	}
	result := make([][]string, len(patterns))
	for _, line := range lines(out) {
		idx, file, ok := strings.Cut(line, "\t")
		i, err := strconv.Atoi(idx)
		if !ok || err != nil || i < 0 || i >= len(patterns) {
			continue
		}
		result[i] = append(result[i], file)
	}
	return result, nil
}

// inspectScript prints path, resolved path and the birth time, or the inode change time
// where the file system does not keep birth times. GNU stat first, then BSD stat.
const inspectScript = `lman_inspect() {
  r=$(readlink -f -- "$1" 2>/dev/null || realpath -- "$1" 2>/dev/null) || return 0
  [ -f "$r" ] || return 0
  t=$(stat -c %W -- "$r" 2>/dev/null)
  case "$t" in ''|0|-) t=$(stat -c %Z -- "$r" 2>/dev/null || stat -f %B -- "$r" 2>/dev/null) ;; esac
  printf '%s\t%s\t%s\n' "$1" "$r" "$t"
}
`

func (s *SSH) Inspect(ctx context.Context, paths []string) ([]app.FileInfo, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	var b strings.Builder
	b.WriteString(inspectScript)
	for _, p := range paths {
		b.WriteString("lman_inspect " + quote(p) + "\n")
	}
	out, err := s.script(ctx, b.String())
	if err != nil {
		return nil, err
	}
	var infos []app.FileInfo
	for _, line := range lines(out) {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			continue
		}
		info := app.FileInfo{Path: fields[0], RealPath: fields[1]}
		if sec, err := strconv.ParseInt(fields[2], 10, 64); err == nil {
			info.InstalledAt = time.Unix(sec, 0)
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// Run passes only PATH of c.Env: the rest belongs to the local computer.
func (s *SSH) Run(ctx context.Context, c app.Command) ([]byte, error) {
	var b strings.Builder
	if p := c.Env["PATH"]; p != "" {
		b.WriteString("export PATH=" + quote(p) + "\n")
	}
	b.WriteString("exec " + quote(c.Path))
	for _, a := range c.Args {
		b.WriteString(" " + quote(a))
	}
	b.WriteString(" 2>&1\n")
	return s.script(ctx, b.String())
}

func (s *SSH) RemoveAll(ctx context.Context, dir string) error {
	_, err := s.script(ctx, "rm -rf -- "+quote(dir))
	return err
}

func (s *SSH) ReadFile(ctx context.Context, name string) ([]byte, error) {
	out, err := s.script(ctx, fmt.Sprintf("[ -f %[1]s ] || exit %d\ncat -- %[1]s", quote(name), missingExit))
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == missingExit {
		return nil, fmt.Errorf("read %s: %w", name, fs.ErrNotExist)
	}
	return out, err
}

func (s *SSH) WriteFile(ctx context.Context, name string, data []byte, perm fs.FileMode) error {
	script := fmt.Sprintf("printf '%%s' %s > %s && chmod %o %s", quote(string(data)), quote(name), perm.Perm(), quote(name))
	_, err := s.script(ctx, script)
	return err
}

func (s *SSH) MkdirAll(ctx context.Context, dir string) error {
	_, err := s.script(ctx, "mkdir -p -- "+quote(dir))
	return err
}

func (s *SSH) Symlink(ctx context.Context, target, link string) error {
	_, err := s.script(ctx, "ln -sfn -- "+quote(target)+" "+quote(link))
	return err
}

func (s *SSH) Remove(ctx context.Context, name string) error {
	_, err := s.script(ctx, "rm -f -- "+quote(name))
	return err
}

func (s *SSH) options() []string {
	opts := []string{"-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=" + connectTimeout}
	if s.controlDir != "" {
		opts = append(opts,
			"-o", "ControlMaster=auto",
			"-o", "ControlPath="+filepath.Join(s.controlDir, "%C"),
			"-o", "ControlPersist=60",
		)
	}
	return opts
}

// script runs body with the remote POSIX shell. The body travels on stdin,
// so no quoting is needed on the ssh command line.
func (s *SSH) script(ctx context.Context, body string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ssh", append(s.options(), s.target, "sh", "-s")...)
	cmd.Stdin = strings.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == sshFailure {
		return nil, fmt.Errorf("ssh %s: %s", s.target, strings.TrimSpace(stderr.String()))
	}
	if err != nil {
		return stdout.Bytes(), err
	}
	return stdout.Bytes(), nil
}

// globScript lists matches of every pattern. A pattern that matches nothing is an empty
// result: the script still exits 0, because [ -e ] would otherwise fail the whole scan.
func globScript(patterns []string) string {
	var b strings.Builder
	for i, p := range patterns {
		fmt.Fprintf(&b, "for f in %s; do [ -e \"$f\" ] && printf '%d\\t%%s\\n' \"$f\"; done\n", globQuote(p), i)
	}
	b.WriteString("true\n")
	return b.String()
}

// quote makes s one literal word for a POSIX shell.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// globQuote escapes everything except the glob characters * ? [ ], so the shell expands only them.
func globQuote(p string) string {
	var b strings.Builder
	for _, r := range p {
		switch {
		case r == '*' || r == '?' || r == '[' || r == ']',
			r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '/' || r == '.' || r == '_' || r == '-' || r == '@':
			b.WriteRune(r)
		default:
			b.WriteRune('\\')
			b.WriteRune(r)
		}
	}
	return b.String()
}

func lines(out []byte) []string {
	return strings.FieldsFunc(string(out), func(r rune) bool { return r == '\n' || r == '\r' })
}
