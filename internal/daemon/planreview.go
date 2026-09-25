package daemon

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
)

// canAnswer reports whether a control client can show the user a
// dialog. hivebar is a menu-bar status item with no UI for a worktree
// choice or a plan review, so counting it would park questions nobody
// can answer. Every other control client — the GUI, the ws-bridge that
// fronts a browser GUI, the test client — can.
func canAnswer(h wire.Hello) bool {
	return !strings.HasPrefix(h.Client, "hivebar/")
}

// addAnswerer adjusts the answering-client count and mirrors it into
// the registry. The registry call happens under d.mu so two concurrent
// changes cannot land out of order; the registry never calls back into
// the daemon, so the lock order is fixed.
func (d *Daemon) addAnswerer(delta int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.controlClients += delta
	d.reg.SetAnswerers(d.controlClients)
}

// planReviewMaxWait is the daemon's own ceiling on one review, matching
// the timeout Hive gives Claude's hook. Var so tests can shrink it.
var planReviewMaxWait = time.Duration(agent.PlanReviewHookTimeout) * time.Second

// Seams for tests: where external-reviewer detection looks.
var (
	planReviewHome    = os.UserHomeDir
	planReviewManaged = agent.ManagedSettingsPath
)

// servePlanReview handles a ModePlanReview connection: one
// PLAN_REVIEW_REQUEST, then — possibly days later — one
// PLAN_REVIEW_DECISION. The connection is not in d.clients (Close does
// not hang it up); instead the wait below watches ctx and d.stop.
func (d *Daemon) servePlanReview(ctx context.Context, conn net.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(eventReadDeadline))
	var req wire.PlanReviewRequest
	ft, err := wire.ReadJSON(conn, &req)
	if err != nil || ft != wire.FramePlanReviewRequest {
		log.Printf("hived: plan review: bad request (%s): %v", ft, err)
		_ = wire.WriteJSON(conn, wire.FramePlanReviewDecision, wire.PlanReviewDecision{Status: wire.PlanReviewInvalid})
		return
	}
	dec := d.decidePlanReview(ctx, conn, req)
	_ = conn.SetWriteDeadline(time.Now().Add(eventReadDeadline))
	_ = wire.WriteJSON(conn, wire.FramePlanReviewDecision, dec)
}

// decidePlanReview runs the gates and, if they pass, parks the review
// and waits for its one outcome.
func (d *Daemon) decidePlanReview(ctx context.Context, conn net.Conn, req wire.PlanReviewRequest) wire.PlanReviewDecision {
	if err := req.Validate(); err != nil {
		log.Printf("hived: plan review: %v", err)
		return wire.PlanReviewDecision{Status: wire.PlanReviewInvalid}
	}
	// Read live, so switching review off takes effect on the next plan.
	// A settings file that will not parse means off: an unreadable
	// preference must never start blocking agents.
	st, err := agent.LoadSettings()
	if err != nil || !st.PlanReview {
		return wire.PlanReviewDecision{Status: wire.PlanReviewDisabled}
	}
	if req.Source == wire.PlanReviewSourceClaude && req.Reviewer != agent.PlanReviewerHive && d.externalReviewer(req.Cwd) {
		return wire.PlanReviewDecision{Status: wire.PlanReviewExternal}
	}

	reviewID, done, err := d.reg.ParkPlanReview(req.SessionID, req.Source, req.Plan)
	switch {
	case errors.Is(err, registry.ErrNoAnswerer):
		return wire.PlanReviewDecision{Status: wire.PlanReviewNoClient}
	case err != nil:
		return wire.PlanReviewDecision{Status: wire.PlanReviewInvalid}
	}

	// The requester sends nothing more; a read returning is it leaving.
	// This goroutine ends when servePlanReview's caller closes conn.
	_ = conn.SetReadDeadline(time.Time{})
	gone := make(chan struct{})
	go func() {
		var b [1]byte
		for {
			if _, err := conn.Read(b[:]); err != nil {
				close(gone)
				return
			}
		}
	}()
	timer := time.NewTimer(planReviewMaxWait)
	defer timer.Stop()

	withdraw := func() wire.PlanReviewDecision {
		d.reg.WithdrawPlanReview(req.SessionID, reviewID, wire.PlanReviewCancelled)
		return wire.PlanReviewDecision{Status: wire.PlanReviewCancelled}
	}
	select {
	case dec := <-done:
		if dec.Status == wire.PlanReviewDeny {
			dec.Message = agent.FormatPlanFeedback(req.Source, dec.Comments, dec.Feedback)
		}
		return dec
	case <-gone:
		// Claude killed its hook because the user answered its own
		// dialog, or Pi's Esc. Withdrawing closes the review in every GUI.
		return withdraw()
	case <-timer.C:
		return withdraw()
	case <-ctx.Done():
		return withdraw()
	case <-d.stop:
		return withdraw()
	}
}

// externalReviewer reports whether another tool reviews ExitPlanMode
// for a Claude session running in cwd.
func (d *Daemon) externalReviewer(cwd string) bool {
	home, err := planReviewHome()
	if err != nil {
		home = ""
	}
	return agent.HasActiveExternalReviewer(agent.ExternalPlanReviewers(agent.ReviewerPaths{
		Home: home, ProjectDir: cwd, Managed: planReviewManaged(),
	}))
}
