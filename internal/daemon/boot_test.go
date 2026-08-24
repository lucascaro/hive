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

// TestCloseLeavesAReplacementSocketAlone pins the fix for a race the
// boot chores opened: Close waits on d.ops, so teardown now spans the
// whole boot instead of the ~100ms the restart spec assumed. That is
// long enough for the GUI's Restart to relaunch and a NEW hived to
// bind a fresh socket at the same path — and an unconditional
// os.Remove in Close would unlink the live daemon's socket, leaving
// it serving an inode nobody can dial.
func TestCloseLeavesAReplacementSocketAlone(t *testing.T) {
	skipOnWindows(t)
	tmp := shortTempDir(t)
	sock := filepath.Join(tmp, "s")

	old, err := New(Config{
		SocketPath:       sock,
		StateDir:         filepath.Join(tmp, "state1"),
		BootstrapSession: session.Options{Shell: "/bin/bash", Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("New old: %v", err)
	}
	// The old daemon stops accepting (what the GUI's Restart action
	// does) and its listener is unlinked, but its teardown — which
	// now waits on the boot chores — has not run yet.
	oldCtx, oldCancel := context.WithCancel(context.Background())
	defer oldCancel()
	runDone := make(chan struct{})
	go func() { defer close(runDone); _ = old.Run(oldCtx) }()
	old.Shutdown()
	<-runDone

	// Replacement takes over the path, exactly as a relaunched GUI's
	// hived would: the old socket is no longer connectable, so New
	// removes it and binds its own.
	replacement, err := New(Config{
		SocketPath:       sock,
		StateDir:         filepath.Join(tmp, "state2"),
		BootstrapSession: session.Options{Shell: "/bin/bash", Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("New replacement: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = replacement.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = replacement.Close()
	})

	// Now the old daemon's teardown lands.
	_ = old.Close()

	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("the replacement daemon's socket was unlinked by the old daemon's Close: %v", err)
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial replacement after old Close: %v", err)
	}
	defer conn.Close()
	if _, err := wire.Handshake(conn, wire.Hello{Client: "test/0", Mode: wire.ModeControl}); err != nil {
		t.Fatalf("handshake with the replacement: %v", err)
	}
}

// TestCloseStopsBootWork pins the stop signal: a daemon closed right
// after New must not sit in ops.Wait forking login shells for a
// process that is going away.
func TestCloseStopsBootWork(t *testing.T) {
	skipOnWindows(t)
	tmp := shortTempDir(t)
	state := filepath.Join(tmp, "state")
	// Several persisted sessions, so an uncancelled revive would be
	// visibly slower than a cancelled one.
	seedPersistedSessions(t, filepath.Join(tmp, "s1"), state, 4)

	d, err := New(Config{
		SocketPath:       filepath.Join(tmp, "s2"),
		StateDir:         state,
		BootstrapSession: session.Options{Shell: "/bin/bash", Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start := time.Now()
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Close took %v; the boot chores are not being told to stop", elapsed)
	}
	// And it really stopped early rather than quietly reviving them
	// all: at most one session (the one in flight) may have spawned.
	alive := 0
	for _, info := range d.Registry().List() {
		if info.Alive {
			alive++
		}
	}
	if alive > 1 {
		t.Fatalf("%d sessions were revived after Close; want at most the one in flight", alive)
	}
}

// seedPersistedSession runs a daemon once against stateDir so it
// leaves a bootstrap session behind on disk, then shuts it down. The
// returned id names an entry that the next daemon loads with no live
// PTY.
func seedPersistedSession(t *testing.T, sock, stateDir string) string {
	t.Helper()
	ids := seedPersistedSessions(t, sock, stateDir, 1)
	return ids[0]
}

// seedPersistedSessions is seedPersistedSession for n sessions.
func seedPersistedSessions(t *testing.T, sock, stateDir string, n int) []string {
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
	for len(d.Registry().List()) < n {
		if _, err := d.Registry().Create(context.Background(), wire.CreateSpec{
			Shell: "/bin/bash", Cols: 80, Rows: 24,
		}); err != nil {
			t.Fatalf("seed Create: %v", err)
		}
	}
	var ids []string
	for _, info := range d.Registry().List() {
		ids = append(ids, info.ID)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("seed Close: %v", err)
	}
	return ids
}

// TestPersistedSessionsNeverLookDeadAtBoot pins the fix for the
// restart flash: the socket is bound before reviveAll has forked any
// PTY, and an entry loaded from disk has no session, so without the
// pre-mark a client's first snapshot showed alive:false with a ready
// phase — the exact combination every client renders as "this session
// died" (the GUI paints its dead overlay, serveAttach answers
// session_dead). Every unrevived entry must read spawning instead.
func TestPersistedSessionsNeverLookDeadAtBoot(t *testing.T) {
	skipOnWindows(t)
	tmp := shortTempDir(t)
	sock := filepath.Join(tmp, "s")
	state := filepath.Join(tmp, "state")
	seedPersistedSessions(t, sock+"1", state, 3)

	// Hold every revive spawn so the boot window stays open for the
	// whole assertion instead of racing the sequential revive.
	release := blockSpawn(t)
	// Function-scoped, so it runs BEFORE the t.Cleanup below: a
	// failing assertion must not leave d.Close() waiting on a revive
	// that is still parked on the gate.
	defer release()

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

	list := d.Registry().List()
	if len(list) != 3 {
		t.Fatalf("List: got %d sessions, want 3", len(list))
	}
	for _, info := range list {
		if !info.Alive && info.Phase == wire.PhaseReady {
			t.Fatalf("session %s reads dead at boot: alive=%v phase=%q",
				info.ID, info.Alive, info.Phase)
		}
	}

	// And the pre-mark must not block the revive itself: once the
	// spawns are let go every session lands alive and ready.
	release()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ok := true
		for _, info := range d.Registry().List() {
			if !info.Alive || info.Phase != wire.PhaseReady {
				ok = false
			}
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("sessions never revived: %+v", d.Registry().List())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
