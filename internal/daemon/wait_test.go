package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/plugin"
	"github.com/lucascaro/hive/internal/wire"
)

// waitCall is one WAIT_SESSION run through handleControlFrame as the
// boss's session connection, answered on res.
type waitCall struct {
	res chan waitResult
}

type waitResult struct {
	resp wire.WaitedResp
	err  *wire.Error
}

// startWait issues WAIT_SESSION from the fixture's sender.
func (f *orchFixture) startWait(t *testing.T, req wire.WaitSessionReq, mutate ...func(*controlOps)) *waitCall {
	t.Helper()
	c := &waitCall{res: make(chan waitResult, 1)}
	ops := controlOps{
		restricted:   true,
		ownSessionID: f.sender.ID,
		ownProjectID: f.sender.ProjectID,
		writeJSON: func(ft wire.FrameType, v any) error {
			if ft != wire.FrameWaited {
				t.Errorf("wrote %v, want WAITED", ft)
			}
			c.res <- waitResult{resp: v.(wire.WaitedResp)}
			return nil
		},
		sendError: func(code, msg string) { c.res <- waitResult{err: &wire.Error{Code: code, Message: msg}} },
	}
	for _, m := range mutate {
		m(&ops)
	}
	go f.d.handleControlFrame(context.Background(), ops, wire.FrameWaitSession, mustJSON(t, req))
	return c
}

func (c *waitCall) get(t *testing.T, within time.Duration) waitResult {
	t.Helper()
	select {
	case r := <-c.res:
		return r
	case <-time.After(within):
		t.Fatal("wait did not answer")
	}
	return waitResult{}
}

// quiet asserts the wait has NOT answered yet.
func (c *waitCall) quiet(t *testing.T, for_ time.Duration) {
	t.Helper()
	select {
	case r := <-c.res:
		t.Fatalf("wait answered early: %+v", r)
	case <-time.After(for_):
	}
}

// report applies one agent event to the fixture's target — what an
// AGENT_EVENT frame does once the daemon has validated it. Applied
// directly because this fixture's daemon is never Run, so a frame
// written to its socket would sit unread.
func (f *orchFixture) report(t *testing.T, kind string) {
	t.Helper()
	ev := wire.AgentEvent{SessionID: f.target.ID, Kind: kind, Source: wire.StateSourceHook}
	if err := f.d.reg.ApplyAgentEvent(f.target.ID, ev); err != nil {
		t.Fatalf("apply %s: %v", kind, err)
	}
}

func (f *orchFixture) stateIs(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s, ok := f.d.sessionInfo(f.target.ID); ok && s.State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("target never reached state %q", want)
}

func TestRestrictedListSessionsProjectScopedAndProjected(t *testing.T) {
	f := newOrchFixture(t, false)
	p, err := f.d.reg.CreateProject(wire.CreateProjectReq{Name: "other", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.reg.Create(context.Background(), wire.CreateSpec{
		Name: "abroad", Agent: "claude", Cmd: []string{"/bin/cat"}, Cols: 80, Rows: 24, ProjectID: p.ID,
	}); err != nil {
		t.Fatal(err)
	}

	ops := controlOps{restricted: true, ownSessionID: f.sender.ID, ownProjectID: f.sender.ProjectID}
	var got wire.SessionsResp
	ops.writeJSON = func(ft wire.FrameType, v any) error {
		if ft != wire.FrameSessions {
			t.Errorf("wrote %v, want SESSIONS", ft)
		}
		got = v.(wire.SessionsResp)
		return nil
	}
	f.d.handleControlFrame(context.Background(), ops, wire.FrameListSessions, []byte("{}"))

	// The caller's project holds the bootstrap shell, boss and worker.
	if len(got.Sessions) != 3 {
		t.Fatalf("got %d sessions, want the caller's project's 3: %+v", len(got.Sessions), got.Sessions)
	}
	for _, s := range got.Sessions {
		if s.Name == "abroad" {
			t.Errorf("other project's session leaked: %+v", s)
		}
		// Exactly id, name, agent, state: everything else must be the
		// zero value, which is what withholding it looks like.
		blank := wire.SessionInfo{ID: s.ID, Name: s.Name, Agent: s.Agent, State: s.State}
		if a, b := mustJSON(t, s), mustJSON(t, blank); string(a) != string(b) {
			t.Errorf("projection carries more than id,name,agent,state: %s", a)
		}
		if (s.Name == "boss" || s.Name == "worker") && s.Agent != "claude" {
			t.Errorf("%s agent = %q, want claude", s.Name, s.Agent)
		}
	}

	// A control (unrestricted) connection still gets everything.
	ops.restricted = false
	f.d.handleControlFrame(context.Background(), ops, wire.FrameListSessions, []byte("{}"))
	if len(got.Sessions) != 4 {
		t.Errorf("control list got %d sessions, want 4", len(got.Sessions))
	}
}

func TestRestrictedListWithUnknownCallerIsEmpty(t *testing.T) {
	if got := ownProjectOnly([]wire.SessionInfo{{ID: "a", ProjectID: ""}}, ""); got != nil {
		t.Errorf("an unresolved caller listed %+v", got)
	}
}

func TestWaitSessionReturnsOnAgentEvent(t *testing.T) {
	f := newOrchFixture(t, false)
	var buf syncBuf
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })

	f.report(t, wire.AgentEventPrompt)
	f.stateIs(t, wire.StateWorking)
	c := f.startWait(t, wire.WaitSessionReq{SessionID: "worker"})
	c.quiet(t, 150*time.Millisecond)
	f.report(t, wire.AgentEventIdle)

	r := c.get(t, 3*time.Second)
	if r.err != nil || r.resp.State != "idle" || r.resp.SessionID != f.target.ID {
		t.Fatalf("got %+v, want idle for %s", r, f.target.ID)
	}
	// Logged just after the answer is written, so poll for it.
	want := "wait caller=session from=" + f.sender.ID + " to=" + f.target.ID + " reached=idle"
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(buf.String(), want) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), want) {
		t.Errorf("log missing %q in:\n%s", want, buf.String())
	}
}

func TestWaitSessionAlreadySatisfiedReturnsImmediately(t *testing.T) {
	f := newOrchFixture(t, false)
	r := f.startWait(t, wire.WaitSessionReq{SessionID: f.target.ID}).get(t, time.Second)
	if r.err != nil || r.resp.State != "idle" {
		t.Fatalf("got %+v, want an immediate idle", r)
	}
}

func TestWaitSessionOnlyNamedStatesEndIt(t *testing.T) {
	f := newOrchFixture(t, false)
	c := f.startWait(t, wire.WaitSessionReq{SessionID: f.target.ID, States: []string{"working"}})
	c.quiet(t, 150*time.Millisecond) // idle is not on the list
	f.report(t, wire.AgentEventPrompt)
	if r := c.get(t, 3*time.Second); r.err != nil || r.resp.State != wire.StateWorking {
		t.Fatalf("got %+v, want working", r)
	}
}

func TestWaitSessionTimeout(t *testing.T) {
	f := newOrchFixture(t, false)
	f.report(t, wire.AgentEventPrompt)
	f.stateIs(t, wire.StateWorking)
	start := time.Now()
	r := f.startWait(t, wire.WaitSessionReq{SessionID: f.target.ID, States: []string{wire.StateError}, TimeoutSeconds: 1}).get(t, 4*time.Second)
	if r.err != nil || r.resp.State != wire.WaitStateTimeout {
		t.Fatalf("got %+v, want timeout", r)
	}
	if time.Since(start) < 900*time.Millisecond {
		t.Errorf("timed out after %s, want ~1s", time.Since(start))
	}
}

// A wait issued right after `hived msg` must not return on the idle that
// predates the agent picking the note up.
func TestWaitSessionAfterWorkingIgnoresStaleIdle(t *testing.T) {
	f := newOrchFixture(t, false)
	c := f.startWait(t, wire.WaitSessionReq{SessionID: f.target.ID, AfterWorking: true})
	c.quiet(t, 200*time.Millisecond)
	f.report(t, wire.AgentEventPrompt)
	f.report(t, wire.AgentEventIdle)
	if r := c.get(t, 3*time.Second); r.err != nil || r.resp.State != "idle" {
		t.Fatalf("got %+v, want idle after the work", r)
	}
}

func TestWaitSessionExited(t *testing.T) {
	f := newOrchFixture(t, false)
	e, err := f.d.reg.Create(context.Background(), wire.CreateSpec{
		Name: "short", Agent: "claude", Cmd: []string{"/bin/sh", "-c", "sleep 0.4"}, Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.target = e.Info()
	r := f.startWait(t, wire.WaitSessionReq{SessionID: "short", States: []string{"exited"}}).get(t, 5*time.Second)
	if r.err != nil || r.resp.State != wire.StateExited {
		t.Fatalf("got %+v, want exited", r)
	}
}

func TestWaitSessionTargetRemovedIsGone(t *testing.T) {
	f := newOrchFixture(t, false)
	f.report(t, wire.AgentEventPrompt)
	f.stateIs(t, wire.StateWorking)
	c := f.startWait(t, wire.WaitSessionReq{SessionID: f.target.ID, States: []string{wire.StateError}})
	c.quiet(t, 100*time.Millisecond)
	if err := f.d.reg.Kill(f.target.ID, true); err != nil {
		t.Fatal(err)
	}
	if r := c.get(t, 3*time.Second); r.err != nil || r.resp.State != wire.WaitStateGone {
		t.Fatalf("got %+v, want gone", r)
	}
}

func TestWaitSessionFailsFast(t *testing.T) {
	f := newOrchFixture(t, false)
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
	for _, n := range []string{"dup", "dup"} {
		if _, err := f.d.reg.Create(context.Background(), wire.CreateSpec{
			Name: n, Agent: "claude", Cmd: []string{"/bin/cat"}, Cols: 80, Rows: 24,
		}); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name string
		req  wire.WaitSessionReq
		code string
	}{
		{"missing", wire.WaitSessionReq{SessionID: "ghost"}, "no_such_session"},
		{"other project by id", wire.WaitSessionReq{SessionID: abroad.ID}, "no_such_session"},
		{"other project by name", wire.WaitSessionReq{SessionID: "abroad"}, "no_such_session"},
		{"self", wire.WaitSessionReq{SessionID: f.sender.ID}, "bad_payload"},
		{"ambiguous name", wire.WaitSessionReq{SessionID: "dup"}, "bad_payload"},
		{"unknown state", wire.WaitSessionReq{SessionID: "worker", States: []string{"asleep"}}, "bad_payload"},
		{"gone is not askable", wire.WaitSessionReq{SessionID: "worker", States: []string{"gone"}}, "bad_payload"},
		{"negative timeout", wire.WaitSessionReq{SessionID: "worker", TimeoutSeconds: -1}, "bad_payload"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := f.startWait(t, tc.req).get(t, time.Second)
			if r.err == nil || r.err.Code != tc.code {
				t.Fatalf("got %+v, want error %s", r, tc.code)
			}
		})
	}
}

func TestWaitSessionRefusedOnControlConnection(t *testing.T) {
	f := newOrchFixture(t, false)
	r := f.startWait(t, wire.WaitSessionReq{SessionID: "worker"}, func(o *controlOps) { o.restricted = false }).get(t, time.Second)
	if r.err == nil || r.err.Code != wire.ErrCodeModeNotAllowed {
		t.Fatalf("got %+v, want mode_not_allowed", r)
	}
}

func TestWaitSessionBadPayloadAnswersBeforeBlocking(t *testing.T) {
	f := newOrchFixture(t, false)
	ops, errs, _ := asyncOps("")
	ops.restricted, ops.ownSessionID, ops.ownProjectID = true, f.sender.ID, f.sender.ProjectID
	f.d.handleControlFrame(context.Background(), ops, wire.FrameWaitSession, []byte("not json"))
	select {
	case e := <-errs:
		if e.Code != "bad_payload" {
			t.Fatalf("got %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked on a bad payload")
	}
}

// The registry drops a listener that falls behind. A wait must notice and
// resync from the live state rather than hang on a missed transition.
func TestWaitSessionListenerDroppedResyncs(t *testing.T) {
	f := newOrchFixture(t, false)
	f.report(t, wire.AgentEventPrompt)
	f.stateIs(t, wire.StateWorking)

	// The first event the wait takes parks its consumer in the hook until
	// released, which is what lets the registry's buffer overflow.
	taken, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	waitEventHook = func() {
		once.Do(func() {
			taken <- struct{}{}
			<-release
		})
	}
	t.Cleanup(func() { waitEventHook = nil })

	c := f.startWait(t, wire.WaitSessionReq{SessionID: f.target.ID})
	// A rename is an event for the target the wait is not interested in.
	rename := func(n int) {
		name := fmt.Sprintf("worker-%d", n)
		if _, err := f.d.reg.Update(wire.UpdateSessionReq{SessionID: f.target.ID, Name: &name}); err != nil {
			t.Fatal(err)
		}
	}
	// Events sent before the wait subscribed are not delivered to it, so
	// keep renaming until the consumer takes one and parks.
	n := 0
	for parked := false; !parked; n++ {
		if n > 200 {
			t.Fatal("the wait never took an event")
		}
		rename(n)
		select {
		case <-taken:
			parked = true
		case <-time.After(20 * time.Millisecond):
		}
	}
	// Overflow the held consumer's 64-slot buffer, then go idle: that
	// transition is among the events the dropped listener never sees.
	for i := 0; i < 100; i++ {
		rename(n + i)
	}
	f.report(t, wire.AgentEventIdle)
	f.stateIs(t, "")
	close(release)

	if r := c.get(t, 5*time.Second); r.err != nil || r.resp.State != "idle" {
		t.Fatalf("got %+v, want idle after the resync", r)
	}
}

func TestWaitSessionRegistryCloseMidWait(t *testing.T) {
	f := newOrchFixture(t, false)
	f.report(t, wire.AgentEventPrompt)
	f.stateIs(t, wire.StateWorking)
	// Only "error" ends it, which nothing in this test reports, so the
	// wait stays parked until the registry closes.
	c := f.startWait(t, wire.WaitSessionReq{SessionID: f.target.ID, States: []string{wire.StateError}})
	c.quiet(t, 100*time.Millisecond)
	_ = f.d.reg.Close()
	r := c.get(t, 5*time.Second)
	if r.err == nil || !strings.Contains(r.err.Message, "shutting down") {
		t.Fatalf("got %+v, want a shutting-down error", r)
	}
}

func TestWaitSessionDisconnectLogged(t *testing.T) {
	f := newOrchFixture(t, false)
	var buf syncBuf
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })

	done := make(chan struct{})
	ops := controlOps{
		restricted: true, ownSessionID: f.sender.ID, ownProjectID: f.sender.ProjectID,
		writeJSON: func(wire.FrameType, any) error { defer close(done); return fmt.Errorf("broken pipe") },
		sendError: func(code, msg string) { t.Errorf("unexpected error %s: %s", code, msg) },
	}
	go f.d.handleControlFrame(context.Background(), ops, wire.FrameWaitSession,
		mustJSON(t, wire.WaitSessionReq{SessionID: f.target.ID}))
	<-done
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(buf.String(), "reached=disconnect") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), "reached=disconnect") {
		t.Errorf("no disconnect line in:\n%s", buf.String())
	}
}

// A wait must not be billed as an expensive frame on a plugin-tagged
// connection: cost-1 frames take no pressure slot (plugins.go).
func TestWaitSessionIsACheapFrame(t *testing.T) {
	if c := plugin.FrameCost(wire.FrameWaitSession); c > 1 {
		t.Errorf("FrameCost(WAIT_SESSION) = %v, want a cheap frame", c)
	}
}

func TestWaitedRoundTrips(t *testing.T) {
	b, _ := json.Marshal(wire.WaitSessionReq{SessionID: "x", States: []string{"idle"}, TimeoutSeconds: 3, AfterWorking: true})
	for _, k := range []string{`"session_id"`, `"states"`, `"timeout_s"`, `"after_working"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("missing snake_case key %s in %s", k, b)
		}
	}
}

// The headline risk of an inline wait: a ModeSession connection is hung
// up on after sessionModeIdleDeadline of silence, and a wait is silent by
// design. Over a real socket with the deadline shrunk, the wait must
// outlive it, then answer.
func TestWaitSessionSurvivesPastTheSessionIdleDeadline(t *testing.T) {
	skipOnWindows(t)
	orig := sessionModeIdleDeadline
	sessionModeIdleDeadline = 150 * time.Millisecond
	t.Cleanup(func() { sessionModeIdleDeadline = orig })

	d := startTestDaemon(t)
	caller := bootstrapSessionID(t, d)
	e, err := d.Registry().Create(context.Background(), wire.CreateSpec{
		Name: "worker", Agent: "claude", Cmd: []string{"/bin/cat"}, Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	reportTool(t, d, wire.AgentEvent{SessionID: e.ID, Kind: wire.AgentEventPrompt})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, _ := d.sessionInfo(e.ID); info.State == wire.StateWorking {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	c := sessionConn(t, d, caller)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameWaitSession, wire.WaitSessionReq{SessionID: "worker"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * sessionModeIdleDeadline)
	reportTool(t, d, wire.AgentEvent{SessionID: e.ID, Kind: wire.AgentEventIdle})

	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		ft, payload, err := wire.ReadFrame(c)
		if err != nil {
			t.Fatalf("the wait connection was hung up on: %v", err)
		}
		if ft != wire.FrameWaited {
			continue
		}
		var resp wire.WaitedResp
		if err := json.Unmarshal(payload, &resp); err != nil {
			t.Fatal(err)
		}
		if resp.State != "idle" || resp.SessionID != e.ID {
			t.Fatalf("got %+v, want idle for %s", resp, e.ID)
		}
		return
	}
}
