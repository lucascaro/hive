package daemon

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// TestBootSocketMeansAnswerable pins the readiness contract every
// client relies on: dialOrSpawn, the GUI's restart probe and the e2e
// waitForSocket all treat "the socket exists" as "the daemon is up",
// and none of them can tell a listening daemon from one that bound
// early and is still doing boot work. Binding last is what makes that
// assumption true — with the bind first, a client that dialed during
// boot sat in the kernel backlog until its HELLO timed out.
//
// The test races a watcher against New: the instant the socket file
// appears it dials and handshakes, and the handshake must complete
// well inside wire's own timeout.
func TestBootSocketMeansAnswerable(t *testing.T) {
	skipOnWindows(t)
	tmp := shortTempDir(t)
	sock := filepath.Join(tmp, "s")
	state := filepath.Join(tmp, "state")

	// A previous run's session, persisted with no live PTY: this is
	// the boot work that used to happen behind an already-bound
	// socket.
	seedPersistedSession(t, sock+"1", state)

	type result struct {
		err     error
		elapsed time.Duration
	}
	res := make(chan result, 1)
	go func() {
		for {
			if _, err := os.Stat(sock); err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		start := time.Now()
		conn, err := net.Dial("unix", sock)
		if err != nil {
			res <- result{err: err}
			return
		}
		defer conn.Close()
		_, err = wire.Handshake(conn, wire.Hello{Client: "test/0", Mode: wire.ModeControl})
		res <- result{err: err, elapsed: time.Since(start)}
	}()

	d, err := New(Config{
		SocketPath: sock,
		StateDir:   state,
		BootstrapSession: session.Options{
			Shell: "/bin/bash",
			Cols:  80,
			Rows:  24,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = d.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = d.Close()
	})

	select {
	case r := <-res:
		if r.err != nil {
			t.Fatalf("handshake on a socket that exists: %v", r.err)
		}
		if r.elapsed > 2*time.Second {
			t.Fatalf("handshake took %v after the socket appeared; the socket is not a readiness signal", r.elapsed)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for the socket to appear")
	}
}

// TestBootRevivesPersistedSessions is the other half: moving revive
// off the boot path must not drop it. The session comes back on its
// own, without anyone attaching.
func TestBootRevivesPersistedSessions(t *testing.T) {
	skipOnWindows(t)
	tmp := shortTempDir(t)
	state := filepath.Join(tmp, "state")
	id := seedPersistedSession(t, filepath.Join(tmp, "s1"), state)

	d, err := New(Config{
		SocketPath: filepath.Join(tmp, "s2"),
		StateDir:   state,
		BootstrapSession: session.Options{
			Shell: "/bin/bash",
			Cols:  80,
			Rows:  24,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = d.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = d.Close()
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		alive := false
		for _, info := range d.Registry().List() {
			if info.ID == id {
				alive = info.Alive
			}
		}
		if alive {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %s was never revived", id)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// seedPersistedSession runs a daemon once against stateDir so it
// leaves a bootstrap session behind on disk, then shuts it down. The
// returned id names an entry that the next daemon loads with no live
// PTY.
func seedPersistedSession(t *testing.T, sock, stateDir string) string {
	t.Helper()
	d, err := New(Config{
		SocketPath: sock,
		StateDir:   stateDir,
		BootstrapSession: session.Options{
			Shell: "/bin/bash",
			Cols:  80,
			Rows:  24,
		},
	})
	if err != nil {
		t.Fatalf("seed New: %v", err)
	}
	sessions := d.Registry().List()
	if len(sessions) != 1 {
		t.Fatalf("seed: got %d sessions, want 1", len(sessions))
	}
	id := sessions[0].ID
	if err := d.Close(); err != nil {
		t.Fatalf("seed Close: %v", err)
	}
	return id
}
