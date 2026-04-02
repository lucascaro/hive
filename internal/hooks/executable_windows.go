//go:build windows

package hooks

import (
	"os"
	"strings"
)

// executableExtensions lists the file extensions that Windows treats as runnable.
var executableExtensions = []string{".bat", ".cmd", ".exe", ".ps1"}

// isExecutable reports whether path is a regular file with an executable extension.
// Windows does not use Unix permission bits; executability is determined by extension.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	lower := strings.ToLower(path)
	for _, ext := range executableExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
