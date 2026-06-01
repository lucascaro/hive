package client

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/session"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("daemon PTY test harness is POSIX-only; client logic is covered by unit tests")
	}
}

// startDaemon boots a real hived on a temp socket with one long-lived
// bootstrap session. Non-zero Cols/Rows trigger bootstrapWanted, so the
// daemon starts a default-shell session kept alive by its PTY.
func startDaemon(t *testing.T) (sock string) {
	t.Helper()
	skipOnWindows(t)
	dir := t.TempDir()
	sock = filepath.Join(dir, "hived.sock")
	d, err := daemon.New(daemon.Config{
		SocketPath:       sock,
		StateDir:         filepath.Join(dir, "state"),
		BootstrapSession: session.Options{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = d.Run(ctx) }()
	t.Cleanup(func() { cancel(); _ = d.Close() })

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", sock); err == nil {
			_ = c.Close()
			return sock
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("daemon socket never appeared: %v", err)
	}
	return sock
}

func TestList_ReturnsBootstrapSession(t *testing.T) {
	sock := startDaemon(t)
	conn, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	sessions, err := List(conn)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) == 0 {
		t.Fatalf("expected at least one session, got none")
	}
}
