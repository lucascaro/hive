//go:build windows

package agent

import "os"

// Claude's inbox on Windows is unverified, so nothing is ever "ours" and
// delivery falls back to typing.
func fileOwnedByUs(os.FileInfo) bool { return false }
