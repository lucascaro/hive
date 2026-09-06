//go:build windows

package daemon

import "os"

// acquireStateLock is a no-op on Windows. The socket path there did not
// move (%LOCALAPPDATA%\Hive), so the pre-existing "something answers
// this socket" probe in New is still a working singleton guard, and the
// upgrade window the POSIX lock exists to close does not open.
func acquireStateLock(string) (*os.File, error) { return nil, nil }
