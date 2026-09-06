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

// An explicit socket path that is not the platform default belongs to a
// test or an isolated dev daemon; neither should refuse to start because
// the user's real, pre-move Hive is running.
func TestLegacyDaemonBlockingIgnoresNonDefaultSockets(t *testing.T) {
	skipOnWindows(t)
	if _, blocked := legacyDaemonBlocking(filepath.Join(t.TempDir(), "s")); blocked {
		t.Fatal("a non-default socket path was blocked by the legacy probe")
	}
}
