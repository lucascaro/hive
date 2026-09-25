package wire

import (
	"encoding/json"
	"strings"
	"testing"
)

// The GUI keys off the snake_case names; a tag typo is invisible in Go
// and silently leaves the review un-raised.
func TestPlanReviewWireSessionInfoKeys(t *testing.T) {
	raw, err := json.Marshal(SessionInfo{ID: "s1", PendingPlanReview: &PendingPlanReview{
		ReviewID: "r1", Source: PlanReviewSourceClaude, CreatedAt: "2026-09-24T00:00:00Z",
	}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"pending_plan_review"`, `"review_id"`, `"source"`, `"created_at"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("wire JSON is missing %s: %s", key, raw)
		}
	}
}

// SessionInfo rides every broadcast: nothing pending means no key.
func TestPlanReviewWireOmittedWhenAbsent(t *testing.T) {
	raw, err := json.Marshal(SessionInfo{ID: "s1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "pending_plan_review") {
		t.Errorf("field must be omitempty; got %s", raw)
	}
}

func TestPlanReviewWireRequestValidate(t *testing.T) {
	ok := PlanReviewRequest{SessionID: "s1", Source: PlanReviewSourcePi, Plan: "# plan"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid request refused: %v", err)
	}
	for name, r := range map[string]PlanReviewRequest{
		"no session": {Source: PlanReviewSourceClaude, Plan: "x"},
		"bad source": {SessionID: "s1", Source: "codex", Plan: "x"},
		"empty plan": {SessionID: "s1", Source: PlanReviewSourceClaude},
		"oversize":   {SessionID: "s1", Source: PlanReviewSourceClaude, Plan: strings.Repeat("a", MaxPlanReviewLen+1)},
	} {
		if r.Validate() == nil {
			t.Errorf("%s: accepted, want refusal", name)
		}
	}
	// The cap must fit a frame even after JSON escaping doubles it
	// (every byte a `\"`), or a legal plan would be unsendable.
	if 2*MaxPlanReviewLen+1024 > MaxPayload {
		t.Errorf("MaxPlanReviewLen %d cannot fit MaxPayload %d escaped", MaxPlanReviewLen, MaxPayload)
	}
}

func TestPlanReviewWireResolveValidate(t *testing.T) {
	ok := ResolvePlanReviewReq{SessionID: "s1", ReviewID: "r1", Decision: PlanReviewDeny,
		Comments: []PlanComment{{Quote: "a", Text: "b"}}, Feedback: "c"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid resolve refused: %v", err)
	}
	many := make([]PlanComment, MaxPlanReviewComments+1)
	for name, r := range map[string]ResolvePlanReviewReq{
		"no review id":  {SessionID: "s1", Decision: PlanReviewApprove},
		"bad decision":  {SessionID: "s1", ReviewID: "r1", Decision: PlanReviewCancelled},
		"many comments": {SessionID: "s1", ReviewID: "r1", Decision: PlanReviewDeny, Comments: many},
		"long quote": {SessionID: "s1", ReviewID: "r1", Decision: PlanReviewDeny,
			Comments: []PlanComment{{Quote: strings.Repeat("q", MaxPlanReviewQuoteLen+1)}}},
		"long feedback": {SessionID: "s1", ReviewID: "r1", Decision: PlanReviewDeny,
			Feedback: strings.Repeat("f", MaxPlanReviewFeedbackLen+1)},
	} {
		if r.Validate() == nil {
			t.Errorf("%s: accepted, want refusal", name)
		}
	}
}

func TestPlanReviewWireFrameNames(t *testing.T) {
	for ft, want := range map[FrameType]string{
		FramePlanReviewRequest:  "PLAN_REVIEW_REQUEST",
		FramePlanReviewDecision: "PLAN_REVIEW_DECISION",
		FrameGetPlanReview:      "GET_PLAN_REVIEW",
		FramePlanReview:         "PLAN_REVIEW",
		FrameResolvePlanReview:  "RESOLVE_PLAN_REVIEW",
	} {
		if got := ft.String(); got != want {
			t.Errorf("%#x.String() = %q, want %q", byte(ft), got, want)
		}
	}
	if name, ok := ControlEventName(FramePlanReview); !ok || name != "planreview:plan" {
		t.Errorf("PLAN_REVIEW must fan out as planreview:plan, got %q %v", name, ok)
	}
}
