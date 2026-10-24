package host

import (
	"io/fs"
	"syscall"
	"time"
)

// createdAt is the file birth time: when the file appeared on this disk.
func createdAt(st fs.FileInfo) time.Time {
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return st.ModTime()
	}
	return time.Unix(sys.Birthtimespec.Sec, sys.Birthtimespec.Nsec)
}
