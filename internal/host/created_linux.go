package host

import (
	"io/fs"
	"syscall"
	"time"
)

// createdAt is the inode change time. Archives keep modification times of the release,
// while the change time is set when the file is written on this disk.
func createdAt(st fs.FileInfo) time.Time {
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return st.ModTime()
	}
	return time.Unix(sys.Ctim.Sec, sys.Ctim.Nsec)
}
