//go:build windows

package tui

import "os"

// lockExclusive is a no-op on Windows.
// The OS-level atomic rename used for state writes provides sufficient
// protection; a proper LockFileEx implementation can be added later.
func lockExclusive(_ *os.File) error { return nil }

// unlockFile is a no-op on Windows.
func unlockFile(_ *os.File) error { return nil }
