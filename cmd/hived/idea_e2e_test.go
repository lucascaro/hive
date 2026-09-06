//go:build e2e

package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// TestE2E_IdeaAddFromSession is the whole point of the CLI: a note
// typed into a running session's own shell reaches the daemon, is
// attributed to that session, and lands in the project the daemon
// resolves for it — no HIVE_PROJECT_ID in the environment, and no
// client-side project bookkeeping.
func TestE2E_IdeaAddFromSession(t *testing.T) {
	d := spawnDaemon(t)
	sessionID := firstSession(t, d)
	bin := hivedBinary(t)

	// Subscribed before typing: the add is announced to every control
	// connection, so a late subscribe would miss it.
	control := dialControl(t, d)

	// Which project the session is in — the value the daemon must
	// resolve on its own from HIVE_SESSION_ID alone.
	if err := control.ListSessions(); err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	snap, err := control.AwaitSessionsSnapshot(3 * time.Second)
	if err != nil {
		t.Fatalf("sessions snapshot: %v", err)
	}
	wantProject := ""
	for _, s := range snap.Sessions {
		if s.ID == sessionID {
			wantProject = s.ProjectID
		}
	}
	if wantProject == "" {
		t.Fatalf("bootstrap session %s has no project", sessionID)
	}

	a := dialAttach(t, d, sessionID)
	const text = "the grid loses focus after two presses"
	cmd := fmt.Sprintf("%s idea add -k bug %q\n", bin, text)
	if err := a.WriteStdin([]byte(cmd)); err != nil {
		t.Fatalf("write stdin: %v", err)
	}

	ev, err := control.AwaitIdeaEvent(wire.IdeaEventAdded, 10*time.Second)
	if err != nil {
		t.Fatalf("await IDEA_EVENT(added): %v", err)
	}
	if ev.Idea.Text != text {
		t.Errorf("text = %q, want %q", ev.Idea.Text, text)
	}
	if ev.Idea.Kind != wire.IdeaKindBug {
		t.Errorf("kind = %q, want bug", ev.Idea.Kind)
	}
	if ev.Idea.SourceSessionID != sessionID {
		t.Errorf("source session = %q, want %q", ev.Idea.SourceSessionID, sessionID)
	}
	if ev.Idea.ProjectID != wantProject {
		t.Errorf("project = %q, want %q (daemon-resolved)", ev.Idea.ProjectID, wantProject)
	}
	if ev.Idea.Status != wire.IdeaStatusOpen {
		t.Errorf("status = %q, want open", ev.Idea.Status)
	}

	// `add` prints the new id, and `list` shows the note back in the
	// same shell — phase 1 is usable before any GUI exists.
	if _, err := a.WaitForData([]byte(ev.Idea.ID), 5*time.Second); err != nil {
		t.Fatalf("add did not print the new id: %v", err)
	}
	if err := a.WriteStdin([]byte(bin + " idea list\n")); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	out, err := a.WaitForData([]byte("loses focus"), 5*time.Second)
	if err != nil {
		t.Fatalf("idea list did not show the idea: %v", err)
	}
	if !strings.Contains(string(out), "bug") {
		t.Errorf("list output has no kind column: %q", out)
	}
}
