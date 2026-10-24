// Package host gives access to the computer whose languages Lman manages:
// the local one through the operating system, a server through the ssh client.
package host

import (
	"regexp"
	"strings"
)

// envMarker separates the output of shell startup files from the environment listing.
const envMarker = "__LMAN_ENV__"

// envScript prints the environment of a new interactive login shell, as a terminal gets it.
const envScript = `printf '\n` + envMarker + `\n'; env`

var envLine = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)

// parseEnv reads `env` output after the last marker. Lines that do not start
// a variable continue the value of the previous one.
func parseEnv(out string) map[string]string {
	if i := strings.LastIndex(out, envMarker+"\n"); i >= 0 {
		out = out[i+len(envMarker)+1:]
	}
	env := make(map[string]string)
	last := ""
	for _, line := range strings.Split(out, "\n") {
		if m := envLine.FindStringSubmatch(line); m != nil {
			env[m[1]] = m[2]
			last = m[1]
			continue
		}
		if last != "" && line != "" {
			env[last] += "\n" + line
		}
	}
	return env
}

func flatten(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}
