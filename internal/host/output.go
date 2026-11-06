package host

import (
	"bytes"
	"errors"
)

// maxOutput bounds what a program or a remote script may print to Lman. A runaway
// binary or file must not exhaust memory before the timeout ends it.
const maxOutput = 8 << 20

var errOutputTooLarge = errors.New("output exceeds 8 MiB")

// boundedBuffer keeps up to maxOutput bytes. Writes past the limit are dropped and
// recorded, and the caller gets an error instead of a truncated result.
type boundedBuffer struct {
	buf      bytes.Buffer
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	room := maxOutput - b.buf.Len()
	if room < 0 {
		room = 0
	}
	if len(p) > room {
		b.overflow = true
		p = p[:room]
	}
	b.buf.Write(p)
	// Report the full length: a short write would make the copy fail for a reason
	// that has nothing to do with the program.
	return n, nil
}

// result returns what was collected and errOutputTooLarge when the limit was passed.
func (b *boundedBuffer) result() ([]byte, error) {
	if b.overflow {
		return b.buf.Bytes(), errOutputTooLarge
	}
	return b.buf.Bytes(), nil
}
