package daemon

import (
	"encoding/json"
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
