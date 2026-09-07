package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
)

// controlConn opens a control connection and drains the unsolicited
// PROJECTS/SESSIONS snapshot so the caller's next read is the reply to
// its own request.
func controlConn(t *testing.T, d *Daemon) net.Conn {
	t.Helper()
	conn := dial(t, d)
	t.Cleanup(func() { _ = conn.Close() })
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	return conn
}

// awaitFrame reads until one of the wanted frame types arrives,
// skipping the snapshots and events that share the connection.
func awaitFrame(t *testing.T, conn net.Conn, want ...wire.FrameType) (wire.FrameType, []byte) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			t.Fatalf("read frame (waiting for %v): %v", want, err)
		}
		for _, w := range want {
			if ft == w {
				return ft, payload
			}
		}
	}
}

func TestListIdeasReply(t *testing.T) {
	// Unix socket, and startTestDaemon's temp dir is under /tmp.
	skipOnWindows(t)
	d := startTestDaemon(t)
	conn := controlConn(t, d)

	if err := wire.WriteJSON(conn, wire.FrameListIdeas, wire.ListIdeasReq{}); err != nil {
		t.Fatalf("write LIST_IDEAS: %v", err)
	}
	_, payload := awaitFrame(t, conn, wire.FrameIdeas)
	var resp wire.IdeasResp
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("decode IDEAS: %v", err)
	}
	if len(resp.Ideas) != 0 {
		t.Fatalf("fresh daemon has ideas: %+v", resp.Ideas)
	}

	if err := wire.WriteJSON(conn, wire.FrameAddIdea, wire.AddIdeaReq{
		Kind: wire.IdeaKindBug,
		Text: "the grid loses focus",
	}); err != nil {
		t.Fatalf("write ADD_IDEA: %v", err)
	}
	// Fan-out first: every control connection hears the add.
	_, payload = awaitFrame(t, conn, wire.FrameIdeaEvent)
	var ev wire.IdeaEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		t.Fatalf("decode IDEA_EVENT: %v", err)
	}
	if ev.Kind != wire.IdeaEventAdded || ev.Idea.Text != "the grid loses focus" {
		t.Fatalf("event = %+v", ev)
	}
	if ev.Idea.Kind != wire.IdeaKindBug || ev.Idea.Status != wire.IdeaStatusOpen {
		t.Errorf("idea = %+v", ev.Idea)
	}

	if err := wire.WriteJSON(conn, wire.FrameListIdeas, wire.ListIdeasReq{}); err != nil {
		t.Fatalf("write LIST_IDEAS: %v", err)
	}
	_, payload = awaitFrame(t, conn, wire.FrameIdeas)
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("decode IDEAS: %v", err)
	}
	if len(resp.Ideas) != 1 || resp.Ideas[0].ID != ev.Idea.ID {
		t.Fatalf("LIST_IDEAS = %+v", resp.Ideas)
	}
}

// A second control connection must hear an idea filed on the first —
// this is the path the GUI's inbox updates through.
func TestIdeaEventFanOut(t *testing.T) {
	// Unix socket, and startTestDaemon's temp dir is under /tmp.
	skipOnWindows(t)
	d := startTestDaemon(t)
	filer := controlConn(t, d)
	watcher := controlConn(t, d)

	if err := wire.WriteJSON(filer, wire.FrameAddIdea, wire.AddIdeaReq{Text: "heard elsewhere"}); err != nil {
		t.Fatalf("write ADD_IDEA: %v", err)
	}
	_, payload := awaitFrame(t, watcher, wire.FrameIdeaEvent)
	var ev wire.IdeaEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		t.Fatalf("decode IDEA_EVENT: %v", err)
	}
	if ev.Kind != wire.IdeaEventAdded || ev.Idea.Text != "heard elsewhere" {
		t.Fatalf("watcher saw %+v", ev)
	}

	// …and the same for update and remove.
	done := wire.IdeaStatusDone
	if err := wire.WriteJSON(filer, wire.FrameUpdateIdea, wire.UpdateIdeaReq{ID: ev.Idea.ID, Status: &done}); err != nil {
		t.Fatalf("write UPDATE_IDEA: %v", err)
	}
	_, payload = awaitFrame(t, watcher, wire.FrameIdeaEvent)
	_ = json.Unmarshal(payload, &ev)
	if ev.Kind != wire.IdeaEventUpdated || ev.Idea.Status != wire.IdeaStatusDone {
		t.Fatalf("watcher saw %+v after update", ev)
	}

	if err := wire.WriteJSON(filer, wire.FrameRemoveIdea, wire.RemoveIdeaReq{ID: ev.Idea.ID}); err != nil {
		t.Fatalf("write REMOVE_IDEA: %v", err)
	}
	_, payload = awaitFrame(t, watcher, wire.FrameIdeaEvent)
	_ = json.Unmarshal(payload, &ev)
	if ev.Kind != wire.IdeaEventRemoved {
		t.Fatalf("watcher saw %+v after remove", ev)
	}
}

func TestAddIdeaTooLongRefused(t *testing.T) {
	// Unix socket, and startTestDaemon's temp dir is under /tmp.
	skipOnWindows(t)
	d := startTestDaemon(t)
	conn := controlConn(t, d)

	long := make([]byte, wire.MaxIdeaText+1)
	for i := range long {
		long[i] = 'x'
	}
	if err := wire.WriteJSON(conn, wire.FrameAddIdea, wire.AddIdeaReq{Text: string(long)}); err != nil {
		t.Fatalf("write ADD_IDEA: %v", err)
	}
	_, payload := awaitFrame(t, conn, wire.FrameError)
	var e wire.Error
	if err := json.Unmarshal(payload, &e); err != nil {
		t.Fatalf("decode ERROR: %v", err)
	}
	if e.Code != wire.ErrCodeIdeaTooLong {
		t.Fatalf("code = %q, want %q", e.Code, wire.ErrCodeIdeaTooLong)
	}
}

// TestCloseGuardRefusesAndForces is what stops the shared close-guard
// refactor from regressing either of the two refusals it now serves.
// Both have the same contract: refuse without the force flag, carry
// the id the client needs to retry, proceed with it.
func TestCloseGuardRefusesAndForces(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		session string
		project string
		want    wire.Error
	}{
		{
			name:    "dirty worktree is session scoped",
			err:     registry.ErrWorktreeDirty,
			session: "s1",
			want: wire.Error{
				Code:      wire.ErrCodeWorktreeDirty,
				Message:   "worktree has uncommitted changes",
				SessionID: "s1",
			},
		},
		{
			name:    "open ideas is project scoped",
			err:     registry.ErrProjectHasIdeas,
			project: "p1",
			want: wire.Error{
				Code:      wire.ErrCodeProjectHasIdeas,
				Message:   registry.ErrProjectHasIdeas.Error(),
				ProjectID: "p1",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := closeGuardError(tc.err, tc.session, tc.project)
			if !ok {
				t.Fatalf("%v is not recognised as a close-guard refusal", tc.err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	// Anything else falls through to the caller's generic code rather
	// than being dressed up as a confirmable refusal.
	if _, ok := closeGuardError(registry.ErrNotFound, "s", "p"); ok {
		t.Error("ErrNotFound was treated as a close-guard refusal")
	}
}

// The daemon end of the same contract: KILL_PROJECT refuses while
// ideas are open and goes through once the client retries with the
// force flag.
func TestKillProjectRefusesOpenIdeasOverWire(t *testing.T) {
	// Unix socket, and startTestDaemon's temp dir is under /tmp.
	skipOnWindows(t)
	d := startTestDaemon(t)
	conn := controlConn(t, d)

	if err := wire.WriteJSON(conn, wire.FrameCreateProject, wire.CreateProjectReq{Name: "doomed"}); err != nil {
		t.Fatalf("write CREATE_PROJECT: %v", err)
	}
	_, payload := awaitFrame(t, conn, wire.FrameProjectEvent)
	var pev wire.ProjectEvent
	if err := json.Unmarshal(payload, &pev); err != nil {
		t.Fatalf("decode PROJECT_EVENT: %v", err)
	}
	doomed := pev.Project.ID

	if err := wire.WriteJSON(conn, wire.FrameAddIdea, wire.AddIdeaReq{
		ProjectID: doomed, Text: "still open",
	}); err != nil {
		t.Fatalf("write ADD_IDEA: %v", err)
	}
	awaitFrame(t, conn, wire.FrameIdeaEvent)

	if err := wire.WriteJSON(conn, wire.FrameKillProject, wire.KillProjectReq{ProjectID: doomed}); err != nil {
		t.Fatalf("write KILL_PROJECT: %v", err)
	}
	_, payload = awaitFrame(t, conn, wire.FrameError)
	var e wire.Error
	if err := json.Unmarshal(payload, &e); err != nil {
		t.Fatalf("decode ERROR: %v", err)
	}
	if e.Code != wire.ErrCodeProjectHasIdeas {
		t.Fatalf("code = %q, want %q", e.Code, wire.ErrCodeProjectHasIdeas)
	}
	if e.ProjectID != doomed {
		t.Errorf("refusal carries project %q, want %q — the client cannot retry without it", e.ProjectID, doomed)
	}

	if err := wire.WriteJSON(conn, wire.FrameKillProject, wire.KillProjectReq{
		ProjectID: doomed, DeleteIdeas: true,
	}); err != nil {
		t.Fatalf("write KILL_PROJECT(force): %v", err)
	}
	// The idea goes with the project.
	_, payload = awaitFrame(t, conn, wire.FrameIdeaEvent)
	var iev wire.IdeaEvent
	if err := json.Unmarshal(payload, &iev); err != nil {
		t.Fatalf("decode IDEA_EVENT: %v", err)
	}
	if iev.Kind != wire.IdeaEventRemoved {
		t.Fatalf("event after forced delete = %+v", iev)
	}
}

// ideaErrorCode's no_such_idea / no_such_project branches are what a
// client keys off to tell "gone" from "broken"; only the too-long
// branch was covered over the wire before.
func TestIdeaErrorCodesOverWire(t *testing.T) {
	// Unix socket, and startTestDaemon's temp dir is under /tmp.
	skipOnWindows(t)
	d := startTestDaemon(t)
	conn := controlConn(t, d)

	done := wire.IdeaStatusDone
	cases := []struct {
		name  string
		frame wire.FrameType
		req   any
		want  string
	}{
		{"update unknown idea", wire.FrameUpdateIdea,
			wire.UpdateIdeaReq{ID: "nope", Status: &done}, "no_such_idea"},
		{"remove unknown idea", wire.FrameRemoveIdea,
			wire.RemoveIdeaReq{ID: "nope"}, "no_such_idea"},
		{"add to unknown project", wire.FrameAddIdea,
			wire.AddIdeaReq{ProjectID: "nope", Text: "homeless"}, "no_such_project"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := wire.WriteJSON(conn, tc.frame, tc.req); err != nil {
				t.Fatalf("write %v: %v", tc.frame, err)
			}
			_, payload := awaitFrame(t, conn, wire.FrameError)
			var e wire.Error
			if err := json.Unmarshal(payload, &e); err != nil {
				t.Fatalf("decode ERROR: %v", err)
			}
			if e.Code != tc.want {
				t.Fatalf("code = %q, want %q", e.Code, tc.want)
			}
		})
	}
}

// The daemon's RESOLVE_PROMPT arm has one decision in it, and it is a
// policy rather than a forward: ErrNotFound is SWALLOWED (an unknown id
// is a benign race with a close) while ErrNoLiveSession is SURFACED
// (the session is real, the offer is still standing, and the user has
// to be told why nothing was pasted). Handling those alike is exactly
// how the earlier silent loss shipped.
//
// The first version of this test asserted nothing: awaitFrame LOOPS
// PAST every frame not in its want list, FrameError included, so the
// `ft == FrameError` check after it was unreachable and the test
// passed whether or not the daemon swallowed anything. Both halves now
// name FrameError in the want list, which is what makes the assertion
// reachable at all.
func TestResolvePromptErrorPolicyOverWire(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	conn := controlConn(t, d)

	// Unknown session: swallowed. Proven by following it with a frame
	// that DOES answer and requiring the reply to be that one — with
	// FrameError in the want list, so an error would win the race
	// rather than being skipped over.
	if err := wire.WriteJSON(conn, wire.FrameResolvePrompt, wire.ResolvePromptReq{
		SessionID: "no-such-session", Paste: true,
	}); err != nil {
		t.Fatalf("write RESOLVE_PROMPT: %v", err)
	}
	if err := wire.WriteJSON(conn, wire.FrameCreateProject, wire.CreateProjectReq{
		Name: "after",
	}); err != nil {
		t.Fatalf("write CREATE_PROJECT: %v", err)
	}
	ft, _ := awaitFrame(t, conn, wire.FrameProjectEvent, wire.FrameError)
	if ft == wire.FrameError {
		t.Fatal("an unknown session id produced an error; it is a benign race with a close and must be swallowed")
	}

}

// The other half of the same policy: WHICH code a failure is reported
// under. Tested on the mapping rather than over the wire because
// reaching a session whose process has died requires registry
// internals this package cannot reach — and the mapping is the part
// that matters, since the client tells "the note is safe, try again"
// from "the note is gone" by this code alone.
func TestResolvePromptErrorCode(t *testing.T) {
	if got := resolvePromptErrorCode(registry.ErrNoLiveSession); got != wire.ErrCodeNoLiveSession {
		t.Errorf("ErrNoLiveSession -> %q, want %q", got, wire.ErrCodeNoLiveSession)
	}
	// Wrapped, because the registry returns fmt.Errorf-wrapped errors
	// elsewhere on this path and errors.Is is what the arm relies on.
	wrapped := fmt.Errorf("resolving: %w", registry.ErrNoLiveSession)
	if got := resolvePromptErrorCode(wrapped); got != wire.ErrCodeNoLiveSession {
		t.Errorf("wrapped ErrNoLiveSession -> %q, want %q", got, wire.ErrCodeNoLiveSession)
	}
	// Anything else has already cleared the prompt, so it must NOT be
	// reported as the retryable one.
	if got := resolvePromptErrorCode(errors.New("pty gone")); got != "resolve_prompt_failed" {
		t.Errorf("generic failure -> %q, want resolve_prompt_failed", got)
	}
}
