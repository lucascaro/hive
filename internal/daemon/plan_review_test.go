package daemon

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

// #457: the daemon half of plan review — the gates, the park, and every
// way a held request ends.

// planReviewSettings points agent settings at a temp dir holding body,
// and external-reviewer detection at an empty fake home.
func planReviewSettings(t *testing.T, body string) (home string) {
	t.Helper()
	dir := t.TempDir()
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, agent.SettingsFileName), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	agent.SetCustomDir(dir)
	t.Cleanup(func() { agent.SetCustomDir("") })
	home = t.TempDir()
	prevHome, prevManaged := planReviewHome, planReviewManaged
	planReviewHome = func() (string, error) { return home, nil }
	planReviewManaged = func() string { return "" }
	t.Cleanup(func() { planReviewHome, planReviewManaged = prevHome, prevManaged })
	return home
}

const reviewOn = `{"plan_review": true}`

// requestPlanReview dials the events socket and sends one request.
func requestPlanReview(t *testing.T, d *Daemon, req wire.PlanReviewRequest) net.Conn {
	t.Helper()
	c := dialEvents(t, d)
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "hived-hook/0", Mode: wire.ModePlanReview,
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if err := wire.WriteJSON(c, wire.FramePlanReviewRequest, req); err != nil {
		t.Fatalf("write request: %v", err)
	}
	return c
}

func readDecision(t *testing.T, c net.Conn, within time.Duration) wire.PlanReviewDecision {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(within))
	var dec wire.PlanReviewDecision
	ft, err := wire.ReadJSON(c, &dec)
	if err != nil || ft != wire.FramePlanReviewDecision {
		t.Fatalf("read decision: %s %v", ft, err)
	}
	return dec
}

// answerer opens a control connection that counts as a GUI.
func answerer(t *testing.T, d *Daemon, client string) net.Conn {
	t.Helper()
	c := dial(t, d)
	handshake(t, c, wire.Hello{Mode: wire.ModeControl, Client: client})
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := c.Read(buf); err != nil {
				return
			}
		}
	}()
	return c
}

func pendingReview(d *Daemon, id string) *wire.PendingPlanReview {
	for _, s := range d.Registry().List() {
		if s.ID == id {
			return s.PendingPlanReview
		}
	}
	return nil
}

func waitPending(t *testing.T, d *Daemon, id string) *wire.PendingPlanReview {
	t.Helper()
	var p *wire.PendingPlanReview
	waitFor(t, 2*time.Second, func() bool { p = pendingReview(d, id); return p != nil })
	return p
}

func answererCount(d *Daemon) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.controlClients
}

func claudeReq(id string) wire.PlanReviewRequest {
	return wire.PlanReviewRequest{SessionID: id, Source: wire.PlanReviewSourceClaude, Plan: "# Plan\n\n- step one\n"}
}

func TestPlanReviewDisabledByDefault(t *testing.T) {
	skipOnWindows(t)
	planReviewSettings(t, "")
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	answerer(t, d, "hivegui/0.2")
	c := requestPlanReview(t, d, claudeReq(id))
	defer c.Close()
	if dec := readDecision(t, c, 2*time.Second); dec.Status != wire.PlanReviewDisabled {
		t.Errorf("status = %q, want disabled", dec.Status)
	}
	if pendingReview(d, id) != nil {
		t.Error("a disabled review parked anyway")
	}
}

func TestPlanReviewFailsFastWithoutClient(t *testing.T) {
	skipOnWindows(t)
	planReviewSettings(t, reviewOn)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	c := requestPlanReview(t, d, claudeReq(id))
	defer c.Close()
	start := time.Now()
	if dec := readDecision(t, c, 2*time.Second); dec.Status != wire.PlanReviewNoClient {
		t.Errorf("status = %q, want no_client", dec.Status)
	}
	if time.Since(start) > time.Second {
		t.Errorf("no_client took %v; it must fail fast", time.Since(start))
	}
}

func TestPlanReviewHivebarIsNotAnAnswerer(t *testing.T) {
	skipOnWindows(t)
	planReviewSettings(t, reviewOn)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	bar := answerer(t, d, "hivebar/0.1")
	defer bar.Close()
	waitFor(t, 2*time.Second, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return len(d.clients) > 0
	})
	if n := answererCount(d); n != 0 {
		t.Fatalf("hivebar counted as an answerer: %d", n)
	}
	c := requestPlanReview(t, d, claudeReq(id))
	defer c.Close()
	if dec := readDecision(t, c, 2*time.Second); dec.Status != wire.PlanReviewNoClient {
		t.Errorf("with only hivebar attached: status %q, want no_client", dec.Status)
	}
}

func TestPlanReviewRoundTripApproveDeny(t *testing.T) {
	skipOnWindows(t)
	planReviewSettings(t, reviewOn)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	gui := answerer(t, d, "hivegui/0.2")
	defer gui.Close()
	waitFor(t, 2*time.Second, func() bool { return answererCount(d) == 1 })

	// Deny with a comment: the requester gets the formatted message.
	c := requestPlanReview(t, d, claudeReq(id))
	defer c.Close()
	p := waitPending(t, d, id)
	if src, plan, ok := d.Registry().PlanReviewText(id, p.ReviewID); !ok || src != wire.PlanReviewSourceClaude || !strings.Contains(plan, "step one") {
		t.Fatalf("PlanReviewText = %q %q %v", src, plan, ok)
	}
	if err := wire.WriteJSON(gui, wire.FrameResolvePlanReview, wire.ResolvePlanReviewReq{
		SessionID: id, ReviewID: p.ReviewID, Decision: wire.PlanReviewDeny,
		Comments: []wire.PlanComment{{Quote: "step one", Text: "split it in two"}}, Feedback: "and add tests",
	}); err != nil {
		t.Fatal(err)
	}
	dec := readDecision(t, c, 2*time.Second)
	if dec.Status != wire.PlanReviewDeny {
		t.Fatalf("status = %q, want deny", dec.Status)
	}
	for _, want := range []string{`On "step one"`, "split it in two", "and add tests", "The user reviewed your plan"} {
		if !strings.Contains(dec.Message, want) {
			t.Errorf("deny message lacks %q:\n%s", want, dec.Message)
		}
	}
	waitFor(t, 2*time.Second, func() bool { return pendingReview(d, id) == nil })

	// Approve.
	c2 := requestPlanReview(t, d, claudeReq(id))
	defer c2.Close()
	p2 := waitPending(t, d, id)
	if err := wire.WriteJSON(gui, wire.FrameResolvePlanReview, wire.ResolvePlanReviewReq{
		SessionID: id, ReviewID: p2.ReviewID, Decision: wire.PlanReviewApprove,
	}); err != nil {
		t.Fatal(err)
	}
	if dec := readDecision(t, c2, 2*time.Second); dec.Status != wire.PlanReviewApprove || dec.Message != "" {
		t.Errorf("approve decision = %+v", dec)
	}
}

func TestPlanReviewCancelsOnRequesterDisconnect(t *testing.T) {
	skipOnWindows(t)
	planReviewSettings(t, reviewOn)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	gui := answerer(t, d, "hivegui/0.2")
	defer gui.Close()
	waitFor(t, 2*time.Second, func() bool { return answererCount(d) == 1 })

	c := requestPlanReview(t, d, claudeReq(id))
	waitPending(t, d, id)
	_ = c.Close() // Claude killed the hook: the user answered in the terminal
	waitFor(t, 2*time.Second, func() bool { return pendingReview(d, id) == nil })
}

func TestPlanReviewCancelsWhenLastAnswererLeaves(t *testing.T) {
	skipOnWindows(t)
	planReviewSettings(t, reviewOn)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	gui := answerer(t, d, "hivegui/0.2")
	waitFor(t, 2*time.Second, func() bool { return answererCount(d) == 1 })

	c := requestPlanReview(t, d, claudeReq(id))
	defer c.Close()
	waitPending(t, d, id)
	_ = gui.Close()
	if dec := readDecision(t, c, 3*time.Second); dec.Status != wire.PlanReviewNoClient {
		t.Errorf("GUI left mid-review: status %q, want no_client", dec.Status)
	}
	if pendingReview(d, id) != nil {
		t.Error("review still pending after the last answerer left")
	}
}

func TestPlanReviewCancelledOnDaemonStop(t *testing.T) {
	skipOnWindows(t)
	planReviewSettings(t, reviewOn)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	answerer(t, d, "hivegui/0.2")
	waitFor(t, 2*time.Second, func() bool { return answererCount(d) == 1 })

	c := requestPlanReview(t, d, claudeReq(id))
	defer c.Close()
	waitPending(t, d, id)
	_ = d.Close()
	// Either the cancelled decision or a closed connection is fine; what
	// must not happen is the requester hanging.
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var dec wire.PlanReviewDecision
	if ft, err := wire.ReadJSON(c, &dec); err == nil && (ft != wire.FramePlanReviewDecision || dec.Status != wire.PlanReviewCancelled) {
		t.Errorf("on daemon stop got %s %+v, want cancelled or EOF", ft, dec)
	} else if err != nil && isTimeout(err) {
		t.Error("requester still waiting after the daemon stopped")
	}
}

func TestPlanReviewCancelledOnTimeout(t *testing.T) {
	skipOnWindows(t)
	planReviewSettings(t, reviewOn)
	prev := planReviewMaxWait
	planReviewMaxWait = 100 * time.Millisecond
	t.Cleanup(func() { planReviewMaxWait = prev })
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	answerer(t, d, "hivegui/0.2")
	waitFor(t, 2*time.Second, func() bool { return answererCount(d) == 1 })

	c := requestPlanReview(t, d, claudeReq(id))
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var dec wire.PlanReviewDecision
	ft, err := wire.ReadJSON(c, &dec)
	if err != nil {
		t.Fatalf("read decision: %v", err)
	}
	if ft != wire.FramePlanReviewDecision || dec.Status != wire.PlanReviewCancelled {
		t.Fatalf("on timeout got %s %+v, want cancelled", ft, dec)
	}
	waitFor(t, 2*time.Second, func() bool { return pendingReview(d, id) == nil })
}

func isTimeout(err error) bool {
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}

func writeReviewerHook(t *testing.T, home string) {
	t.Helper()
	p := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"hooks":{"PermissionRequest":[{"matcher":"ExitPlanMode","hooks":[]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPlanReviewExternalReviewerWins(t *testing.T) {
	skipOnWindows(t)
	home := planReviewSettings(t, reviewOn)
	writeReviewerHook(t, home)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	answerer(t, d, "hivegui/0.2")
	waitFor(t, 2*time.Second, func() bool { return answererCount(d) == 1 })

	c := requestPlanReview(t, d, claudeReq(id))
	defer c.Close()
	if dec := readDecision(t, c, 2*time.Second); dec.Status != wire.PlanReviewExternal {
		t.Errorf("status = %q, want external", dec.Status)
	}
	// Pi has no external reviewer concept: it is reviewed regardless.
	pi := requestPlanReview(t, d, wire.PlanReviewRequest{SessionID: id, Source: wire.PlanReviewSourcePi, Plan: "# p"})
	defer pi.Close()
	waitPending(t, d, id)
}

// The reviewer is the one the session was spawned with: a session whose
// plugin reviewer Hive disabled is reviewed by Hive; a session spawned
// under "external" keeps deferring even if the setting now says hive,
// because its plugin is still live and would prompt too.
func TestPlanReviewReviewerIsSpawnTime(t *testing.T) {
	skipOnWindows(t)
	home := planReviewSettings(t, `{"plan_review": true, "plan_reviewer": "hive"}`)
	writeReviewerHook(t, home)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	answerer(t, d, "hivegui/0.2")
	waitFor(t, 2*time.Second, func() bool { return answererCount(d) == 1 })

	spawnedExternal := claudeReq(id)
	spawnedExternal.Reviewer = agent.PlanReviewerExternal
	c := requestPlanReview(t, d, spawnedExternal)
	defer c.Close()
	if dec := readDecision(t, c, 2*time.Second); dec.Status != wire.PlanReviewExternal {
		t.Errorf("session spawned under external: status %q, want external", dec.Status)
	}

	spawnedHive := claudeReq(id)
	spawnedHive.Reviewer = agent.PlanReviewerHive
	c2 := requestPlanReview(t, d, spawnedHive)
	defer c2.Close()
	waitPending(t, d, id)
}

func TestPlanReviewOversizeRefused(t *testing.T) {
	skipOnWindows(t)
	planReviewSettings(t, reviewOn)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	answerer(t, d, "hivegui/0.2")
	req := claudeReq(id)
	req.Plan = strings.Repeat("x", wire.MaxPlanReviewLen+1)
	c := requestPlanReview(t, d, req)
	defer c.Close()
	if dec := readDecision(t, c, 2*time.Second); dec.Status != wire.PlanReviewInvalid {
		t.Errorf("status = %q, want invalid", dec.Status)
	}
}

// The mode is an events-socket verb. The control socket must not grow
// a second way in.
func TestPlanReviewModeRefusedOnControlSocket(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := dial(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "x/0", Mode: wire.ModePlanReview,
	}); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var e wire.Error
	ft, err := wire.ReadJSON(c, &e)
	if err != nil || ft != wire.FrameError || e.Code != "unknown_mode" {
		t.Errorf("got %s %+v %v, want unknown_mode", ft, e, err)
	}
}

func TestGetPlanReviewStale(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	gui := dial(t, d)
	defer gui.Close()
	handshake(t, gui, wire.Hello{Mode: wire.ModeControl, Client: "hivegui/0.2"})
	if err := wire.WriteJSON(gui, wire.FrameGetPlanReview, wire.GetPlanReviewReq{SessionID: id, ReviewID: "gone"}); err != nil {
		t.Fatal(err)
	}
	var e wire.Error
	if err := jsonUnmarshal(readControlFrame(t, gui, wire.FrameError, 2*time.Second), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != wire.ErrCodePlanReviewStale || e.SessionID != id {
		t.Errorf("error = %+v, want %s for %s", e, wire.ErrCodePlanReviewStale, id)
	}
}
