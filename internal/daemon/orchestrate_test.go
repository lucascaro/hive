package daemon

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// orchFixture is a daemon with a granted sender and an agent-session
// sibling in one project, plus helpers to push SEND_TO_SESSION frames
// through handleControlFrame the way a ModeSession connection would.
type orchFixture struct {
	d      *Daemon
	sender wire.SessionInfo
	target wire.SessionInfo
}

func newOrchFixture(t *testing.T, granted bool) *orchFixture {
	t.Helper()
	d := newFrameTestDaemon(t)
	mk := func(name string, grant bool) wire.SessionInfo {
		e, err := d.reg.Create(context.Background(), wire.CreateSpec{
			Name: name, Agent: "claude", Cmd: []string{"/bin/cat"},
			Cols: 80, Rows: 24, Orchestrator: grant,
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return e.Info()
	}
	f := &orchFixture{d: d}
	f.sender = mk("boss", granted)
	f.target = mk("worker", false)
	return f
}

// send runs one frame as the sender's session connection and returns the
// ERROR it produced, or nil on a SENT.
func (f *orchFixture) send(t *testing.T, ref, text string, mutate ...func(*controlOps)) *wire.Error {
	t.Helper()
	ops, errs, frames := asyncOps("")
	ops.restricted = true
	ops.ownSessionID = f.sender.ID
	ops.ownProjectID = f.sender.ProjectID
	for _, m := range mutate {
		m(&ops)
	}
	f.d.handleControlFrame(context.Background(), ops, wire.FrameSendToSession, sendReq(t, ref, text))
	select {
	case e := <-errs:
		return &e
	case ft := <-frames:
		if ft != wire.FrameSent {
			t.Fatalf("got frame %v, want SENT", ft)
		}
		return nil
	case <-time.After(3 * time.Second):
		t.Fatal("no reply")
	}
	return nil
}

func TestSessionModeSendRequiresGrant(t *testing.T) {
	f := newOrchFixture(t, false)
	e := f.send(t, f.target.ID, "hi")
	if e == nil || e.Code != wire.ErrCodeNotOrchestrator {
		t.Fatalf("got %+v, want not_orchestrator", e)
	}
}

func TestSessionModeSendDelivers(t *testing.T) {
	f := newOrchFixture(t, true)
	if e := f.send(t, f.target.ID, "hello"); e != nil {
		t.Fatalf("granted send refused: %+v", e)
	}
	// By exact name too, and a client-claimed queue=false is irrelevant.
	if e := f.send(t, "worker", "again"); e != nil {
		t.Fatalf("send by name refused: %+v", e)
	}
}

// Revocation must bite on the very next frame of an already-open
// connection: the grant is read per frame, not cached at handshake.
func TestSessionModeSendRevokedMidConnection(t *testing.T) {
	f := newOrchFixture(t, true)
	if e := f.send(t, f.target.ID, "one"); e != nil {
		t.Fatalf("first send refused: %+v", e)
	}
	off := false
	ctl, _, _ := asyncOps("")
	f.d.handleControlFrame(context.Background(), ctl, wire.FrameUpdateSession, mustJSON(t, wire.UpdateSessionReq{
		SessionID: f.sender.ID, Orchestrator: &off,
	}))
	if e := f.send(t, f.target.ID, "two"); e == nil || e.Code != wire.ErrCodeNotOrchestrator {
		t.Fatalf("after revoke got %+v, want not_orchestrator", e)
	}
}

func TestSessionModeSendOtherProjectNotFound(t *testing.T) {
	f := newOrchFixture(t, true)
	p, err := f.d.reg.CreateProject(wire.CreateProjectReq{Name: "other", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	abroad, err := f.d.reg.Create(context.Background(), wire.CreateSpec{
		Name: "abroad", Agent: "claude", Cmd: []string{"/bin/cat"}, Cols: 80, Rows: 24, ProjectID: p.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{abroad.ID, "abroad"} {
		if e := f.send(t, ref, "x"); e == nil || e.Code != "no_such_session" {
			t.Errorf("ref %q: got %+v, want no_such_session", ref, e)
		}
	}
}

func TestSessionModeSendSelfRefused(t *testing.T) {
	f := newOrchFixture(t, true)
	for _, ref := range []string{f.sender.ID, "boss"} {
		if e := f.send(t, ref, "x"); e == nil || e.Code != "bad_payload" {
			t.Errorf("ref %q: got %+v, want bad_payload", ref, e)
		}
	}
}

func TestSessionModeSendEmptyBodyRefused(t *testing.T) {
	f := newOrchFixture(t, true)
	if e := f.send(t, f.target.ID, "   "); e == nil || e.Code != "bad_payload" {
		t.Fatalf("got %+v, want bad_payload", e)
	}
}

func TestPluginCannotSetOrchestrator(t *testing.T) {
	f := newOrchFixture(t, false)
	on := true
	ops, errs, _ := asyncOps("plugin:acme")
	f.d.handleControlFrame(context.Background(), ops, wire.FrameUpdateSession, mustJSON(t, wire.UpdateSessionReq{
		SessionID: f.sender.ID, Orchestrator: &on,
	}))
	select {
	case e := <-errs:
		if e.Code != wire.ErrCodeModeNotAllowed {
			t.Fatalf("code = %q, want mode_not_allowed", e.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("update by a plugin was not refused")
	}
	if f.d.reg.Get(f.sender.ID).Info().Orchestrator {
		t.Fatal("a plugin granted orchestration")
	}
	// A create from a plugin arrives ungranted whatever the spec says.
	f.d.handleControlFrame(context.Background(), ops, wire.FrameCreateSession, mustJSON(t, wire.CreateSpec{
		Name: "spawned", Agent: "claude", Cmd: []string{"/bin/cat"}, Cols: 80, Rows: 24, Orchestrator: true,
	}))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range f.d.reg.List() {
			if s.Name == "spawned" {
				if s.Orchestrator {
					t.Fatal("a plugin-created session came up granted")
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("plugin create never produced a session")
}

func TestSessionModeSendLogLines(t *testing.T) {
	f := newOrchFixture(t, true)
	var buf syncBuf
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })

	if e := f.send(t, "worker", "from a session"); e != nil {
		t.Fatalf("send: %+v", e)
	}
	// The control caller may be told the target is busy; either reply
	// leaves the evidence line, which is what this test is about.
	ctl, errs, frames := asyncOps("")
	f.d.handleControlFrame(context.Background(), ctl, wire.FrameSendToSession, sendReq(t, f.target.ID, "from the user"))
	select {
	case <-frames:
	case <-errs:
	case <-time.After(3 * time.Second):
		t.Fatal("control send never answered")
	}
	got := buf.String()
	for _, want := range []string{
		"send_to_session caller=session from=" + f.sender.ID + ` to="` + f.target.ID + `" delivery=`,
		"send_to_session caller=control from=user to=\"" + f.target.ID + "\" delivery=",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("log missing %q in:\n%s", want, got)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
