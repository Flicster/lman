package host

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileRejectsNUL(t *testing.T) {
	err := (&SSH{target: "box"}).WriteFile(context.Background(), "/tmp/state.json", []byte("a\x00b"), 0o600)
	require.ErrorContains(t, err, "NUL")
}

func TestRemoteScriptLeavesTheWorkingDirectory(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("POSIX shell")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o644))
	t.Chdir(dir)

	cmd := exec.Command("sh", "-s")
	cmd.Stdin = strings.NewReader(remoteScript("test ! -f marker && pwd\n"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Equal(t, "/\n", string(out))
}

func TestQuoteSurvivesTheShell(t *testing.T) {
	values := []string{"plain", "with space", "it's", `$HOME "x" \n`, "multi\nline"}
	for _, v := range values {
		out, err := exec.Command("sh", "-c", "printf '%s' "+quote(v)).Output()
		require.NoError(t, err)
		assert.Equal(t, v, string(out))
	}
}

func TestGlobQuoteExpandsOnlyGlobs(t *testing.T) {
	dir := posixPath(t, t.TempDir()) + "/Application Support"
	require.NoError(t, exec.Command("mkdir", "-p", dir+"/v1/bin", dir+"/v2/bin").Run())
	require.NoError(t, exec.Command("touch", dir+"/v1/bin/node", dir+"/v2/bin/node").Run())

	script := "for f in " + globQuote(dir+"/*/bin/node") + `; do [ -e "$f" ] && printf '%s\n' "$f"; done`
	out, err := exec.Command("sh", "-c", script).CombinedOutput()
	require.NoError(t, err, string(out))

	assert.Equal(t, dir+"/v1/bin/node\n"+dir+"/v2/bin/node\n", string(out))
	assert.Equal(t, `/a\ b/\$x/*`, globQuote("/a b/$x/*"))
}

// posixPath is a path the POSIX shell can glob. A Windows temp path contains
// backslashes; once the pattern has a *, the shell treats them as escapes and
// the glob matches nothing.
func posixPath(t *testing.T, path string) string {
	t.Helper()
	if runtime.GOOS != goosWindows {
		return path
	}
	out, err := exec.Command("sh", "-c", `cygpath -u "$1"`, "sh", path).Output()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func TestGlobScriptSucceedsWhenTheLastPatternMisses(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go"), []byte("x"), 0o755))

	cmd := exec.Command("sh", "-s")
	cmd.Stdin = strings.NewReader(globScript([]string{filepath.Join(dir, "go"), filepath.Join(dir, "missing", "*")}))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Equal(t, "0\t"+filepath.Join(dir, "go")+"\n", string(out))
}

func TestWriteFileScriptPreservesMode(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "rc")
	require.NoError(t, os.WriteFile(dest, []byte("secret"), 0o600))

	cmd := exec.Command("sh", "-s")
	cmd.Stdin = strings.NewReader(writeFileScript(dest, []byte("it's new"), 0o644))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "it's new", string(got))
	assertUnixMode(t, dest, 0o600)

	fresh := filepath.Join(dir, "state.json")
	cmd = exec.Command("sh", "-s")
	cmd.Stdin = strings.NewReader(writeFileScript(fresh, []byte("{}\n"), 0o600))
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	assertUnixMode(t, fresh, 0o600)
}

// assertUnixMode checks permission bits. Windows reports every writable file as 0666.
func assertUnixMode(t *testing.T, name string, mode fs.FileMode) {
	t.Helper()
	if runtime.GOOS == goosWindows {
		return
	}
	st, err := os.Stat(name)
	require.NoError(t, err)
	assert.Equal(t, mode, st.Mode().Perm())
}

func TestRemoveAllScriptFailsClosedWithoutCanonicalizer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the remote shell is POSIX")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "go1.2.3")
	require.NoError(t, os.Mkdir(target, 0o755))
	empty := t.TempDir()

	cmd := exec.Command("sh", "-s")
	cmd.Stdin = strings.NewReader(removeAllScript(target))
	cmd.Env = []string{"PATH=" + empty}
	err := cmd.Run()
	require.Error(t, err)
	_, statErr := os.Stat(target)
	require.NoError(t, statErr, "the directory stays when its real path cannot be checked")
}

func TestSSHOptionsDisableForwarding(t *testing.T) {
	opts := strings.Join((&SSH{target: "box"}).options(), " ")
	assert.Contains(t, opts, "ForwardAgent=no")
	assert.Contains(t, opts, "ForwardX11=no")
	assert.Contains(t, opts, "ClearAllForwardings=yes")
	assert.Contains(t, opts, "PermitLocalCommand=no")
}

func TestValidTarget(t *testing.T) {
	valid := []string{"box", "dev@box", "dev@10.0.0.5", "dev@fe80::1", "my-server.example.com", "a"}
	for _, target := range valid {
		assert.True(t, validTarget(target), target)
	}

	invalid := []string{
		"", "-oProxyCommand=id", "--help", "box;id", "box id", "box%h", "$(id)", "`id`",
		"box'", `box"`, "box\n", "../box", "dev@box/x", ".hidden",
	}
	for _, target := range invalid {
		assert.False(t, validTarget(target), target)
	}
}

func TestParseEnvSkipsStartupOutput(t *testing.T) {
	out := "Welcome!\nPATH=/fake\n\n" + envMarker + "\nPATH=/usr/bin:/bin\nHOME=/home/u\nMULTI=a\nb\n"

	env := parseEnv(out)

	assert.Equal(t, map[string]string{"PATH": "/usr/bin:/bin", "HOME": "/home/u", "MULTI": "a\nb"}, env)
}
