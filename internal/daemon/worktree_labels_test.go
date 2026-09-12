package daemon

import (
	"net"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// readProjectEvent reads control frames until a PROJECT_EVENT arrives,
// returning it. Mirrors readWorktrees: a silent timeout would hide an
// actual refusal, so any ERROR seen along the way is reported.
func readProjectEvent(t *testing.T, conn interface {
	SetReadDeadline(time.Time) error
	Read([]byte) (int, error)
}) wire.ProjectEvent {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr wire.Error
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			continue
		}
		switch ft {
		case wire.FrameProjectEvent:
			var ev wire.ProjectEvent
			if err := jsonUnmarshal(payload, &ev); err != nil {
				t.Fatalf("decode PROJECT_EVENT: %v", err)
			}
			return ev
		case wire.FrameError:
			_ = jsonUnmarshal(payload, &lastErr)
		}
	}
	if lastErr.Code != "" {
		t.Fatalf("no PROJECT_EVENT; last ERROR was %s: %s", lastErr.Code, lastErr.Message)
	}
	t.Fatal("timed out waiting for PROJECT_EVENT")
	return wire.ProjectEvent{}
}

// A worktree label has to repaint EVERY open sidebar, not just the
// window that set it. Every existing worktree test reads back on the
// same connection it mutated on, so none of them would catch a fan-out
// that reaches only the caller — which is exactly what would happen if
// SetWorktreeLabel had been built on the worktree mutations' reply
// pattern (daemon.sendWorktrees writes to the requesting conn alone).
// So: set on A, assert B hears it.
func TestControl_SetWorktreeLabelReachesOtherConnections(t *testing.T) {
	d, _ := startDaemonInRepo(t)
	pid := projectIDOf(t, d)

	setter := dial(t, d)
	defer setter.Close()
	_ = handshake(t, setter, wire.Hello{Mode: wire.ModeControl})

	observer := dial(t, d)
	defer observer.Close()
	_ = handshake(t, observer, wire.Hello{Mode: wire.ModeControl})

	const path = "/repo/.worktrees/auth"
	if err := wire.WriteJSON(setter, wire.FrameSetWorktreeLabel, wire.SetWorktreeLabelReq{
		ProjectID: pid, Path: path, Label: "auth refactor",
	}); err != nil {
		t.Fatalf("write SET_WORKTREE_LABEL: %v", err)
	}

	ev := readProjectEvent(t, observer)
	if ev.Project.ID != pid {
		t.Fatalf("event for project %q, want %q", ev.Project.ID, pid)
	}
	if got := ev.Project.WorktreeLabels[path]; got != "auth refactor" {
		t.Errorf("observer saw label %q, want %q (labels: %#v)",
			got, "auth refactor", ev.Project.WorktreeLabels)
	}
}

// Clearing must reach the other connections too — otherwise a second
// window keeps painting a name the user just deleted.
func TestControl_ClearWorktreeLabelReachesOtherConnections(t *testing.T) {
	d, _ := startDaemonInRepo(t)
	pid := projectIDOf(t, d)

	setter := dial(t, d)
	defer setter.Close()
	_ = handshake(t, setter, wire.Hello{Mode: wire.ModeControl})

	const path = "/repo/.worktrees/auth"
	if err := wire.WriteJSON(setter, wire.FrameSetWorktreeLabel, wire.SetWorktreeLabelReq{
		ProjectID: pid, Path: path, Label: "auth refactor",
	}); err != nil {
		t.Fatalf("write SET_WORKTREE_LABEL: %v", err)
	}

	// Connect only after the label exists, so the clear is the first
	// event this connection can possibly see.
	observer := dial(t, d)
	defer observer.Close()
	_ = handshake(t, observer, wire.Hello{Mode: wire.ModeControl})
	drainProjectSnapshot(t, observer)

	if err := wire.WriteJSON(setter, wire.FrameSetWorktreeLabel, wire.SetWorktreeLabelReq{
		ProjectID: pid, Path: path, Label: "",
	}); err != nil {
		t.Fatalf("write clear: %v", err)
	}

	ev := readProjectEvent(t, observer)
	if _, present := ev.Project.WorktreeLabels[path]; present {
		t.Errorf("cleared label still present: %#v", ev.Project.WorktreeLabels)
	}
}

// drainProjectSnapshot consumes whatever the daemon pushes immediately
// after a control handshake, so a following read sees the event under
// test rather than the initial snapshot.
func drainProjectSnapshot(t *testing.T, conn net.Conn) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		ft, _, err := wire.ReadFrame(conn)
		if err != nil {
			return // nothing more queued
		}
		if ft == wire.FrameProjectEvent {
			return
		}
	}
}

// An unknown project must surface as an ERROR, not as a silent no-op:
// the GUI would otherwise show a name it never persisted.
func TestControl_SetWorktreeLabelUnknownProjectErrors(t *testing.T) {
	d, _ := startDaemonInRepo(t)

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})

	if err := wire.WriteJSON(conn, wire.FrameSetWorktreeLabel, wire.SetWorktreeLabelReq{
		ProjectID: "no-such-project", Path: "/repo/.worktrees/a", Label: "x",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			continue
		}
		if ft == wire.FrameError {
			var e wire.Error
			_ = jsonUnmarshal(payload, &e)
			if e.Code != "set_worktree_label_failed" {
				t.Errorf("error code = %q, want set_worktree_label_failed", e.Code)
			}
			return
		}
	}
	t.Fatal("no ERROR frame for an unknown project")
}
