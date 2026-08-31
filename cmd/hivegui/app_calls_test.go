package main

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// The Wails-bound RPCs in app_calls.go are the GUI's entire vocabulary
// for talking to the daemon, and none of them had a test: they are
// bindings, so nothing in the Go suite calls them and the Playwright
// suites reach them only through a real daemon. A net.Pipe is enough —
// each method's contract is "write THIS frame with THIS payload", and
// that is exactly what a pipe can observe.

// appWithControl returns an App wired to an in-memory control
// connection, plus a read function that returns the next frame the App
// wrote.
func appWithControl(t *testing.T) (*App, func(t *testing.T) (wire.FrameType, []byte)) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	a := &App{control: wire.NewClient(client)}

	next := func(t *testing.T) (wire.FrameType, []byte) {
		t.Helper()
		type frame struct {
			ft      wire.FrameType
			payload []byte
			err     error
		}
		ch := make(chan frame, 1)
		go func() {
			ft, payload, err := wire.ReadFrame(server)
			ch <- frame{ft, payload, err}
		}()
		select {
		case f := <-ch:
			if f.err != nil {
				t.Fatalf("read frame: %v", f.err)
			}
			return f.ft, f.payload
		case <-time.After(3 * time.Second):
			t.Fatal("no frame written to the control connection")
			return 0, nil
		}
	}
	return a, next
}

// TestCreateSessionResumeSuppressesWorktree pins the one real decision
// CreateSession makes on the way to the wire: resuming work in an
// existing worktree must not ALSO ask for a fresh one, or the daemon
// stacks a nested worktree inside it.
func TestCreateSessionResumeSuppressesWorktree(t *testing.T) {
	a, next := appWithControl(t)

	done := make(chan error, 1)
	go func() {
		done <- a.CreateSession("claude", "proj-1", "n", "#fff", 80, 24,
			true /* useWorktree */, "", "feature/x", "/tmp/existing-wt", true)
	}()

	ft, payload := next(t)
	if ft != wire.FrameCreateSession {
		t.Fatalf("frame = %#x, want FrameCreateSession", byte(ft))
	}
	var spec wire.CreateSpec
	if err := json.Unmarshal(payload, &spec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if spec.UseWorktree {
		t.Error("resuming an existing worktree still asked the daemon to create one")
	}
	if spec.WorktreePath != "/tmp/existing-wt" || spec.Branch != "feature/x" {
		t.Errorf("worktree fields lost in translation: %+v", spec)
	}
	if !spec.ContinueConversation || spec.Agent != "claude" || spec.ProjectID != "proj-1" {
		t.Errorf("payload does not match the arguments: %+v", spec)
	}
	if err := <-done; err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
}

// TestCreateSessionWithoutWorktreePathKeepsTheRequest is the control
// for the test above: the suppression must be conditional, not a
// blanket false.
func TestCreateSessionWithoutWorktreePathKeepsTheRequest(t *testing.T) {
	a, next := appWithControl(t)
	go func() {
		_ = a.CreateSession("claude", "proj-1", "n", "#fff", 80, 24, true, "", "", "", false)
	}()
	_, payload := next(t)
	var spec wire.CreateSpec
	if err := json.Unmarshal(payload, &spec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !spec.UseWorktree {
		t.Error("a plain new-worktree session lost its UseWorktree flag")
	}
}

// TestRPCsRequireAControlConnection: every one of these methods is
// reachable from the frontend before (or after) the daemon is
// connected. They must return an error, not panic on a nil client —
// the frontend surfaces the message, and a panic takes the GUI with it.
func TestRPCsRequireAControlConnection(t *testing.T) {
	a := &App{} // no control connection
	calls := map[string]func() error{
		"CreateSession": func() error {
			return a.CreateSession("claude", "p", "n", "#fff", 80, 24, false, "", "", "", false)
		},
		"DuplicateSession":       func() error { return a.DuplicateSession("claude", "p", "/tmp", "") },
		"KillSession":            func() error { return a.KillSession("s", false) },
		"KillSessionAndWorktree": func() error { return a.KillSessionAndWorktree("s") },
		"RestartSession":         func() error { return a.RestartSession("s") },
		"UpdateSession":          func() error { return a.UpdateSession("s", "n", "#fff", 0) },
		"CreateProject":          func() error { return a.CreateProject("n", "#fff", "/tmp") },
		"KillProject":            func() error { return a.KillProject("p", false) },
		"UpdateProject":          func() error { return a.UpdateProject("p", "n", "#fff", "/tmp", 0) },
		"ListWorktrees":          func() error { return a.ListWorktrees("p") },
		"CreateWorktree":         func() error { return a.CreateWorktree("p", "b") },
		"RemoveWorktree":         func() error { return a.RemoveWorktree("p", "/tmp/wt", false, false, false) },
		"RenameWorktree":         func() error { return a.RenameWorktree("p", "/tmp/wt", "b2") },
		"DeleteBranch":           func() error { return a.DeleteBranch("p", "b", false, false) },
		"RestoreSession":         func() error { return a.RestoreSession("s") },
		"ListClosedSessions":     func() error { return a.ListClosedSessions() },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Errorf("%s with no control connection returned nil, want an error", name)
			}
		})
	}
}

// TestRestoreSessionCall pins the payload the reopen affordances send.
// The empty-id form is the one ⌘Z uses, and it must stay empty on the
// wire: the daemon resolves "the last one" so the client cannot race a
// retention prune between listing and restoring.
func TestRestoreSessionCall(t *testing.T) {
	for _, tc := range []struct{ name, id string }{
		{"explicit id", "sess-1"},
		{"reopen last", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, next := appWithControl(t)
			done := make(chan error, 1)
			go func() { done <- a.RestoreSession(tc.id) }()

			ft, payload := next(t)
			if ft != wire.FrameRestoreSession {
				t.Fatalf("frame = %#x, want FrameRestoreSession", byte(ft))
			}
			var req wire.RestoreSessionReq
			if err := json.Unmarshal(payload, &req); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if req.SessionID != tc.id {
				t.Errorf("SessionID = %q, want %q", req.SessionID, tc.id)
			}
			if err := <-done; err != nil {
				t.Fatalf("RestoreSession: %v", err)
			}
		})
	}
}

// TestListClosedSessionsCall: the reopen list is a plain query, so the
// only thing worth pinning is that it reaches the wire as LIST_CLOSED.
func TestListClosedSessionsCall(t *testing.T) {
	a, next := appWithControl(t)
	done := make(chan error, 1)
	go func() { done <- a.ListClosedSessions() }()

	ft, _ := next(t)
	if ft != wire.FrameListClosed {
		t.Fatalf("frame = %#x, want FrameListClosed", byte(ft))
	}
	if err := <-done; err != nil {
		t.Fatalf("ListClosedSessions: %v", err)
	}
}
