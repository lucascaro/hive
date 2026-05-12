package daemon

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// drainControlSnapshots eats the unsolicited PROJECTS + SESSIONS pair
// the daemon emits right after a control handshake.
func drainControlSnapshots(t *testing.T, conn net.Conn) {
	t.Helper()
	for i := range 2 {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := wire.ReadFrame(conn); err != nil {
			t.Fatalf("drain snapshot %d: %v", i, err)
		}
	}
}

// readEventUntil reads frames until it finds one matching want, or the
// deadline expires. Returns the payload of the matching frame, or nil
// on timeout.
func readEventUntil(t *testing.T, conn net.Conn, want wire.FrameType, deadline time.Time) []byte {
	t.Helper()
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			continue
		}
		if ft == want {
			return payload
		}
	}
	return nil
}

// TestUpdateSession_BroadcastsUpdatedEvent verifies that renaming a
// session emits SESSION_EVENT(updated) with the new name.
func TestUpdateSession_BroadcastsUpdatedEvent(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := firstSessionID(t, d)

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	drainControlSnapshots(t, conn)

	newName := "renamed"
	if err := wire.WriteJSON(conn, wire.FrameUpdateSession, wire.UpdateSessionReq{
		SessionID: id,
		Name:      &newName,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	payload := readEventUntil(t, conn, wire.FrameSessionEvent, time.Now().Add(2*time.Second))
	if payload == nil {
		t.Fatalf("no SESSION_EVENT received")
	}
	var ev wire.SessionEvent
	_ = jsonUnmarshal(payload, &ev)
	if ev.Kind != wire.SessionEventUpdated {
		t.Errorf("kind: got %q, want %q", ev.Kind, wire.SessionEventUpdated)
	}
	if ev.Session.Name != newName {
		t.Errorf("name: got %q, want %q", ev.Session.Name, newName)
	}
	if entry := d.Registry().Get(id); entry == nil || entry.Name != newName {
		t.Errorf("registry not updated: %+v", entry)
	}
}

// TestUpdateProject_BroadcastsUpdatedEvent: renaming a project emits
// PROJECT_EVENT(updated).
func TestUpdateProject_BroadcastsUpdatedEvent(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	drainControlSnapshots(t, conn)

	projects := d.Registry().ListProjects()
	if len(projects) == 0 {
		t.Fatalf("no projects in registry")
	}
	pid := projects[0].ID
	newName := "alpha"
	if err := wire.WriteJSON(conn, wire.FrameUpdateProject, wire.UpdateProjectReq{
		ProjectID: pid,
		Name:      &newName,
	}); err != nil {
		t.Fatalf("update project: %v", err)
	}
	payload := readEventUntil(t, conn, wire.FrameProjectEvent, time.Now().Add(2*time.Second))
	if payload == nil {
		t.Fatalf("no PROJECT_EVENT received")
	}
	var ev wire.ProjectEvent
	_ = jsonUnmarshal(payload, &ev)
	if ev.Kind != wire.ProjectEventUpdated || ev.Project.Name != newName {
		t.Errorf("project event: %+v", ev)
	}
}

// TestBroadcast_TwoControlConns: events should fan out to every control
// connection — second client must see a session created via the first.
func TestBroadcast_TwoControlConns(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)

	c1 := dial(t, d)
	defer c1.Close()
	_ = handshake(t, c1, wire.Hello{Mode: wire.ModeControl, Client: "c1"})
	drainControlSnapshots(t, c1)

	c2 := dial(t, d)
	defer c2.Close()
	_ = handshake(t, c2, wire.Hello{Mode: wire.ModeControl, Client: "c2"})
	drainControlSnapshots(t, c2)

	if err := wire.WriteJSON(c1, wire.FrameCreateSession, wire.CreateSpec{
		Name: "fanout", Cols: 80, Rows: 24, Shell: "/bin/bash",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for _, c := range []net.Conn{c1, c2} {
		payload := readEventUntil(t, c, wire.FrameSessionEvent, deadline)
		if payload == nil {
			t.Fatalf("conn missed SESSION_EVENT broadcast")
		}
		var ev wire.SessionEvent
		_ = jsonUnmarshal(payload, &ev)
		if ev.Kind != wire.SessionEventAdded || ev.Session.Name != "fanout" {
			t.Errorf("ev: %+v", ev)
		}
	}
}

// TestRestartSession_KeepsEntry: restart preserves the session entry
// (same id, same name) and broadcasts an updated event.
func TestRestartSession_KeepsEntry(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := firstSessionID(t, d)
	origName := d.Registry().Get(id).Name

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	drainControlSnapshots(t, conn)

	if err := wire.WriteJSON(conn, wire.FrameRestartSession, wire.RestartSessionReq{
		SessionID: id,
	}); err != nil {
		t.Fatalf("restart: %v", err)
	}

	// Give the registry a moment to respawn before checking liveness.
	time.Sleep(200 * time.Millisecond)
	entry := d.Registry().Get(id)
	if entry == nil {
		t.Fatalf("session vanished after restart")
	}
	if entry.Name != origName {
		t.Errorf("name changed across restart: %q → %q", origName, entry.Name)
	}
	if entry.Session() == nil {
		t.Errorf("session has no PTY after restart")
	}
}

// TestResize_PropagatesToShell: attach, resize, and verify the PTY
// honours the new size by asking the shell via `stty size`.
func TestResize_PropagatesToShell(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := firstSessionID(t, d)

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeAttach, SessionID: id})

	var buf bytes.Buffer
	readUntilReplayDone(t, conn, &buf)

	// Resize to a distinctive size.
	if err := wire.WriteJSON(conn, wire.FrameResize, wire.Resize{Cols: 123, Rows: 37}); err != nil {
		t.Fatalf("resize: %v", err)
	}
	// Give the kernel a tick to deliver SIGWINCH and update.
	time.Sleep(150 * time.Millisecond)
	if err := wire.WriteFrame(conn, wire.FrameData, []byte("stty size\n")); err != nil {
		t.Fatalf("write stty: %v", err)
	}
	buf.Reset()
	drainFor(conn, &buf, 800*time.Millisecond)
	out := buf.String()
	if !strings.Contains(out, "37 123") {
		t.Errorf("stty size did not reflect resize, got: %q", out)
	}
}

// TestCreateSession_UnknownProjectFallsBackToDefault: documents the
// (slightly surprising) registry behavior — unknown project_id is
// silently coerced to the default project rather than erroring.
func TestCreateSession_UnknownProjectFallsBackToDefault(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	drainControlSnapshots(t, conn)

	if err := wire.WriteJSON(conn, wire.FrameCreateSession, wire.CreateSpec{
		Name: "stray", Cols: 80, Rows: 24, Shell: "/bin/bash",
		ProjectID: "no-such-project-id",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	payload := readEventUntil(t, conn, wire.FrameSessionEvent, time.Now().Add(2*time.Second))
	if payload == nil {
		t.Fatalf("no SESSION_EVENT after create with bogus project_id")
	}
	var ev wire.SessionEvent
	_ = jsonUnmarshal(payload, &ev)
	// Should have been routed to the default project.
	defaultID := d.Registry().ListProjects()[0].ID
	if ev.Session.ProjectID != defaultID {
		t.Errorf("project_id: got %q, want default %q", ev.Session.ProjectID, defaultID)
	}
}
