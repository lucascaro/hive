//go:build !e2e

package main

import (
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/wire"
)

// Live probes for plan review (#457). Opt-in, like every probe here:
// each costs real API calls.
//
//	HIVE_PROBE_CLAUDE=1 go test ./cmd/hived -run TestClaudeProbePlanReview -v
//	HIVE_PROBE_PI=1     go test ./cmd/hived -run TestPiProbePlanReview -v

// probeReviewer stands in for the GUI: a real control connection (so
// the daemon counts an answerer) that answers each pending review of
// session id with decide(plan, n) and records every plan it saw.
type probeReviewer struct {
	mu    sync.Mutex
	plans []string
}

func (r *probeReviewer) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.plans...)
}

func startProbeReviewer(t *testing.T, d *daemon.Daemon, id string, decide func(plan string, n int) wire.ResolvePlanReviewReq) *probeReviewer {
	t.Helper()
	conn, err := net.Dial("unix", d.SocketPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "probe-reviewer/0", Mode: wire.ModeControl,
	}); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			if _, _, err := wire.ReadFrame(conn); err != nil {
				return
			}
		}
	}()
	r := &probeReviewer{}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		answered := map[string]bool{}
		for {
			select {
			case <-stop:
				return
			case <-time.After(200 * time.Millisecond):
			}
			info, ok := findSessionByID(d, id)
			if !ok || info.PendingPlanReview == nil || answered[info.PendingPlanReview.ReviewID] {
				continue
			}
			rid := info.PendingPlanReview.ReviewID
			_, plan, ok := d.Registry().PlanReviewText(id, rid)
			if !ok {
				continue
			}
			answered[rid] = true
			r.mu.Lock()
			r.plans = append(r.plans, plan)
			n := len(r.plans)
			r.mu.Unlock()
			req := decide(plan, n)
			req.SessionID, req.ReviewID = id, rid
			_ = wire.WriteJSON(conn, wire.FrameResolvePlanReview, req)
		}
	}()
	return r
}

// denyThenApprove denies the first plan with a comment carrying a
// sentinel the revised plan must mention, and approves the rest.
func denyThenApprove(plan string, n int) wire.ResolvePlanReviewReq {
	if n == 1 {
		quote := strings.SplitN(strings.TrimSpace(plan), "\n", 2)[0]
		return wire.ResolvePlanReviewReq{Decision: wire.PlanReviewDeny, Comments: []wire.PlanComment{{
			Quote: quote, Text: "Name the file sentinel42.txt instead, and say sentinel42.txt in the plan.",
		}}}
	}
	return wire.ResolvePlanReviewReq{Decision: wire.PlanReviewApprove}
}

func TestClaudeProbePlanReview(t *testing.T) {
	if os.Getenv("HIVE_PROBE_CLAUDE") != "1" {
		t.Skip("set HIVE_PROBE_CLAUDE=1 to run the real-claude probe")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not on PATH")
	}
	sess, wait, d, sink := startClaudeProbeDaemon(t)
	// Read live by the daemon, so writing it after spawn is enough.
	if err := agent.SaveSettings(agent.Settings{ClaudeTaskTools: false, PiTodoTool: true, PlanReview: true}); err != nil {
		t.Fatal(err)
	}
	id := d.Registry().List()[len(d.Registry().List())-1].ID
	r := startProbeReviewer(t, d, id, denyThenApprove)

	// Shift+Tab cycles the permission mode; press until the footer says
	// plan mode. The number of presses differs between Claude builds.
	for i := 0; i < 6 && !sink.contains("plan mode on"); i++ {
		_, _ = sess.Write([]byte("\x1b[Z"))
		time.Sleep(800 * time.Millisecond)
	}
	if !sink.contains("plan mode on") {
		wait(time.Millisecond, func(wire.SessionInfo) bool { return false }, "plan mode")
	}
	_, _ = sess.Write([]byte("Plan adding a file hello.txt containing hi. Keep the plan to two bullets, then exit plan mode."))
	time.Sleep(700 * time.Millisecond)
	_, _ = sess.Write([]byte("\r"))

	// Through wait, so a timeout prints what the terminal showed.
	wait(240*time.Second, func(wire.SessionInfo) bool { return len(r.seen()) >= 2 }, "two plan reviews (deny, then the revision)")
	plans := r.seen()
	if !strings.Contains(strings.ToLower(plans[1]), "sentinel42") {
		// wait's timeout path prints the terminal tail.
		wait(time.Millisecond, func(wire.SessionInfo) bool { return false },
			"a revised plan that addresses the comment; got:\n"+plans[1])
	}
	wait(60*time.Second, func(i wire.SessionInfo) bool { return i.PendingPlanReview == nil }, "the approved review to clear")
	_, _ = sess.Write([]byte("\x1b"))
}

func TestPiProbePlanReview(t *testing.T) {
	if os.Getenv("HIVE_PROBE_PI") != "1" {
		t.Skip("set HIVE_PROBE_PI=1 to run the real-pi probe")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skip("pi not on PATH")
	}
	agent.SetCustomDir(t.TempDir())
	t.Cleanup(func() { agent.SetCustomDir("") })
	if err := agent.SaveSettings(agent.Settings{ClaudeTaskTools: true, PiTodoTool: false, PlanReview: true}); err != nil {
		t.Fatal(err)
	}
	d, id, sess, wait := startPiProbe(t)
	r := startProbeReviewer(t, d, id, denyThenApprove)

	// No mention of the tool: the extension's guidelines are what must
	// make the model submit a plan (a named failure signal is Pi
	// ignoring the tool).
	if _, err := sess.Write([]byte("Add a file hello.txt containing hi, then a file bye.txt containing bye.\r")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(240 * time.Second)
	for len(r.seen()) < 2 && time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
	}
	if plans := r.seen(); len(plans) < 2 {
		// Say what Pi did instead: which tools it ran, and its last words.
		act, _ := d.Registry().ActivitySnapshot(id)
		var tools []string
		for _, ev := range act.Events {
			tools = append(tools, ev.Tool)
		}
		info, _ := findSessionByID(d, id)
		t.Fatalf("saw %d plan reviews, want 2; tools run: %v; last summary: %q", len(plans), tools, info.LastSummary)
	}
	plans := r.seen()
	if !strings.Contains(strings.ToLower(plans[1]), "sentinel42") {
		t.Errorf("the revised plan ignored the reviewer's comment:\n%s", plans[1])
	}
	wait(180*time.Second, func(i wire.SessionInfo) bool {
		return i.State == wire.StateWaitingInput && i.PendingPlanReview == nil
	}, "the turn to finish after approval")
}

// TestPiProbePlanReviewOff is the negative control: with review off the
// tool does not exist, so no review is ever parked.
func TestPiProbePlanReviewOff(t *testing.T) {
	if os.Getenv("HIVE_PROBE_PI") != "1" {
		t.Skip("set HIVE_PROBE_PI=1 to run the real-pi probe")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skip("pi not on PATH")
	}
	agent.SetCustomDir(t.TempDir())
	t.Cleanup(func() { agent.SetCustomDir("") })
	d, id, sess, wait := startPiProbe(t)
	r := startProbeReviewer(t, d, id, func(string, int) wire.ResolvePlanReviewReq {
		return wire.ResolvePlanReviewReq{Decision: wire.PlanReviewApprove}
	})
	if _, err := sess.Write([]byte("If you have a tool named hive_submit_plan, call it once with the plan: one step, say hi. " +
		"If you do not have it, run no tool. Then reply done.\r")); err != nil {
		t.Fatal(err)
	}
	wait(30*time.Second, func(i wire.SessionInfo) bool { return i.State == wire.StateWorking }, "the turn to start")
	wait(120*time.Second, func(i wire.SessionInfo) bool {
		return i.State == wire.StateWaitingInput && i.StateSource == wire.StateSourceExtension
	}, "the turn to finish")
	if p := r.seen(); len(p) != 0 {
		t.Errorf("review off, but %d plans were submitted", len(p))
	}
}
