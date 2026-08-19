package daemon

import (
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// blockSpawn holds every PTY spawn until the returned release is
// called, so a create can be kept in flight for the duration of a test.
func blockSpawn(t *testing.T) (release func()) {
	t.Helper()
	gate := make(chan struct{})
	restore := registry.SetStartSessionForTest(func(opts session.Options) (*session.Session, error) {
		<-gate
		return session.Start(opts)
	})
	var once bool
	t.Cleanup(func() {
		if !once {
			close(gate)
		}
		restore()
	})
	return func() {
		if !once {
			once = true
			close(gate)
		}
	}
}

// TestControlLoopStaysResponsiveDuringCreate is the freeze regression
// test. Session creation shells out to git and forks a PTY; it used to
// run inline on the control read loop, so every other client request —
// from every other connection — waited behind it.
func TestControlLoopStaysResponsiveDuringCreate(t *testing.T) {
	skipOnWindows(t)
	// Daemon first: its bootstrap session must spawn normally, before
	// we gate the seam.
	d := startTestDaemon(t)
	release := blockSpawn(t)

	conn := dial(t, d)
	defer conn.Close()
	handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	// Drain the initial PROJECTS + SESSIONS snapshots.
	readControlFrame(t, conn, wire.FrameSessions, 2*time.Second)

	if err := wire.WriteJSON(conn, wire.FrameCreateSession, wire.CreateSpec{
		Name: "blocked", Cols: 80, Rows: 24, Shell: "/bin/bash",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// The entry is announced while the spawn is still blocked...
	ev := awaitSessionEvent(t, conn, func(ev wire.SessionEvent) bool {
		return ev.Kind == wire.SessionEventAdded && ev.Session.Name == "blocked"
	}, 2*time.Second)
	if ev.Session.Alive {
		t.Error("added event claims alive:true before the PTY exists")
	}
	if ev.Session.Phase != wire.PhaseStarting {
		t.Errorf("added phase: got %q, want %q", ev.Session.Phase, wire.PhaseStarting)
	}

	// ...and the loop keeps answering requests meanwhile, on this
	// connection and on a second one.
	if err := wire.WriteJSON(conn, wire.FrameListSessions, wire.ListSessionsReq{}); err != nil {
		t.Fatalf("list: %v", err)
	}
	readControlFrame(t, conn, wire.FrameSessions, 2*time.Second)

	other := dial(t, d)
	defer other.Close()
	handshake(t, other, wire.Hello{Mode: wire.ModeControl})
	readControlFrame(t, other, wire.FrameSessions, 2*time.Second)

	release()
	awaitSessionEvent(t, conn, func(ev wire.SessionEvent) bool {
		return ev.Session.Name == "blocked" &&
			ev.Session.Alive && ev.Session.Phase == wire.PhaseReady
	}, 5*time.Second)
}

// TestAttachWhileStartingReportsStarting: attaching in the window
// between `added` and PhaseReady is a normal race, not a dead session
// — the client is told to wait rather than shown a death overlay.
func TestAttachWhileStartingReportsStarting(t *testing.T) {
	skipOnWindows(t)
	// Daemon first: its bootstrap session must spawn normally, before
	// we gate the seam.
	d := startTestDaemon(t)
	release := blockSpawn(t)

	conn := dial(t, d)
	defer conn.Close()
	handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	readControlFrame(t, conn, wire.FrameSessions, 2*time.Second)

	if err := wire.WriteJSON(conn, wire.FrameCreateSession, wire.CreateSpec{
		Name: "pending", Cols: 80, Rows: 24, Shell: "/bin/bash",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	ev := awaitSessionEvent(t, conn, func(ev wire.SessionEvent) bool {
		return ev.Kind == wire.SessionEventAdded && ev.Session.Name == "pending"
	}, 2*time.Second)

	att := dial(t, d)
	defer att.Close()
	if err := wire.WriteJSON(att, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0",
		Mode: wire.ModeAttach, SessionID: ev.Session.ID,
	}); err != nil {
		t.Fatalf("attach hello: %v", err)
	}
	var werr wire.Error
	_ = att.SetReadDeadline(time.Now().Add(2 * time.Second))
	ft, err := wire.ReadJSON(att, &werr)
	if err != nil {
		t.Fatalf("attach read: %v", err)
	}
	if ft != wire.FrameError || werr.Code != wire.ErrCodeSessionStarting {
		t.Fatalf("attach while starting: got %s %+v, want ERROR %s", ft, werr, wire.ErrCodeSessionStarting)
	}
	if werr.SessionID != ev.Session.ID {
		t.Errorf("error session_id: got %q, want %q", werr.SessionID, ev.Session.ID)
	}
	release()
}

// TestCreateModeStillSynchronous: ModeCreate creates and attaches on
// one connection, so that path must stay synchronous even though the
// control-frame path no longer is.
func TestCreateModeStillSynchronous(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)

	conn := dial(t, d)
	defer conn.Close()
	w := handshake(t, conn, wire.Hello{
		Mode: wire.ModeCreate,
		Create: &wire.CreateSpec{
			Name: "inline", Cols: 80, Rows: 24, Shell: "/bin/bash",
		},
	})
	if w.SessionID == "" {
		t.Fatal("ModeCreate welcome carried no session id")
	}
	if got := d.Registry().Phase(w.SessionID); got != wire.PhaseReady {
		t.Errorf("ModeCreate session phase: got %q, want ready", got)
	}
	if e := d.Registry().Get(w.SessionID); e == nil || e.Session() == nil {
		t.Error("ModeCreate returned before the PTY existed")
	}
}
