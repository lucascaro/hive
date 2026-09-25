package registry

import (
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// decisionOf waits briefly for ch's one decision.
func decisionOf(t *testing.T, ch <-chan wire.PlanReviewDecision) wire.PlanReviewDecision {
	t.Helper()
	select {
	case d := <-ch:
		return d
	case <-time.After(2 * time.Second):
		t.Fatal("no plan review decision")
		return wire.PlanReviewDecision{}
	}
}

func pendingReviewOf(r *Registry, id string) *wire.PendingPlanReview {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[id]; ok {
		return e.Info().PendingPlanReview
	}
	return nil
}

func TestParkPlanReviewReplacesPrior(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)

	id1, ch1, err := r.ParkPlanReview(e.ID, wire.PlanReviewSourceClaude, "# one")
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	id2, _, err := r.ParkPlanReview(e.ID, wire.PlanReviewSourceClaude, "# two")
	if err != nil {
		t.Fatalf("second park: %v", err)
	}
	if d := decisionOf(t, ch1); d.Status != wire.PlanReviewCancelled {
		t.Errorf("replaced review decided %q, want cancelled", d.Status)
	}
	if p := pendingReviewOf(r, e.ID); p == nil || p.ReviewID != id2 || id1 == id2 {
		t.Errorf("pending = %+v, want the second review %s", p, id2)
	}
	if _, plan, ok := r.PlanReviewText(e.ID, id2); !ok || plan != "# two" {
		t.Errorf("PlanReviewText = %q %v", plan, ok)
	}
	if _, _, ok := r.PlanReviewText(e.ID, id1); ok {
		t.Error("the replaced review's text must not be served")
	}
}

func TestResolvePlanReviewStaleIDIgnored(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	id, ch, err := r.ParkPlanReview(e.ID, wire.PlanReviewSourcePi, "# plan")
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	if r.ResolvePlanReview(wire.ResolvePlanReviewReq{SessionID: e.ID, ReviewID: "other", Decision: wire.PlanReviewApprove}) {
		t.Fatal("a mismatched review id was applied")
	}
	if pendingReviewOf(r, e.ID) == nil {
		t.Fatal("a stale answer cleared the review")
	}
	deny := wire.ResolvePlanReviewReq{SessionID: e.ID, ReviewID: id, Decision: wire.PlanReviewDeny,
		Comments: []wire.PlanComment{{Quote: "step 1", Text: "no"}}, Feedback: "redo"}
	if !r.ResolvePlanReview(deny) {
		t.Fatal("matching answer not applied")
	}
	d := decisionOf(t, ch)
	if d.Status != wire.PlanReviewDeny || len(d.Comments) != 1 || d.Feedback != "redo" {
		t.Errorf("decision = %+v", d)
	}
	if r.ResolvePlanReview(deny) {
		t.Error("a second answer to a decided review was applied")
	}
	if pendingReviewOf(r, e.ID) != nil {
		t.Error("review still pending after the answer")
	}
}

func TestKillCancelsPlanReview(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	_, ch, err := r.ParkPlanReview(e.ID, wire.PlanReviewSourceClaude, "# plan")
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	if err := r.Kill(e.ID, true); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if d := decisionOf(t, ch); d.Status != wire.PlanReviewCancelled {
		t.Errorf("decision = %q, want cancelled", d.Status)
	}
}

// Restart clears e.sess itself, so watchSessionExit no-ops for the old
// PTY; the review must still be cancelled with the agent it belonged to.
func TestRestartCancelsPlanReview(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	_, ch, err := r.ParkPlanReview(e.ID, wire.PlanReviewSourceClaude, "# plan")
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	if err := r.Restart(e.ID); err != nil {
		t.Fatalf("restart: %v", err)
	}
	t.Cleanup(func() { _ = r.Kill(e.ID, true) })
	if d := decisionOf(t, ch); d.Status != wire.PlanReviewCancelled {
		t.Errorf("decision = %q, want cancelled", d.Status)
	}
	if pendingReviewOf(r, e.ID) != nil {
		t.Error("a restarted session still shows the old agent's review")
	}
}

func TestSessionExitCancelsPlanReview(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	_, ch, err := r.ParkPlanReview(e.ID, wire.PlanReviewSourceClaude, "# plan")
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	_ = e.Session().Close()
	if d := decisionOf(t, ch); d.Status != wire.PlanReviewCancelled {
		t.Errorf("decision = %q, want cancelled", d.Status)
	}
	if pendingReviewOf(r, e.ID) != nil {
		t.Error("an exited session still shows a pending review")
	}
	if _, _, err := r.ParkPlanReview(e.ID, wire.PlanReviewSourceClaude, "# plan"); err != ErrSessionNotAlive {
		t.Errorf("park on a dead session: %v, want ErrSessionNotAlive", err)
	}
}

// The answerer check and the park share r.mu with SetAnswerers, so a
// park can neither land while nobody can answer nor outlive the last
// answerer leaving. The second half is the race #457's second opinion
// named: a park landing after the withdrawal would wait days.
func TestParkPlanReviewAtomicWithAnswerers(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)

	r.SetAnswerers(0)
	if _, _, err := r.ParkPlanReview(e.ID, wire.PlanReviewSourceClaude, "# plan"); err != ErrNoAnswerer {
		t.Fatalf("park with no answerer: %v, want ErrNoAnswerer", err)
	}
	r.SetAnswerers(1)
	_, ch, err := r.ParkPlanReview(e.ID, wire.PlanReviewSourceClaude, "# plan")
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	r.SetAnswerers(0)
	if d := decisionOf(t, ch); d.Status != wire.PlanReviewNoClient {
		t.Errorf("last answerer left: decision %q, want no_client", d.Status)
	}
	if pendingReviewOf(r, e.ID) != nil {
		t.Error("review still pending with no answerer")
	}
}

func TestWithdrawPlanReviewOnlyMatching(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	id, ch, err := r.ParkPlanReview(e.ID, wire.PlanReviewSourceClaude, "# plan")
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	r.WithdrawPlanReview(e.ID, "not-it", wire.PlanReviewCancelled)
	if pendingReviewOf(r, e.ID) == nil {
		t.Fatal("withdraw of another id cleared the review")
	}
	r.WithdrawPlanReview(e.ID, id, wire.PlanReviewCancelled)
	if d := decisionOf(t, ch); d.Status != wire.PlanReviewCancelled {
		t.Errorf("decision = %q", d.Status)
	}
}

// SessionInfo rides every broadcast; a 128 KiB plan must not.
func TestPlanReviewInfoIsSmall(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	plan := "# plan\n" + strings.Repeat("x", 64<<10)
	if _, _, err := r.ParkPlanReview(e.ID, wire.PlanReviewSourceClaude, plan); err != nil {
		t.Fatalf("park: %v", err)
	}
	r.mu.Lock()
	info := r.entries[e.ID].Info()
	r.mu.Unlock()
	if info.PendingPlanReview == nil {
		t.Fatal("pending review missing from Info")
	}
	if got := len(info.PendingPlanReview.ReviewID) + len(info.PendingPlanReview.Source) + len(info.PendingPlanReview.CreatedAt); got > 200 {
		t.Errorf("pending review is %d bytes on SessionInfo", got)
	}
}
