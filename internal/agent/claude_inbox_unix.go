//go:build !windows

package agent

import (
	"os"
	"syscall"
)

func fileOwnedByUs(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
