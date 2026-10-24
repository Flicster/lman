package host

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	if runtime.GOOS != "windows" {
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

func TestParseEnvSkipsStartupOutput(t *testing.T) {
	out := "Welcome!\nPATH=/fake\n\n" + envMarker + "\nPATH=/usr/bin:/bin\nHOME=/home/u\nMULTI=a\nb\n"

	env := parseEnv(out)

	assert.Equal(t, map[string]string{"PATH": "/usr/bin:/bin", "HOME": "/home/u", "MULTI": "a\nb"}, env)
}
