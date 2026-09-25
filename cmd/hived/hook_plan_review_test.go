package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

// fakeReviewDaemon listens on a private unix socket and answers every
// plan_review request with reply, recording what it was asked. Event
// connections are drained and counted.
type fakeReviewDaemon struct {
	sock  string
	mu    sync.Mutex
	dials int
	reqs  []wire.PlanReviewRequest
	evs   []wire.AgentEvent
}

func startFakeReviewDaemon(t *testing.T, reply wire.PlanReviewDecision) *fakeReviewDaemon {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets")
	}
	dir, err := os.MkdirTemp("/tmp", "hr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	f := &fakeReviewDaemon{sock: filepath.Join(dir, "s")}
	ln, err := net.Listen("unix", f.sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var h wire.Hello
				if _, err := wire.ReadJSON(c, &h); err != nil {
					return
				}
				f.mu.Lock()
				f.dials++
				f.mu.Unlock()
				switch h.Mode {
				case wire.ModePlanReview:
					var req wire.PlanReviewRequest
					if _, err := wire.ReadJSON(c, &req); err != nil {
						return
					}
					f.mu.Lock()
					f.reqs = append(f.reqs, req)
					f.mu.Unlock()
					_ = wire.WriteJSON(c, wire.FramePlanReviewDecision, reply)
				case wire.ModeEvent:
					for {
						var ev wire.AgentEvent
						if _, err := wire.ReadJSON(c, &ev); err != nil {
							return
						}
						f.mu.Lock()
						f.evs = append(f.evs, ev)
						f.mu.Unlock()
					}
				}
			}(c)
		}
	}()
	return f
}

func (f *fakeReviewDaemon) snapshot() (int, []wire.PlanReviewRequest, []wire.AgentEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dials, append([]wire.PlanReviewRequest(nil), f.reqs...), append([]wire.AgentEvent(nil), f.evs...)
}

type hookDecision struct {
	HookSpecificOutput struct {
		HookEventName string `json:"hookEventName"`
		Decision      struct {
			Behavior     string          `json:"behavior"`
			Message      string          `json:"message"`
			UpdatedInput json.RawMessage `json:"updatedInput"`
		} `json:"decision"`
	} `json:"hookSpecificOutput"`
}

func decodeHookDecision(t *testing.T, out []byte) hookDecision {
	t.Helper()
	var d hookDecision
	if err := json.Unmarshal(out, &d); err != nil {
		t.Fatalf("hook stdout is not JSON: %v\n%s", err, out)
	}
	if d.HookSpecificOutput.HookEventName != "PermissionRequest" {
		t.Errorf("hookEventName = %q", d.HookSpecificOutput.HookEventName)
	}
	return d
}

// Approving must echo tool_input as updatedInput, or Claude Code drops
// the allow and shows its own dialog (verified live, 2.1.282).
func TestPlanReviewOutputAllowEchoesUpdatedInput(t *testing.T) {
	f := startFakeReviewDaemon(t, wire.PlanReviewDecision{Status: wire.PlanReviewApprove})
	t.Setenv(agent.PlanReviewerEnv, agent.PlanReviewerHive)
	raw := readFixture(t, "permission_request_exitplanmode.json")
	out := planReviewOutput(raw, f.sock, "sess-1")
	d := decodeHookDecision(t, out)
	if d.HookSpecificOutput.Decision.Behavior != "allow" {
		t.Fatalf("behavior = %q, want allow", d.HookSpecificOutput.Decision.Behavior)
	}
	var fixture struct {
		ToolInput map[string]any `json:"tool_input"`
		Cwd       string         `json:"cwd"`
	}
	_ = json.Unmarshal(raw, &fixture)
	var echoed map[string]any
	if err := json.Unmarshal(d.HookSpecificOutput.Decision.UpdatedInput, &echoed); err != nil {
		t.Fatalf("updatedInput: %v", err)
	}
	if !reflect.DeepEqual(echoed, fixture.ToolInput) {
		t.Errorf("updatedInput = %v, want tool_input %v", echoed, fixture.ToolInput)
	}

	// The resolve event rides its own connection, which the fake reads
	// asynchronously: wait for it rather than race it.
	_, reqs, evs := f.snapshot()
	for deadline := time.Now().Add(2 * time.Second); len(evs) == 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
		_, reqs, evs = f.snapshot()
	}
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	r := reqs[0]
	if r.SessionID != "sess-1" || r.Source != wire.PlanReviewSourceClaude || r.Cwd != fixture.Cwd ||
		r.Reviewer != agent.PlanReviewerHive || r.Plan != fixture.ToolInput["plan"] {
		t.Errorf("request = %+v", r)
	}
	if len(evs) != 1 || evs[0].Kind != wire.AgentEventPermissionResolved {
		t.Errorf("a decided review must clear waiting_permission; events = %+v", evs)
	}
}

func TestPlanReviewOutputDenyForwardsMessage(t *testing.T) {
	msg := "The user reviewed your plan in Hive and did not approve it yet.\n1. On \"x\":\n   > y\n"
	f := startFakeReviewDaemon(t, wire.PlanReviewDecision{Status: wire.PlanReviewDeny, Message: msg})
	out := planReviewOutput(readFixture(t, "permission_request_exitplanmode.json"), f.sock, "sess-1")
	d := decodeHookDecision(t, out)
	if d.HookSpecificOutput.Decision.Behavior != "deny" || d.HookSpecificOutput.Decision.Message != msg {
		t.Errorf("decision = %+v, want deny with the daemon's message verbatim", d.HookSpecificOutput.Decision)
	}
	if len(d.HookSpecificOutput.Decision.UpdatedInput) != 0 {
		t.Error("a deny must not carry updatedInput")
	}
}

// Everything that is not the user's approve or deny prints nothing, so
// Claude shows its own dialog. Non-plan events must not even dial.
func TestPlanReviewOutputFallsThrough(t *testing.T) {
	exitPlan := readFixture(t, "permission_request_exitplanmode.json")
	withPlan := func(plan string) []byte {
		var p map[string]any
		_ = json.Unmarshal(exitPlan, &p)
		p["tool_input"].(map[string]any)["plan"] = plan
		b, _ := json.Marshal(p)
		return b
	}
	for _, status := range []string{wire.PlanReviewDisabled, wire.PlanReviewExternal, wire.PlanReviewNoClient, wire.PlanReviewCancelled, wire.PlanReviewInvalid} {
		t.Run(status, func(t *testing.T) {
			f := startFakeReviewDaemon(t, wire.PlanReviewDecision{Status: status})
			if out := planReviewOutput(exitPlan, f.sock, "s"); out != nil {
				t.Errorf("status %s printed %s", status, out)
			}
			if _, _, evs := f.snapshot(); len(evs) != 0 {
				t.Errorf("status %s sent %+v; only a decision resolves the wait", status, evs)
			}
		})
	}
	for name, raw := range map[string][]byte{
		"other permission": readFixture(t, "permission_request.json"),
		"question tool":    readFixture(t, "permission_request_question.json"),
		"pre tool use":     readFixture(t, "pre_tool_use.json"),
		"empty plan":       withPlan(""),
		"oversize plan":    withPlan(strings.Repeat("x", wire.MaxPlanReviewLen+1)),
		"malformed":        []byte("{ nope"),
	} {
		t.Run(name, func(t *testing.T) {
			f := startFakeReviewDaemon(t, wire.PlanReviewDecision{Status: wire.PlanReviewApprove})
			if out := planReviewOutput(raw, f.sock, "s"); out != nil {
				t.Errorf("printed %s", out)
			}
			if dials, _, _ := f.snapshot(); dials != 0 {
				t.Errorf("dialed the daemon %d times; only a reviewable ExitPlanMode may", dials)
			}
		})
	}
	t.Run("daemon down", func(t *testing.T) {
		if out := planReviewOutput(exitPlan, filepath.Join(t.TempDir(), "gone"), "s"); out != nil {
			t.Errorf("printed %s", out)
		}
	})
}

// The full round trip against a real daemon: the hook blocks, the
// session shows a pending review, a control client answers through
// GET/RESOLVE, and the hook prints Claude's decision.
func TestHookPlanReviewRoundTrip(t *testing.T) {
	settings := t.TempDir()
	if err := os.WriteFile(filepath.Join(settings, agent.SettingsFileName), []byte(`{"plan_review": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	agent.SetCustomDir(settings)
	t.Cleanup(func() { agent.SetCustomDir("") })
	t.Setenv("HOME", t.TempDir()) // no external reviewer
	d := startHookTestDaemon(t)
	id := d.Registry().List()[0].ID
	gui, _ := dialControlAndSubscribe(t, d)
	defer gui.Close()

	raw := readFixture(t, "permission_request_exitplanmode.json")
	run := func() <-chan []byte {
		ch := make(chan []byte, 1)
		go func() { ch <- planReviewOutput(raw, d.EventSocketPath(), id) }()
		return ch
	}
	pending := func() *wire.PendingPlanReview {
		var p *wire.PendingPlanReview
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if s, ok := findSessionByID(d, id); ok && s.PendingPlanReview != nil {
				p = s.PendingPlanReview
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if p == nil {
			t.Fatal("review never became pending")
		}
		return p
	}
	await := func(ch <-chan []byte) []byte {
		select {
		case out := <-ch:
			return out
		case <-time.After(3 * time.Second):
			t.Fatal("hook never returned")
			return nil
		}
	}

	// Deny.
	ch := run()
	p := pending()
	if err := wire.WriteJSON(gui, wire.FrameResolvePlanReview, wire.ResolvePlanReviewReq{
		SessionID: id, ReviewID: p.ReviewID, Decision: wire.PlanReviewDeny,
		Comments: []wire.PlanComment{{Quote: "Create hello.txt", Text: "name it greeting.txt"}},
	}); err != nil {
		t.Fatal(err)
	}
	dec := decodeHookDecision(t, await(ch))
	if dec.HookSpecificOutput.Decision.Behavior != "deny" ||
		!strings.Contains(dec.HookSpecificOutput.Decision.Message, "name it greeting.txt") ||
		!strings.Contains(dec.HookSpecificOutput.Decision.Message, `On "Create hello.txt"`) {
		t.Errorf("deny = %+v", dec.HookSpecificOutput.Decision)
	}

	// Approve.
	ch = run()
	p = pending()
	if err := wire.WriteJSON(gui, wire.FrameResolvePlanReview, wire.ResolvePlanReviewReq{
		SessionID: id, ReviewID: p.ReviewID, Decision: wire.PlanReviewApprove,
	}); err != nil {
		t.Fatal(err)
	}
	if dec := decodeHookDecision(t, await(ch)); dec.HookSpecificOutput.Decision.Behavior != "allow" {
		t.Errorf("approve = %+v", dec.HookSpecificOutput.Decision)
	}
	if s, _ := findSessionByID(d, id); s.PendingPlanReview != nil {
		t.Error("review still pending after the answer")
	}

	// The GUI quits mid-review: the hook prints nothing, so Claude's own
	// dialog takes over.
	ch = run()
	pending()
	_ = gui.Close()
	if out := await(ch); out != nil {
		t.Errorf("GUI left mid-review but the hook printed %s", out)
	}
}
