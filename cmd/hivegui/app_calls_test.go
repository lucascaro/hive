package main

import (
	"encoding/json"
	"net"
	"strings"
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
		"KillProject":            func() error { return a.KillProject("p", false, false) },
		"ListIdeas":              func() error { return a.ListIdeas("p") },
		"AddIdea":                func() error { return a.AddIdea("s", "p", "idea", "t") },
		"UpdateIdea":             func() error { return a.UpdateIdea("i", "t", "done", "") },
		"RemoveIdea":             func() error { return a.RemoveIdea("i") },
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

// StateDirID namespaces the frontend's persisted project-id sets, so a
// GUI on one state dir stops pruning away another's ids (#340). Two
// properties matter: stable for a given dir (or the sets are lost on
// every boot) and distinct across dirs (or the bug is unchanged).
func TestStateDirID(t *testing.T) {
	var a App

	t.Setenv("HIVE_STATE_DIR", "/tmp/hive-state-one")
	first := a.StateDirID()
	if len(first) != 8 {
		t.Fatalf("StateDirID() = %q, want 8 hex chars", first)
	}
	for _, r := range first {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("StateDirID() = %q, want lowercase hex", first)
		}
	}
	if again := a.StateDirID(); again != first {
		t.Errorf("StateDirID() not stable: %q then %q", first, again)
	}

	t.Setenv("HIVE_STATE_DIR", "/tmp/hive-state-two")
	if other := a.StateDirID(); other == first {
		t.Errorf("StateDirID() = %q for both state dirs; must differ", other)
	}
}

// TestUpdateIdeaOmitsUnsetFields pins the empty-string-means-no-change
// mapping in UpdateIdea. It is the one idea binding that decides
// anything on the way to the wire: wire.UpdateIdeaReq's fields are
// pointers, and a mapping that filled Text with "" would blank the
// note on every "mark done" — with every existing test still green,
// because none of them reads the payload.
func TestUpdateIdeaOmitsUnsetFields(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		text, status, sessionID     string
		wantText, wantStatus, wantS *string
	}{
		{name: "mark done sends status only", status: "done", wantStatus: strptr("done")},
		{name: "edit sends text only", text: "sharper", wantText: strptr("sharper")},
		{
			name: "start carries the session with the status",
			// The phase-3 shape: status and session_id together.
			status: "started", sessionID: "s7",
			wantStatus: strptr("started"), wantS: strptr("s7"),
		},
		{name: "nothing set sends nothing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, next := appWithControl(t)
			done := make(chan error, 1)
			go func() { done <- a.UpdateIdea("i1", tc.text, tc.status, tc.sessionID) }()

			ft, payload := next(t)
			if ft != wire.FrameUpdateIdea {
				t.Fatalf("frame = %#x, want FrameUpdateIdea", byte(ft))
			}
			var req wire.UpdateIdeaReq
			if err := json.Unmarshal(payload, &req); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if req.ID != "i1" {
				t.Errorf("ID = %q, want %q", req.ID, "i1")
			}
			checkStrPtr(t, "Text", req.Text, tc.wantText)
			checkStrPtr(t, "Status", req.Status, tc.wantStatus)
			checkStrPtr(t, "SessionID", req.SessionID, tc.wantS)
			if err := <-done; err != nil {
				t.Fatalf("UpdateIdea: %v", err)
			}
		})
	}
}

// TestAddIdeaCall: the daemon resolves an empty ProjectID from the
// filing session, so both fields have to reach it verbatim — a binding
// that defaulted either one would silently file against the wrong
// project.
func TestAddIdeaCall(t *testing.T) {
	a, next := appWithControl(t)
	done := make(chan error, 1)
	go func() { done <- a.AddIdea("s1", "", "bug", "it crashes") }()

	ft, payload := next(t)
	if ft != wire.FrameAddIdea {
		t.Fatalf("frame = %#x, want FrameAddIdea", byte(ft))
	}
	var req wire.AddIdeaReq
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.SessionID != "s1" || req.ProjectID != "" ||
		req.Kind != "bug" || req.Text != "it crashes" {
		t.Errorf("req = %+v, want {SessionID:s1 ProjectID: Kind:bug Text:it crashes}", req)
	}
	if err := <-done; err != nil {
		t.Fatalf("AddIdea: %v", err)
	}
}

// TestKillProjectDeleteIdeas: deleteIdeas is the after-confirmation
// override for the daemon's project_has_ideas refusal. Sending it when
// the user has not confirmed destroys captured work, so the flag must
// travel exactly as passed — never defaulted, never inferred from
// killSessions.
func TestKillProjectDeleteIdeas(t *testing.T) {
	for _, tc := range []struct{ kill, ideas bool }{
		{false, false}, {true, false}, {false, true}, {true, true},
	} {
		t.Run("", func(t *testing.T) {
			a, next := appWithControl(t)
			done := make(chan error, 1)
			go func() { done <- a.KillProject("p1", tc.kill, tc.ideas) }()

			ft, payload := next(t)
			if ft != wire.FrameKillProject {
				t.Fatalf("frame = %#x, want FrameKillProject", byte(ft))
			}
			var req wire.KillProjectReq
			if err := json.Unmarshal(payload, &req); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if req.ProjectID != "p1" || req.KillSessions != tc.kill || req.DeleteIdeas != tc.ideas {
				t.Errorf("req = %+v, want {p1 %v %v}", req, tc.kill, tc.ideas)
			}
			if err := <-done; err != nil {
				t.Fatalf("KillProject: %v", err)
			}
		})
	}
}

func strptr(s string) *string { return &s }

func checkStrPtr(t *testing.T, name string, got, want *string) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s = %q, want unset", name, *got)
	case want != nil && got == nil:
		t.Errorf("%s unset, want %q", name, *want)
	case want != nil && got != nil && *got != *want:
		t.Errorf("%s = %q, want %q", name, *got, *want)
	}
}
