//go:build !darwin && !linux && !windows

package host

import (
	"io/fs"
	"time"
)

func createdAt(st fs.FileInfo) time.Time {
	return st.ModTime()
}
