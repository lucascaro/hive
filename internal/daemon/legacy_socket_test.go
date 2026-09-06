package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// HIVE_SOCKET pins the path explicitly, so there is nothing to migrate
// from and the probe must stay out of the way — an isolated dev daemon
// (scripts/dev-iso.sh) would otherwise refuse to start whenever the
// user's real Hive was up.
func TestLegacySocketPathIsEmptyWhenHiveSocketIsSet(t *testing.T) {
	skipOnWindows(t)
	t.Setenv("HIVE_SOCKET", filepath.Join(t.TempDir(), "s"))
	if got := LegacySocketPath(); got != "" {
		t.Fatalf("LegacySocketPath() = %q with HIVE_SOCKET set, want empty", got)
	}
}

// A leftover socket file is not a running daemon. Spawning onto the old
// path because a stale file was there would be worse than the collision
// the probe exists to catch.
func TestLegacyDaemonAliveIgnoresAStaleSocketFile(t *testing.T) {
	skipOnWindows(t)
	// shortTempDir, not t.TempDir: AF_UNIX paths are capped at 104
	// bytes on macOS and the per-test temp path blows straight past it.
	dir := shortTempDir(t)
	stale := filepath.Join(dir, "hived.sock")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// legacyAlive is what LegacyDaemonAlive does once it has a path;
	// the path itself is uid-derived and not overridable.
	if alive := legacyAlive(stale); alive {
		t.Fatal("a stale socket file was read as a live daemon")
	}

	ln, err := net.Listen("unix", filepath.Join(dir, "live.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if alive := legacyAlive(filepath.Join(dir, "live.sock")); !alive {
		t.Fatal("a listening socket was not read as a live daemon")
	}
}

// The legacy path lives in the world-writable /tmp, and on a machine
// that never ran a pre-move Hive the directory is absent — so another
// account can create it and listen. Believing that would wedge every
// consumer of the answer: the daemon refuses to start beside a
// "running" daemon the user cannot see, hivebar pins to the squatted
// path on every reconnect, and the GUI's spawn hits the same refusal.
// An unverifiable directory must read as "no legacy daemon".
func TestLegacyDaemonIsIgnoredWhenItsDirIsNotOurs(t *testing.T) {
	skipOnWindows(t)
	dir := shortTempDir(t)
	sock := filepath.Join(dir, "hived.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Same live socket, only the directory changes.
	if !legacyAlive(sock) {
		t.Fatal("a live socket in a 0700 dir was not read as a legacy daemon")
	}
	for _, mode := range []os.FileMode{0o755, 0o777, 0o707} {
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		if legacyAlive(sock) {
			t.Errorf("dir mode %o: a socket in a group/world-reachable dir was read as a legacy daemon", mode)
		}
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

// An explicit socket path that is not the platform default belongs to a
// test or an isolated dev daemon; neither should refuse to start because
// the user's real, pre-move Hive is running.
func TestLegacyDaemonBlockingIgnoresNonDefaultSockets(t *testing.T) {
	skipOnWindows(t)
	if _, blocked := legacyDaemonBlocking(filepath.Join(t.TempDir(), "s")); blocked {
		t.Fatal("a non-default socket path was blocked by the legacy probe")
	}
}
