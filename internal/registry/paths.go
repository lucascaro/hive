package registry

import (
	"os"
	"path/filepath"
	"runtime"
)

// StateDir returns the platform-specific Hive state directory.
//
//	macOS:   ~/Library/Application Support/Hive
//	Linux:   $XDG_STATE_HOME/hive  (default: ~/.local/state/hive)
//	Windows: %LOCALAPPDATA%\Hive
//
// Tests can override by passing an explicit path to OpenAt.
func StateDir() string {
	switch runtime.GOOS {
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "Hive")
		}
	case "linux", "freebsd", "openbsd", "netbsd":
		if x := os.Getenv("XDG_STATE_HOME"); x != "" {
			return filepath.Join(x, "hive")
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".local", "state", "hive")
		}
	case "windows":
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "Hive")
		}
	}
	return filepath.Join(os.TempDir(), "hive")
}

// SessionsDir is the directory that holds per-session subdirectories
// and the index file.
func SessionsDir(stateDir string) string {
	return filepath.Join(stateDir, "sessions")
}

// ProjectsDir is the directory that holds per-project subdirectories
// and the projects index file.
func ProjectsDir(stateDir string) string {
	return filepath.Join(stateDir, "projects")
}
