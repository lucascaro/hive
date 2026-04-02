//go:build !windows

package hooks

import "os"

// isExecutable reports whether path is a regular, executable file.
// On Unix a file is executable if any execute bit is set.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Mode()&0o111 != 0
}
