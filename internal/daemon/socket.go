package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/lucascaro/hive/internal/registry"
)

// SocketPath returns the canonical hived socket path for the current
// user and platform. Unix domain sockets are used on all platforms
// rather than a named-pipe split for Windows: AF_UNIX has been
// supported since Windows 10 1803, which is our floor, so one
// transport covers every target.
//
// Setting HIVE_SOCKET overrides the platform default — useful for
// running an isolated dev daemon alongside a production one without
// touching its sessions.
//
// The directory must be one only this user can reach. A shared /tmp is
// not: another account can pre-create /tmp/hive-<uid> and park a fake
// socket in it, and every client here would connect to it and hand it
// every keystroke (2026-09 audit, finding 1). So:
//   - Linux/BSD: $XDG_RUNTIME_DIR/hive (per-user, 0700 by the login
//     manager); without it, fall back to the state dir.
//   - macOS: $TMPDIR/hive — launchd gives every user a private 0700
//     temp dir under /var/folders and os.TempDir returns it. When
//     $TMPDIR is unset os.TempDir returns /tmp, which is refused here
//     in favour of the state dir.
//   - Windows: %LOCALAPPDATA%\Hive (unchanged; per-user profile).
//
// AF_UNIX paths are capped at 104 bytes on macOS; the launchd $TMPDIR
// is ~49, which leaves room for "/hive/hived.sock" and the ".events"
// suffix EventSocketPath appends.
func SocketPath() string {
	if s := os.Getenv("HIVE_SOCKET"); s != "" {
		return s
	}
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
		if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
			return filepath.Join(dir, "hive", "hived.sock")
		}
		return filepath.Join(registry.StateDir(), "hived.sock")
	case "darwin":
		if tmp := os.TempDir(); tmp != "/tmp" && tmp != "/private/tmp" {
			return filepath.Join(tmp, "hive", "hived.sock")
		}
		return filepath.Join(registry.StateDir(), "hived.sock")
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

// EventSocketPath is the narrowed listener that sits next to the
// control socket. It is what spawned sessions get as HIVE_SOCKET: hooks
// and extensions report state on it with ModeEvent, and `hive idea`
// opens a ModeSession connection for ADD_IDEA / LIST_IDEAS plus a
// SESSIONS snapshot narrowed to its own session. Every other mode is
// refused with mode_not_allowed, so a subprocess of an agent cannot
// create, attach to or kill sessions through the environment it
// inherited.
func EventSocketPath(controlSock string) string { return controlSock + ".events" }

// EnsureSocketDir creates the socket's directory 0700 when missing and
// then verifies it with CheckSocketDir. The daemon calls it before
// binding; the GUI calls it before dialing, since it may be the one to
// spawn the daemon.
func EnsureSocketDir(sockPath string) error {
	dir := filepath.Dir(sockPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return CheckSocketDir(sockPath)
}

// CheckSocketDir refuses a socket directory another user could have
// created or can write to: it must be a real directory (not a
// symlink), owned by this uid, with no group or other permission bits.
// Clients call it before dialing so an impostor socket is never
// trusted; the daemon calls it before binding.
//
// No-op on Windows, where the path is under the per-user profile and
// POSIX mode bits do not describe who can reach it.
func CheckSocketDir(sockPath string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir := filepath.Dir(sockPath)
	st, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("socket dir %s is a symlink; refusing", dir)
	}
	if !st.IsDir() {
		return fmt.Errorf("socket dir %s is not a directory", dir)
	}
	if perm := st.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("socket dir %s has mode %04o; must be 0700 (chmod 700 %s)", dir, perm, dir)
	}
	uid, err := dirOwnerUID(st)
	if err != nil {
		return err
	}
	if uid != os.Getuid() {
		return fmt.Errorf("socket dir %s is owned by uid %d, not %d; refusing", dir, uid, os.Getuid())
	}
	return nil
}

// errNoOwner is what dirOwnerUID returns when the platform's FileInfo
// carries no uid — it should not happen on the POSIX targets, and the
// safe reading of "cannot tell who owns this" is to refuse.
var errNoOwner = errors.New("socket dir: cannot read owner")
