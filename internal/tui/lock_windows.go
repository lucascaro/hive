//go:build windows

package tui

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockExclusive acquires an exclusive (write) advisory lock on f.
// It blocks until the lock is available.
func lockExclusive(f *os.File) error {
	// LockFileEx with LOCKFILE_EXCLUSIVE_LOCK and no LOCKFILE_FAIL_IMMEDIATELY
	// blocks until the lock is acquired.
	ol := new(windows.Overlapped)
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0,           // reserved
		1, 0,        // lock 1 byte
		ol,
	)
}

// unlockFile releases the advisory lock on f.
func unlockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(
		windows.Handle(f.Fd()),
		0,        // reserved
		1, 0,     // unlock 1 byte
		ol,
	)
}
