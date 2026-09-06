//go:build windows

package daemon

import "os"

// dirOwnerUID is unreachable on Windows: CheckSocketDir returns before
// it. It exists so the POSIX-only syscall.Stat_t stays out of the
// Windows build.
func dirOwnerUID(os.FileInfo) (int, error) { return 0, errNoOwner }
