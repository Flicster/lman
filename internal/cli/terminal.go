package cli

import (
	"io"
	"strings"
	"unicode/utf8"
)

// terminalWriter strips bytes a terminal would act on. A value that arrived from
// the host, such as a path or a manual, cannot move the cursor, restyle the
// confirmation or write to the clipboard.
type terminalWriter struct {
	w io.Writer
}

func (t terminalWriter) Write(p []byte) (int, error) {
	if _, err := t.w.Write(sanitizeTerminal(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}

func sanitizeTerminal(p []byte) []byte {
	var b strings.Builder
	for len(p) > 0 {
		r, size := utf8.DecodeRune(p)
		// Controls in this range are a single ASCII byte. The source byte is
		// used so the caret form does not convert a rune.
		c := p[0]
		p = p[size:]
		if r == utf8.RuneError && size == 1 {
			writeControl(&b, c)
			continue
		}
		if r == '\n' || r == '\t' {
			b.WriteRune(r)
			continue
		}
		if r < 0x20 || r == 0x7f {
			writeControl(&b, c)
			continue
		}
		if isTerminalControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return []byte(b.String())
}

func writeControl(b *strings.Builder, c byte) {
	if c == 0x7f {
		b.WriteString("^?")
		return
	}
	if c < 0x20 {
		b.WriteByte('^')
		b.WriteByte(c + '@')
	}
}

// isTerminalControl reports C1 controls and bidi overrides. A path printed in a
// confirmation can otherwise redraw the question or reverse the text.
func isTerminalControl(r rune) bool {
	switch {
	case r >= 0x80 && r <= 0x9f:
		return true
	case r == 0x061c || r == 0x200e || r == 0x200f || r == 0x2028 || r == 0x2029:
		return true
	case r >= 0x202a && r <= 0x202e:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	default:
		return false
	}
}
