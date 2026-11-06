package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeTerminalDropsBidiAndC1(t *testing.T) {
	in := "ok\x1b[31m\u202ehidden\u009b31m\x9b"
	got := string(sanitizeTerminal([]byte(in)))

	assert.Contains(t, got, "ok")
	assert.Contains(t, got, "^[")
	assert.Contains(t, got, "hidden")
	assert.NotContains(t, got, "\u202e")
	assert.NotContains(t, got, "\u009b")
	assert.NotContains(t, got, "\x9b")
}
