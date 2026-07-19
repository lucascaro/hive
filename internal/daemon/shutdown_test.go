package daemon

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// TestShutdownFrameStopsDaemon covers the GUI's primary restart
// channel: a control client asks the daemon to exit in-band, and the
// socket must stop answering. This is what lets RestartDaemon replace
// the daemon without depending on the pidfile — the handle that goes
// missing in the bug this test exists for.
func TestShutdownFrameStopsDaemon(t *testing.T) {
	skipOnWindows(t)
	tmp := shortTempDir(t)
	sock := filepath.Join(tmp, "s")

	d, err := New(Config{
		SocketPath: sock,
		StateDir:   filepath.Join(tmp, "state"),
		BootstrapSession: session.Options{
			Shell: "/bin/bash",
			Cols:  80,
			Rows:  24,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	// Run with a context that is never cancelled, so a clean return
	// can only have come from the shutdown frame.
	runErr := make(chan error, 1)
	go func() { runErr <- d.Run(context.Background()) }()

	conn := dial(t, d)
	handshake(t, conn, wire.Hello{Mode: wire.ModeControl})

	if err := wire.WriteFrame(conn, wire.FrameShutdown, nil); err != nil {
		t.Fatalf("write shutdown: %v", err)
	}

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run after shutdown: got %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return within 3s of FrameShutdown")
	}

	// The listener is closed, so nothing may answer on the socket —
	// this is precisely the condition RestartDaemon probes for before
	// it relaunches the GUI.
	if c, err := net.DialTimeout("unix", sock, time.Second); err == nil {
		_ = c.Close()
		t.Fatalf("socket %s still accepting after shutdown", sock)
	}
}

// TestShutdownIsIdempotent guards the sync.Once: a client may send
// the frame twice (or two clients may race), and the second close of
// the shutdown channel would panic without it.
func TestShutdownIsIdempotent(t *testing.T) {
	skipOnWindows(t)
	tmp := shortTempDir(t)
	d, err := New(Config{
		SocketPath: filepath.Join(tmp, "s"),
		StateDir:   filepath.Join(tmp, "state"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	d.Shutdown()
	d.Shutdown()
}
