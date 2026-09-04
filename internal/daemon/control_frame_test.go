package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// recordOps captures what a frame handler wrote, so a handler can be
// driven directly instead of through a socket. That seam is the point
// of splitting handleControlFrame out of serveControl: before it,
// asserting "a malformed payload answers bad_payload and keeps the
// connection open" meant standing up a daemon, dialing it, and
// round-tripping frames.
type recordOps struct {
	errs    []wire.Error
	frames  []wire.FrameType
	mutated []string
}

func (r *recordOps) ops() controlOps {
	return controlOps{
		writeJSON: func(t wire.FrameType, _ any) error {
			r.frames = append(r.frames, t)
			return nil
		},
		sendError: func(code, msg string) {
			r.errs = append(r.errs, wire.Error{Code: code, Message: msg})
		},
		sendWorktrees:  func(projectID, _ string) { r.mutated = append(r.mutated, projectID) },
		finishMutation: func(projectID string, _ error, _ string) { r.mutated = append(r.mutated, projectID) },
	}
}

func newFrameTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	skipOnWindows(t)
	tmp := shortTempDir(t)
	d, err := New(Config{
		SocketPath: filepath.Join(tmp, "s"),
		StateDir:   filepath.Join(tmp, "state"),
		BootstrapSession: session.Options{
			Shell: "/bin/bash", Cols: 80, Rows: 24,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// TestControlFrameBadPayload pins the prologue that decodeReq now
// owns for twelve frames: a payload that is not the frame's request
// type is answered with bad_payload, and the connection keeps reading
// rather than dropping the client.
func TestControlFrameBadPayload(t *testing.T) {
	d := newFrameTestDaemon(t)
	// A JSON array parses as JSON but never as any of the request
	// structs, so it exercises the unmarshal failure for every frame.
	bad := []byte(`["not","an","object"]`)
	frames := []wire.FrameType{
		wire.FrameCreateSession,
		wire.FrameKillSession,
		wire.FrameRestartSession,
		wire.FrameUpdateSession,
		wire.FrameCreateProject,
		wire.FrameKillProject,
		wire.FrameUpdateProject,
		wire.FrameListWorktrees,
		wire.FrameRemoveWorktree,
		wire.FrameCreateWorktree,
		wire.FrameDeleteBranch,
		wire.FrameRenameWorktree,
	}
	for _, ft := range frames {
		t.Run(fmt.Sprintf("frame_%#x", byte(ft)), func(t *testing.T) {
			rec := &recordOps{}
			if done := d.handleControlFrame(context.Background(), rec.ops(), ft, bad); done {
				t.Fatalf("frame %#x: a bad payload closed the connection", byte(ft))
			}
			if len(rec.errs) != 1 || rec.errs[0].Code != "bad_payload" {
				t.Fatalf("frame %#x: got %+v, want exactly one bad_payload error", byte(ft), rec.errs)
			}
			if len(rec.mutated) != 0 {
				t.Errorf("frame %#x: a bad payload still reached the registry: %v", byte(ft), rec.mutated)
			}
		})
	}
}

// TestControlFrameListsAnswerInPlace: the two list frames need no
// payload and answer on the same call, so they are the cheapest proof
// that the dispatch wiring survived the split.
func TestControlFrameListsAnswerInPlace(t *testing.T) {
	d := newFrameTestDaemon(t)
	for ft, want := range map[wire.FrameType]wire.FrameType{
		wire.FrameListSessions: wire.FrameSessions,
		wire.FrameListProjects: wire.FrameProjects,
	} {
		rec := &recordOps{}
		if done := d.handleControlFrame(context.Background(), rec.ops(), ft, nil); done {
			t.Fatalf("frame %#x closed the connection", byte(ft))
		}
		if len(rec.frames) != 1 || rec.frames[0] != want {
			t.Errorf("frame %#x: got frames %v, want [%#x]", byte(ft), rec.frames, byte(want))
		}
	}
}

// TestControlFrameUnknownIsIgnored: an unrecognised frame must not
// close the connection — a newer client sending a frame this daemon
// does not know is a version skew, not a protocol violation.
func TestControlFrameUnknownIsIgnored(t *testing.T) {
	d := newFrameTestDaemon(t)
	rec := &recordOps{}
	if done := d.handleControlFrame(context.Background(), rec.ops(), wire.FrameType(0xFE), nil); done {
		t.Fatal("an unknown frame closed the connection")
	}
	if len(rec.errs) != 0 || len(rec.frames) != 0 {
		t.Errorf("an unknown frame answered: errs=%v frames=%v", rec.errs, rec.frames)
	}
}

// TestDecodeReqEmptyPayload: jsonUnmarshal treats an empty payload as
// "leave the zero value alone", which several frames rely on for
// requests whose fields are all optional. Pinning it here keeps that
// permissiveness deliberate rather than incidental.
func TestDecodeReqEmptyPayload(t *testing.T) {
	var got []wire.Error
	send := func(code, msg string) { got = append(got, wire.Error{Code: code, Message: msg}) }

	req, ok := decodeReq[wire.ListWorktreesReq](nil, send)
	if !ok || len(got) != 0 {
		t.Fatalf("empty payload rejected: ok=%v errs=%v", ok, got)
	}
	if req.ProjectID != "" {
		t.Errorf("empty payload produced %+v, want the zero value", req)
	}

	raw, err := json.Marshal(wire.ListWorktreesReq{ProjectID: "p1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if req, ok = decodeReq[wire.ListWorktreesReq](raw, send); !ok || req.ProjectID != "p1" {
		t.Fatalf("round trip failed: ok=%v req=%+v errs=%v", ok, req, got)
	}
}

// --- undo close ---

// TestControlFrameRestoreBadPayload extends the bad_payload contract
// to the restore frame. Kept out of the table above because that test
// asserts the registry is untouched via `mutated`, which the restore
// path does not use.
func TestControlFrameRestoreBadPayload(t *testing.T) {
	d := newFrameTestDaemon(t)
	rec := &recordOps{}
	if done := d.handleControlFrame(context.Background(), rec.ops(),
		wire.FrameRestoreSession, []byte(`["not","an","object"]`)); done {
		t.Fatal("a bad payload closed the connection")
	}
	if len(rec.errs) != 1 || rec.errs[0].Code != "bad_payload" {
		t.Fatalf("got %+v, want exactly one bad_payload error", rec.errs)
	}
}

// TestControlFrameListClosedAnswersInPlace: LIST_CLOSED needs no
// payload and answers synchronously, like the other list frames.
func TestControlFrameListClosedAnswersInPlace(t *testing.T) {
	d := newFrameTestDaemon(t)
	rec := &recordOps{}
	if done := d.handleControlFrame(context.Background(), rec.ops(), wire.FrameListClosed, nil); done {
		t.Fatal("LIST_CLOSED closed the connection")
	}
	if len(rec.frames) != 1 || rec.frames[0] != wire.FrameClosed {
		t.Errorf("got frames %v, want [CLOSED]", rec.frames)
	}
}

// TestControlFrameRestoreUnknownIDSendsError: asking to reopen a
// session with no tombstone is a refusal the user can act on, not a
// generic failure — the GUI shows different copy for each.
func TestControlFrameRestoreUnknownIDSendsError(t *testing.T) {
	d := newFrameTestDaemon(t)
	rec := &recordOps{}
	payload, _ := json.Marshal(wire.RestoreSessionReq{SessionID: "no-such-session"})
	d.handleControlFrame(context.Background(), rec.ops(), wire.FrameRestoreSession, payload)
	d.ops.Wait()

	if len(rec.errs) != 1 || rec.errs[0].Code != "no_such_closed_session" {
		t.Fatalf("got %+v, want one no_such_closed_session error", rec.errs)
	}
	// A refused restore still refreshes the reopen list: a pruned or
	// already-restored tombstone is exactly why it failed.
	if len(rec.frames) != 1 || rec.frames[0] != wire.FrameClosed {
		t.Errorf("got frames %v, want [CLOSED] after a refusal", rec.frames)
	}
}

// TestControlFrameRestoreEmptyIDWithNothingClosed: the reopen-last
// affordance sends an empty id. With no tombstones at all that must
// say so rather than fall through to "not found" for a session the
// user never named.
func TestControlFrameRestoreEmptyIDWithNothingClosed(t *testing.T) {
	d := newFrameTestDaemon(t)
	rec := &recordOps{}
	payload, _ := json.Marshal(wire.RestoreSessionReq{})
	d.handleControlFrame(context.Background(), rec.ops(), wire.FrameRestoreSession, payload)
	d.ops.Wait()

	if len(rec.errs) != 1 || rec.errs[0].Code != "no_closed_sessions" {
		t.Fatalf("got %+v, want one no_closed_sessions error", rec.errs)
	}
}

// TestControlFrameRestoreRoundTrip drives a real close and reopen
// through the frame layer: the session comes back and the daemon
// reports what it could not restore.
func TestControlFrameRestoreRoundTrip(t *testing.T) {
	d := newFrameTestDaemon(t)
	reg := d.Registry()
	if _, err := reg.EnsureDefaultProject(t.TempDir()); err != nil {
		t.Fatalf("EnsureDefaultProject: %v", err)
	}
	e, err := reg.Create(context.Background(), wire.CreateSpec{Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := e.ID
	if err := reg.Kill(id, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	rec := &recordOps{}
	payload, _ := json.Marshal(wire.RestoreSessionReq{SessionID: id})
	d.handleControlFrame(context.Background(), rec.ops(), wire.FrameRestoreSession, payload)
	d.ops.Wait()
	defer reg.Kill(id, true)

	if len(rec.errs) != 0 {
		t.Fatalf("restore errored: %+v", rec.errs)
	}
	if reg.Get(id) == nil {
		t.Fatal("session did not come back")
	}
	// SESSION_RESTORED (what was degraded) then CLOSED (the refreshed
	// reopen list), in that order.
	want := []wire.FrameType{wire.FrameSessionRestored, wire.FrameClosed}
	if len(rec.frames) != len(want) {
		t.Fatalf("got frames %v, want %v", rec.frames, want)
	}
	for i, w := range want {
		if rec.frames[i] != w {
			t.Errorf("frame %d = %#x, want %#x", i, byte(rec.frames[i]), byte(w))
		}
	}
}

// TestControlFrameRestoreAlreadyOpen: reopening a session that is
// already open must be its own refusal, not a silent duplicate.
func TestControlFrameRestoreAlreadyOpen(t *testing.T) {
	d := newFrameTestDaemon(t)
	reg := d.Registry()
	if _, err := reg.EnsureDefaultProject(t.TempDir()); err != nil {
		t.Fatalf("EnsureDefaultProject: %v", err)
	}
	e, err := reg.Create(context.Background(), wire.CreateSpec{Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := e.ID
	if err := reg.Kill(id, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, _, err := reg.Restore(id, session.Options{Shell: "/bin/bash", Cols: 80, Rows: 24}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	defer reg.Kill(id, true)

	// The tombstone is gone after a successful restore, so the frame
	// path reports "no longer restorable" — which is the honest answer
	// and, importantly, is not a second copy of the session.
	rec := &recordOps{}
	payload, _ := json.Marshal(wire.RestoreSessionReq{SessionID: id})
	d.handleControlFrame(context.Background(), rec.ops(), wire.FrameRestoreSession, payload)
	d.ops.Wait()

	if len(rec.errs) != 1 {
		t.Fatalf("got %+v, want exactly one refusal", rec.errs)
	}
	if c := rec.errs[0].Code; c != "no_such_closed_session" && c != "session_already_open" {
		t.Errorf("refusal code = %q, want no_such_closed_session or session_already_open", c)
	}
	copies := 0
	for _, info := range reg.List() {
		if info.ID == id {
			copies++
		}
	}
	if copies != 1 {
		t.Errorf("registry holds %d copies of %s, want 1 — a second restore duplicated it", copies, id)
	}
}

// TestClientCommandRelaysToSubscribers pins the relay contract: a
// recognised verb reaches every control connection, and the daemon
// itself does nothing with it.
func TestClientCommandRelaysToSubscribers(t *testing.T) {
	d := newFrameTestDaemon(t)
	ch, unsub := d.commands.Subscribe()
	defer unsub()

	var r recordOps
	payload, _ := json.Marshal(wire.ClientCommand{Cmd: wire.CmdFocusSession, SessionID: "s7"})
	if stop := d.handleControlFrame(context.Background(), r.ops(), wire.FrameClientCommand, payload); stop {
		t.Fatal("a client command must not end the connection")
	}
	if len(r.errs) != 0 {
		t.Fatalf("unexpected errors: %+v", r.errs)
	}

	select {
	case got := <-ch:
		if got.Cmd != wire.CmdFocusSession || got.SessionID != "s7" {
			t.Errorf("relayed %+v, want focus_session/s7", got)
		}
	default:
		t.Fatal("command was not relayed to the subscriber")
	}
}

// An unrecognised verb is refused to its sender rather than fanned
// out. The daemon is the only thing every client holds a connection
// to, so a typo must not become a frame every window has to guess at.
func TestClientCommandRejectsUnknownVerb(t *testing.T) {
	d := newFrameTestDaemon(t)
	ch, unsub := d.commands.Subscribe()
	defer unsub()

	var r recordOps
	payload, _ := json.Marshal(wire.ClientCommand{Cmd: "rm_minus_rf"})
	if stop := d.handleControlFrame(context.Background(), r.ops(), wire.FrameClientCommand, payload); stop {
		t.Fatal("a rejected client command must not end the connection")
	}
	if len(r.errs) != 1 || r.errs[0].Code != "unknown_client_command" {
		t.Fatalf("errs = %+v, want one unknown_client_command", r.errs)
	}
	select {
	case got := <-ch:
		t.Fatalf("unknown verb was relayed anyway: %+v", got)
	default:
	}
}

// A frame this daemon does not know must be logged and ignored, never
// treated as fatal. That is what makes adding frames backward
// compatible, and it is why wire.PROTOCOL_VERSION does not have to be
// bumped for the client-command pair: an older daemon meeting a newer
// GUI's CLIENT_COMMAND keeps serving the connection.
func TestUnknownControlFrameKeepsConnectionAlive(t *testing.T) {
	d := newFrameTestDaemon(t)
	var r recordOps
	// 0x7e: not allocated now and not plausibly allocated soon.
	if stop := d.handleControlFrame(context.Background(), r.ops(), wire.FrameType(0x7e), []byte(`{}`)); stop {
		t.Fatal("an unknown frame must not end the connection")
	}
	if len(r.errs) != 0 || len(r.frames) != 0 {
		t.Errorf("unknown frame should be silent to the client: errs=%+v frames=%+v", r.errs, r.frames)
	}
}

// An UPDATE_SESSION that only clears the attention flag must not reach
// Registry.Update: that path persists the entry, rewrites the index and
// broadcasts "updated". Focusing a session happens many times a minute,
// and it records something that is never persisted at all.
func TestUpdateSessionAttentionOnlySkipsPersistence(t *testing.T) {
	d := newFrameTestDaemon(t)
	sessions := d.reg.List()
	if len(sessions) == 0 {
		t.Fatal("expected the bootstrap session")
	}
	id := sessions[0].ID

	ch, unsub := d.reg.Subscribe()
	defer unsub()
	for len(ch) > 0 {
		<-ch
	}

	clear := false
	var r recordOps
	payload, _ := json.Marshal(wire.UpdateSessionReq{
		SessionID: id, NeedsAttention: &clear,
	})
	d.handleControlFrame(context.Background(), r.ops(), wire.FrameUpdateSession, payload)

	if len(r.errs) != 0 {
		t.Fatalf("unexpected errors: %+v", r.errs)
	}
	// Already clear, so SetAttention is a no-op and Update must not have
	// run — any event at all here means one of them fired.
	select {
	case ev := <-ch:
		t.Errorf("clearing an already-clear flag broadcast %s", ev.Kind)
	default:
	}
}

func TestUpdateSessionAttentionEmitsAttentionKind(t *testing.T) {
	d := newFrameTestDaemon(t)
	sessions := d.reg.List()
	if len(sessions) == 0 {
		t.Fatal("expected the bootstrap session")
	}
	id := sessions[0].ID

	ch, unsub := d.reg.Subscribe()
	defer unsub()
	for len(ch) > 0 {
		<-ch
	}

	set := true
	var r recordOps
	payload, _ := json.Marshal(wire.UpdateSessionReq{
		SessionID: id, NeedsAttention: &set,
	})
	d.handleControlFrame(context.Background(), r.ops(), wire.FrameUpdateSession, payload)

	select {
	case ev := <-ch:
		if ev.Kind != wire.SessionEventAttention {
			t.Errorf("kind = %q, want %q — clients that re-render on "+
				"\"updated\" must not be woken by a bell", ev.Kind, wire.SessionEventAttention)
		}
	default:
		t.Fatal("no event")
	}
}
