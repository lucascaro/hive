package registry

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/lucascaro/hive/internal/wire"
)

// ErrNoAnswerer is returned by ParkPlanReview when no connected client
// can answer: the requester should fall back to the agent's own
// terminal approval rather than wait.
var ErrNoAnswerer = errors.New("no client can answer a plan review")

// ErrSessionNotAlive is returned by ParkPlanReview for a session with
// no running process. A review for it could never be acted on.
var ErrSessionNotAlive = errors.New("session is not running")

// planReview is one agent plan waiting on the user.
//
// The requester's connection is held open by the daemon for the whole
// wait, but nothing here blocks: the review is data on the entry, like
// a parked worktree choice, and the requester reads its decision from
// done. Every transition happens under r.mu, so a review is decided
// exactly once.
type planReview struct {
	id      string
	source  string
	plan    string
	created time.Time
	// done receives the one decision. Buffered 1 and written only via
	// finish, so a decision never blocks the registry and a requester
	// that already left never leaks a goroutine.
	done chan wire.PlanReviewDecision
}

func (p *planReview) info() *wire.PendingPlanReview {
	if p == nil {
		return nil
	}
	return &wire.PendingPlanReview{
		ReviewID:  p.id,
		Source:    p.source,
		CreatedAt: p.created.UTC().Format(time.RFC3339),
	}
}

// finish delivers d. Callers hold r.mu and clear the entry's field.
func (p *planReview) finish(d wire.PlanReviewDecision) {
	select {
	case p.done <- d:
	default:
	}
}

// finishPlanReviewLocked decides e's pending review, clears it and
// tells every client, all under the r.mu the caller holds.
func (r *Registry) finishPlanReviewLocked(e *Entry, d wire.PlanReviewDecision) {
	e.planReview.finish(d)
	e.planReview = nil
	r.broadcastLocked(wire.SessionEventUpdated, e.Info())
}

// ParkPlanReview parks plan on session id for the user to review and
// returns the review's id and the channel its one decision arrives on.
//
// The answerer check and the park happen under one r.mu hold, the same
// lock SetAnswerers takes to withdraw reviews when the last client
// leaves. A park can therefore never land in the gap after that
// withdrawal and wait days for nobody.
//
// A newer request for the same session replaces the older one, which
// is decided wire.PlanReviewCancelled: the agent has moved on to a new
// plan, and approving the stale one would be wrong.
func (r *Registry) ParkPlanReview(id, source, plan string) (string, <-chan wire.PlanReviewDecision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return "", nil, ErrNotFound
	}
	if !e.Alive() {
		return "", nil, ErrSessionNotAlive
	}
	if !r.canAskUserLocked() {
		return "", nil, ErrNoAnswerer
	}
	if e.planReview != nil {
		e.planReview.finish(wire.PlanReviewDecision{Status: wire.PlanReviewCancelled})
	}
	pr := &planReview{
		id:      uuid.NewString(),
		source:  source,
		plan:    plan,
		created: time.Now(),
		done:    make(chan wire.PlanReviewDecision, 1),
	}
	e.planReview = pr
	r.broadcastLocked(wire.SessionEventUpdated, e.Info())
	return pr.id, pr.done, nil
}

// WithdrawPlanReview drops session id's review if it is still reviewID
// — the requester went away (Claude killed its hook because the user
// answered in the terminal, Pi's Esc, the daemon stopping). Withdrawing
// a review that was already decided or replaced is a no-op.
func (r *Registry) WithdrawPlanReview(id, reviewID string, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok || e.planReview == nil || e.planReview.id != reviewID {
		return
	}
	r.finishPlanReviewLocked(e, wire.PlanReviewDecision{Status: status})
}

// PlanReviewText returns the source and plan of session id's pending
// review, if it is still reviewID.
func (r *Registry) PlanReviewText(id, reviewID string) (source, plan string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, found := r.entries[id]
	if !found || e.planReview == nil || e.planReview.id != reviewID {
		return "", "", false
	}
	return e.planReview.source, e.planReview.plan, true
}

// ResolvePlanReview applies the user's answer. It reports whether a
// review was decided: an answer for a review that is no longer pending,
// or whose id does not match, is ignored, so two windows racing to
// answer produce exactly one decision.
func (r *Registry) ResolvePlanReview(req wire.ResolvePlanReviewReq) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[req.SessionID]
	if !ok || e.planReview == nil || e.planReview.id != req.ReviewID {
		return false
	}
	r.finishPlanReviewLocked(e, wire.PlanReviewDecision{
		Status:   req.Decision,
		Comments: req.Comments,
		Feedback: req.Feedback,
	})
	return true
}
