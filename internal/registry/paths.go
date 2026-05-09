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
// Setting HIVE_STATE_DIR overrides the platform default — useful for
// running an isolated dev daemon alongside a production one without
// touching its registry. Tests can also override by passing an explicit
// path to OpenAt.
func StateDir() string {
	if s := os.Getenv("HIVE_STATE_DIR"); s != "" {
		return s
	}
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

// StateDirOverridden reports whether HIVE_STATE_DIR is set, i.e. the
// caller is running with an isolated (non-canonical) state directory.
// Code that touches state shared across daemon instances — like the
// on-disk worktree namespace under <project>/.worktrees/ — should
// refuse to garbage-collect when this is true.
func StateDirOverridden() bool {
	return os.Getenv("HIVE_STATE_DIR") != ""
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
