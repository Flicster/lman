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
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Flicster/lman/internal/app"
	"github.com/Flicster/lman/internal/domain"
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
	if !validTarget(target) {
		return nil, fmt.Errorf("invalid server %q", target)
	}
	s := &SSH{target: target}
	if runtime.GOOS != goosWindows {
		if err := os.MkdirAll(controlDir, 0o700); err != nil {
			return nil, fmt.Errorf("create ssh control directory: %w", err)
		}
		// MkdirAll keeps the mode of an existing directory; the control sockets must stay private.
		// #nosec G302 -- a directory needs the search bit; 0600 would make the sockets unreachable
		if err := os.Chmod(controlDir, 0o700); err != nil {
			return nil, fmt.Errorf("protect ssh control directory: %w", err)
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

// targetPattern allows an alias from ~/.ssh/config or user@host. ssh expands %-tokens and
// some options in the configuration with the host name, so %, spaces, quotes and other
// punctuation are refused. A leading "-" would be read as an option.
var targetPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@:-]{0,254}$`)

func validTarget(target string) bool {
	return targetPattern.MatchString(target)
}

// Close ends the shared connection.
func (s *SSH) Close() error {
	if s.controlDir == "" {
		return nil
	}
	if !validTarget(s.target) {
		return fmt.Errorf("invalid server %q", s.target)
	}
	// A dead server must not hold the process that is shutting the window down.
	ctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
	defer cancel()
	// #nosec G204 -- the executable is the ssh client; the host matches validTarget, so it is not an option
	cmd := exec.Command("ssh", append(s.options(), "-O", "exit", s.target)...)
	err := runCmd(ctx, cmd)
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
	ctx, cancel := context.WithTimeout(ctx, loginShellTimeout)
	defer cancel()
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
		if !ok || err != nil || i < 0 || i >= len(patterns) || strings.ContainsAny(file, "\n\r\t\x00") {
			continue
		}
		result[i] = append(result[i], file)
	}
	return result, nil
}

// inspectScript prints path, resolved path and the birth time, or the inode change time
// where the file system does not keep birth times. GNU stat first, then BSD stat.
const inspectScript = `lman_inspect() {
  case "$1" in *[[:cntrl:]]*|*'
'*) return 0 ;; esac
  r=$(readlink -f -- "$1" 2>/dev/null || realpath -- "$1" 2>/dev/null) || return 0
  case "$r" in *[[:cntrl:]]*|*'
'*) return 0 ;; esac
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
	asked := make(map[string]bool, len(paths))
	for _, p := range paths {
		if strings.ContainsAny(p, "\n\r\t\x00") {
			continue
		}
		asked[p] = true
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
		if !asked[fields[0]] || strings.ContainsAny(fields[1], "\n\r\t\x00") {
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

// lockedExit is the exit status of lockScript when another run holds the lock.
const lockedExit = 5

// lockAttempts is how many times the server is asked for the lock, one second apart.
const lockAttempts = 30

// Lock takes a lock directory on the server. mkdir creates it or fails on every POSIX
// system, so no flock is needed. A lock left by a killed run is named in the error.
func (s *SSH) Lock(ctx context.Context, name string) (func() error, error) {
	_, err := s.script(ctx, fmt.Sprintf(`i=0
until mkdir %[1]s 2>/dev/null; do
  i=$((i+1))
  [ "$i" -le %[2]d ] || exit %[3]d
  sleep 1
done
`, quote(name), lockAttempts, lockedExit))
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == lockedExit {
		return nil, fmt.Errorf("state is locked on %s by another run; if none is running there, remove %s", s.target, name)
	}
	if err != nil {
		return nil, err
	}
	return func() error {
		uctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
		defer cancel()
		_, err := s.script(uctx, "rmdir "+quote(name)+"\n")
		return err
	}, nil
}

// unlockTimeout bounds releasing a lock after the operation, even if its context ended.
const unlockTimeout = 30 * time.Second

// linkedExit is the exit status of removeAllScript when the path runs through a symbolic link.
const linkedExit = 4

// removeAllScript deletes d only when its real path is d itself: no component of the
// path is a symbolic link.
func removeAllScript(d string) string {
	// Missing readlink and realpath is a refusal. Treating the path as already
	// canonical would let rm follow a symlink on a host that has neither tool.
	return fmt.Sprintf(`d=%[1]s
[ -e "$d" ] || [ -L "$d" ] || exit 0
r=$(readlink -f -- "$d" 2>/dev/null || realpath -- "$d" 2>/dev/null) || exit 1
[ "$r" = "$d" ] || exit %[2]d
rm -rf -- "$d"
`, quote(d), linkedExit)
}

func (s *SSH) RemoveAll(ctx context.Context, dir string) error {
	_, err := s.script(ctx, removeAllScript(dir))
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == linkedExit {
		return fmt.Errorf("refusing to remove %s: it is reached through a symbolic link", dir)
	}
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
	if strings.Contains(name, "\x00") || bytes.Contains(data, []byte{0}) {
		return fmt.Errorf("write %s: a NUL byte cannot be passed through the remote shell", name)
	}
	_, err := s.script(ctx, writeFileScript(name, data, perm.Perm()))
	return err
}

// EnsureBlock appends block when marker is absent. The existing file is not
// read back or rewritten, so a large profile and a NUL byte in it stay intact.
func (s *SSH) EnsureBlock(ctx context.Context, name, marker string, block []byte) error {
	if strings.Contains(name, "\x00") || strings.Contains(marker, "\x00") || bytes.Contains(block, []byte{0}) {
		return fmt.Errorf("write %s: a NUL byte cannot be passed through the remote shell", name)
	}
	script := fmt.Sprintf(`dest=%[1]s
marker=%[2]s
if [ -L "$dest" ]; then
  resolved=$(readlink -f -- "$dest" 2>/dev/null || realpath -- "$dest") || exit 1
  dest=$resolved
fi
if [ -f "$dest" ] && grep -F -q -e "$marker" -- "$dest"; then
  exit 0
fi
if [ -s "$dest" ]; then
  printf '\n' >> "$dest"
fi
printf '%%s' %[3]s >> "$dest"
`, quote(name), quote(marker), quote(string(block)))
	_, err := s.script(ctx, script)
	return err
}

// writeFileScript replaces name only after the bytes are in a temporary file.
// An existing file keeps its mode, so a private shell profile is not made world-readable.
// A symlink is followed and left in place.
func writeFileScript(name string, data []byte, perm fs.FileMode) string {
	var b strings.Builder
	fmt.Fprintf(&b, "dest=%s\n", quote(name))
	b.WriteString(`if [ -L "$dest" ]; then
  resolved=$(readlink -f -- "$dest" 2>/dev/null || realpath -- "$dest") || exit 1
  dest=$resolved
fi
dir=$(dirname -- "$dest")
tmp=$(mktemp "$dir/.lman.XXXXXX") || exit 1
trap 'rm -f "$tmp"' EXIT
`)
	fmt.Fprintf(&b, "printf '%%s' %s > \"$tmp\" || exit 1\n", quote(string(data)))
	// chmod on macOS does not accept --, and both paths are absolute.
	fmt.Fprintf(&b, `if [ -e "$dest" ]; then
  cur=$(stat -c %%a -- "$dest" 2>/dev/null || stat -f %%OLp "$dest" 2>/dev/null) && chmod "$cur" "$tmp"
else
  chmod %o "$tmp" || exit 1
fi
mv -f -- "$tmp" "$dest"
`, perm)
	return b.String()
}

func (s *SSH) MkdirAll(ctx context.Context, dir string) error {
	_, err := s.script(ctx, "mkdir -p -- "+quote(dir)+" && chmod 700 "+quote(dir))
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
	// Command-line options win over ~/.ssh/config, so a Host * block cannot
	// forward the agent or run a LocalCommand for a connection Lman opened.
	opts := []string{
		"-T",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=" + connectTimeout,
		"-o", "ForwardAgent=no",
		"-o", "ForwardX11=no",
		"-o", "ClearAllForwardings=yes",
		"-o", "PermitLocalCommand=no",
		"-o", "ServerAliveInterval=5",
		"-o", "ServerAliveCountMax=2",
	}
	if s.controlDir != "" {
		opts = append(opts,
			"-o", "ControlMaster=auto",
			"-o", "ControlPath="+filepath.Join(s.controlDir, "%C"),
			"-o", "ControlPersist=60",
		)
	}
	return opts
}

// remoteScript runs body from the filesystem root. The remote shell otherwise
// starts in the home directory, and a toolchain file there can redirect a
// program the script starts, the same way a project directory can locally.
func remoteScript(body string) string {
	return "cd / || exit 1\n" + body
}

// script runs body with the remote POSIX shell. The body travels on stdin,
// so no quoting is needed on the ssh command line.
func (s *SSH) script(ctx context.Context, body string) ([]byte, error) {
	if !validTarget(s.target) {
		return nil, fmt.Errorf("invalid server %q", s.target)
	}
	// #nosec G204 -- the executable is the ssh client; the host matches validTarget and the script travels on stdin
	cmd := exec.Command("ssh", append(s.options(), s.target, "sh", "-s")...)
	cmd.Stdin = strings.NewReader(remoteScript(body))
	var stdout, stderr boundedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := runCmd(ctx, cmd)
	var exit *exec.ExitError
	// 255 is also a status a remote command may choose. Treat it as the ssh client
	// failing only when the client itself wrote the diagnostic.
	if errors.As(err, &exit) && exit.ExitCode() == sshFailure && sshClientFailed(stderr.buf.Bytes()) {
		return nil, fmt.Errorf("ssh %s: %s", s.target, strings.TrimSpace(stderr.buf.String()))
	}
	data, overflow := stdout.result()
	if overflow != nil {
		return nil, fmt.Errorf("ssh %s: %w", s.target, overflow)
	}
	if err != nil {
		return data, err
	}
	return data, nil
}

// globScript lists matches of every pattern. A pattern that matches nothing is an empty
// result: the script still exits 0, because [ -e ] would otherwise fail the whole scan.
func globScript(patterns []string) string {
	var b strings.Builder
	for i, p := range patterns {
		fmt.Fprintf(&b, "for f in %s; do\n%v[ -e \"$f\" ] && printf '%d\\t%%s\\n' \"$f\"\ndone\n", globQuote(p), skipControl("f"), i)
	}
	b.WriteString("true\n")
	return b.String()
}

// skipControl drops a name that contains a control character. A tab or a newline
// would be read as another record by the client.
func skipControl(name string) string {
	// The quoted newline is part of the pattern: * does not match a newline.
	return "case \"$" + name + "\" in *[[:cntrl:]]*|*" + "'\n'" + "*) continue ;; esac\n"
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

// sshClientFailed reports whether stderr is the ssh client talking, not the remote command.
func sshClientFailed(stderr []byte) bool {
	text := string(stderr)
	return strings.Contains(text, "ssh:") ||
		strings.Contains(text, "Connection ") ||
		strings.Contains(text, "Permission denied") ||
		strings.Contains(text, "Could not resolve")
}

func lines(out []byte) []string {
	return strings.FieldsFunc(string(out), func(r rune) bool { return r == '\n' || r == '\r' })
}
