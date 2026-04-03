//go:build !windows

package tui

import (
	"os"
	"syscall"
)

// lockExclusive acquires an exclusive (write) advisory lock on f.
// It blocks until the lock is available.
func lockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// unlockFile releases the advisory lock on f.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
