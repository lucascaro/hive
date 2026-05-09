package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// SocketPath returns the canonical hived socket path for the current
// user and platform. Phase 1 uses Unix domain sockets on all platforms
// (Windows 10 1803+ supports AF_UNIX).
//
// Setting HIVE_SOCKET overrides the platform default — useful for
// running an isolated dev daemon alongside a production one without
// touching its sessions.
func SocketPath() string {
	if s := os.Getenv("HIVE_SOCKET"); s != "" {
		return s
	}
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
		if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
			return filepath.Join(dir, "hive", "hived.sock")
		}
		return fmt.Sprintf("/tmp/hive-%d/hived.sock", os.Getuid())
	case "darwin":
		// macOS doesn't ship XDG_RUNTIME_DIR. /tmp is the path of least
		// resistance and matches what most Mac daemons do.
		return fmt.Sprintf("/tmp/hive-%d/hived.sock", os.Getuid())
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = os.TempDir()
		}
		return filepath.Join(base, "Hive", "hived.sock")
	default:
		return filepath.Join(os.TempDir(), "hived.sock")
	}
}

// EnsureSocketDir makes sure the directory containing the socket
// exists, with restrictive permissions on POSIX.
func EnsureSocketDir(sockPath string) error {
	dir := filepath.Dir(sockPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return nil
}
