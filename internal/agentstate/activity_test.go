package agentstate

import (
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

func boolp(b bool) *bool { return &b }

// hooked returns a machine already on the hook tier, so the tests
// exercise activity rather than tier promotion.
func hooked(t *testing.T) (*Machine, time.Time) {
	t.Helper()
	base := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	return New(base), base
}

func start(m *Machine, at time.Time, callID, tool, target string) {
	m.Apply(Event{
		Kind: KindToolStart, Source: wire.StateSourceHook,
		At: at, Now: at, CallID: callID, Tool: tool, Target: target,
	})
}

func end(m *Machine, at time.Time, callID string, ok bool) {
	m.Apply(Event{
		Kind: KindToolEnd, Source: wire.StateSourceHook,
		At: at, Now: at, CallID: callID, OK: boolp(ok),
	})
}

func setPlanAt(m *Machine, at time.Time, items ...wire.PlanItem) {
	m.Apply(Event{
		Kind: KindPlan, Source: wire.StateSourceHook,
		At: at, Now: at, Items: items,
	})
}

// TestRingEvictsAtCap: the ring is bounded and drops the OLDEST.
func TestRingEvictsAtCap(t *testing.T) {
	m, base := hooked(t)
	total := ActivityRingCap + 50
	for i := 0; i < total; i++ {
		at := base.Add(time.Duration(i) * time.Second)
		id := "call-" + itoa(i)
		start(m, at, id, "Bash", "cmd")
		end(m, at.Add(time.Millisecond), id, true)
	}
	events, _ := m.Activity()
	if len(events) != ActivityRingCap {
		t.Fatalf("ring holds %d, want %d", len(events), ActivityRingCap)
	}
	// The first 50 must be gone, and the newest must be present.
	if events[0].CallID != "call-50" {
		t.Errorf("oldest retained = %q, want call-50", events[0].CallID)
	}
	if last := events[len(events)-1]; last.CallID != "call-"+itoa(total-1) {
		t.Errorf("newest = %q, want call-%d", last.CallID, total-1)
	}
}

// TestTallySurvivesEviction is the spec's criterion: the per-step tool
// count must stay right after the events that produced it have aged
// out of the ring. It is correct precisely because the tally lives on
// the plan item rather than being derived from the ring.
func TestTallySurvivesEviction(t *testing.T) {
	m, base := hooked(t)
	setPlanAt(m, base, wire.PlanItem{Text: "step one", Status: wire.PlanStatusActive})

	runs := ActivityRingCap + 25
	for i := 0; i < runs; i++ {
		at := base.Add(time.Duration(i+1) * time.Second)
		id := "c" + itoa(i)
		start(m, at, id, "Bash", "cmd")
		end(m, at.Add(time.Millisecond), id, true)
	}

	events, plan := m.Activity()
	if len(events) != ActivityRingCap {
		t.Fatalf("ring holds %d, want %d", len(events), ActivityRingCap)
	}
	if plan[0].Tools != runs {
		t.Errorf("tally = %d, want %d (a ring-derived count would read %d)",
			plan[0].Tools, runs, ActivityRingCap)
	}
}

// TestPlanReplacementPreservesTallies: TodoWrite fires repeatedly with
// the whole evolving list, so a naive wholesale replacement would zero
// every tally on each fire.
func TestPlanReplacementPreservesTallies(t *testing.T) {
	m, base := hooked(t)
	setPlanAt(m, base,
		wire.PlanItem{Text: "alpha", Status: wire.PlanStatusActive},
		wire.PlanItem{Text: "beta", Status: wire.PlanStatusPending},
	)
	start(m, base.Add(time.Second), "c1", "Bash", "x")
	end(m, base.Add(2*time.Second), "c1", true)
	start(m, base.Add(3*time.Second), "c2", "Edit", "y")
	end(m, base.Add(4*time.Second), "c2", true)

	// The agent revises: alpha done, beta active, a new step appended.
	setPlanAt(m, base.Add(5*time.Second),
		wire.PlanItem{Text: "alpha", Status: wire.PlanStatusDone},
		wire.PlanItem{Text: "beta", Status: wire.PlanStatusActive},
		wire.PlanItem{Text: "gamma", Status: wire.PlanStatusPending},
	)

	_, plan := m.Activity()
	if len(plan) != 3 {
		t.Fatalf("plan has %d items, want 3", len(plan))
	}
	if plan[0].Tools != 2 {
		t.Errorf("alpha tally = %d, want 2 (carried across the replacement)", plan[0].Tools)
	}
	if plan[1].Tools != 0 {
		t.Errorf("beta tally = %d, want 0", plan[1].Tools)
	}
	if plan[2].Tools != 0 {
		t.Errorf("gamma is new; tally = %d, want 0", plan[2].Tools)
	}
}

// TestPlanReplacementDuplicateTextFirstMatchWins: an agent can emit
// two steps with identical text, and truncation can make two long
// steps identical. Each old item must be consumed at most once.
func TestPlanReplacementDuplicateTextFirstMatchWins(t *testing.T) {
	m, base := hooked(t)
	setPlanAt(m, base,
		wire.PlanItem{Text: "same", Status: wire.PlanStatusActive},
		wire.PlanItem{Text: "same", Status: wire.PlanStatusPending},
	)
	start(m, base.Add(time.Second), "c1", "Bash", "x")
	end(m, base.Add(2*time.Second), "c1", true)

	_, before := m.Activity()
	if before[0].Tools != 1 || before[1].Tools != 0 {
		t.Fatalf("setup: tallies = %d,%d want 1,0", before[0].Tools, before[1].Tools)
	}

	setPlanAt(m, base.Add(3*time.Second),
		wire.PlanItem{Text: "same", Status: wire.PlanStatusDone},
		wire.PlanItem{Text: "same", Status: wire.PlanStatusActive},
	)
	_, plan := m.Activity()
	if plan[0].Tools != 1 {
		t.Errorf("first duplicate tally = %d, want 1", plan[0].Tools)
	}
	if plan[1].Tools != 0 {
		t.Errorf("second duplicate tally = %d, want 0 (the first consumed the match)", plan[1].Tools)
	}
}

// TestDurationUsesDaemonClock is the spec's criterion. The reporter's
// own timestamps are skewed by an hour between the two ends; the
// duration must come from the daemon's clock and be unaffected.
func TestDurationUsesDaemonClock(t *testing.T) {
	m, base := hooked(t)
	m.Apply(Event{
		Kind: KindToolStart, Source: wire.StateSourceHook,
		At: base, Now: base, CallID: "c1", Tool: "Bash",
	})
	m.Apply(Event{
		Kind: KindToolEnd, Source: wire.StateSourceHook,
		// The reporter claims an hour passed...
		At: base.Add(time.Hour),
		// ...but the daemon saw 250ms.
		Now:    base.Add(250 * time.Millisecond),
		CallID: "c1", OK: boolp(true),
	})
	events, _ := m.Activity()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].DurationMS != 250 {
		t.Errorf("duration = %dms, want 250ms (reporter clocks must not be used)", events[0].DurationMS)
	}
}

// TestParallelToolsPairByCallID: agents run tools concurrently and can
// finish out of order. Pairing by tool name would fail this outright.
func TestParallelToolsPairByCallID(t *testing.T) {
	m, base := hooked(t)
	start(m, base, "a", "Bash", "npm test")
	start(m, base.Add(10*time.Millisecond), "b", "Bash", "go build")
	// b finishes first.
	end(m, base.Add(100*time.Millisecond), "b", true)
	end(m, base.Add(500*time.Millisecond), "a", false)

	events, _ := m.Activity()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].CallID != "b" || events[0].Target != "go build" {
		t.Errorf("first out = %+v, want the b/go build pair", events[0])
	}
	if events[0].DurationMS != 90 {
		t.Errorf("b duration = %dms, want 90ms", events[0].DurationMS)
	}
	if events[1].CallID != "a" || events[1].Target != "npm test" {
		t.Errorf("second out = %+v, want the a/npm test pair", events[1])
	}
	if events[1].DurationMS != 500 {
		t.Errorf("a duration = %dms, want 500ms", events[1].DurationMS)
	}
	if events[1].OK == nil || *events[1].OK {
		t.Errorf("a should have failed; OK = %v", events[1].OK)
	}
}

// TestToolEndWithoutStart: recorded unpaired rather than dropped.
// "This ran and we missed the beginning" beats silence.
func TestToolEndWithoutStart(t *testing.T) {
	m, base := hooked(t)
	end(m, base, "orphan", true)
	events, _ := m.Activity()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].StartedAt != "" {
		t.Errorf("StartedAt = %q, want empty", events[0].StartedAt)
	}
	if events[0].DurationMS != 0 {
		t.Errorf("duration = %d, want 0 for an unpaired end", events[0].DurationMS)
	}
}

// TestToolStartWithoutCallID still records: an event with no id cannot
// pair, but it must not vanish.
func TestToolStartWithoutCallID(t *testing.T) {
	m, base := hooked(t)
	start(m, base, "", "Bash", "npm test")
	events, _ := m.Activity()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Target != "npm test" {
		t.Errorf("target = %q, want npm test", events[0].Target)
	}
}

// TestOpenCallsBounded: a reporter that dies mid-tool never sends the
// end, so the pending map needs a cap — and, a map having no order, an
// explicit definition of which entry "oldest" means.
func TestOpenCallsBounded(t *testing.T) {
	m, base := hooked(t)
	for i := 0; i < 50; i++ {
		start(m, base.Add(time.Duration(i)*time.Second), "c"+itoa(i), "Bash", "x")
	}
	if got := len(m.act.open); got != activityOpenCap {
		t.Fatalf("open holds %d, want %d", got, activityOpenCap)
	}
	// The oldest starts must be the ones evicted.
	if _, ok := m.act.open["c0"]; ok {
		t.Errorf("c0 (the oldest) should have been evicted")
	}
	if _, ok := m.act.open["c49"]; !ok {
		t.Errorf("c49 (the newest) should be retained")
	}
}

// TestActivityClearedOnNewMachine: a fresh Machine has no history.
// This is what makes "restart/revive clears activity" true without any
// clearing code — the registry builds a new Machine at those points.
func TestActivityClearedOnNewMachine(t *testing.T) {
	m, base := hooked(t)
	setPlanAt(m, base, wire.PlanItem{Text: "x", Status: wire.PlanStatusActive})
	start(m, base.Add(time.Second), "c1", "Bash", "x")
	end(m, base.Add(2*time.Second), "c1", true)

	fresh := New(base)
	events, plan := fresh.Activity()
	if len(events) != 0 || len(plan) != 0 {
		t.Errorf("fresh machine has %d events and %d plan items, want 0 and 0", len(events), len(plan))
	}
	if d, tot, cur := fresh.planSummary(); d != 0 || tot != 0 || cur != "" {
		t.Errorf("fresh summary = %d/%d %q, want 0/0 \"\"", d, tot, cur)
	}
}

// TestPlanSummary is what the sidebar row renders from — and the only
// activity a client needs to draw a row.
func TestPlanSummary(t *testing.T) {
	m, base := hooked(t)
	setPlanAt(m, base,
		wire.PlanItem{Text: "one", Status: wire.PlanStatusDone},
		wire.PlanItem{Text: "two", Status: wire.PlanStatusDone},
		wire.PlanItem{Text: "three", Status: wire.PlanStatusActive},
		wire.PlanItem{Text: "four", Status: wire.PlanStatusPending},
	)
	snap := m.Snapshot()
	if snap.PlanDone != 2 || snap.PlanTotal != 4 {
		t.Errorf("plan = %d/%d, want 2/4", snap.PlanDone, snap.PlanTotal)
	}
	if snap.CurrentTool != "" {
		t.Errorf("CurrentTool = %q with nothing running, want empty", snap.CurrentTool)
	}

	start(m, base.Add(time.Second), "c1", "Bash", "npm test")
	if got := m.Snapshot().CurrentTool; got != "Bash" {
		t.Errorf("CurrentTool = %q, want Bash", got)
	}
	// The newest start wins when several run at once.
	start(m, base.Add(2*time.Second), "c2", "WebFetch", "example.com")
	if got := m.Snapshot().CurrentTool; got != "WebFetch" {
		t.Errorf("CurrentTool = %q, want the newest (WebFetch)", got)
	}
	end(m, base.Add(3*time.Second), "c2", true)
	if got := m.Snapshot().CurrentTool; got != "Bash" {
		t.Errorf("CurrentTool = %q after the newest ended, want Bash", got)
	}
}

// TestToolEventsKeepWorkingState pins the behaviour the split must not
// change: these three hooks used to collapse into permission_resolved,
// which moved the session to working. Splitting them apart must move
// no glyph.
func TestToolEventsKeepWorkingState(t *testing.T) {
	for _, kind := range []string{KindToolStart, KindToolEnd} {
		m, base := hooked(t)
		m.Apply(Event{
			Kind: KindWaitingPermission, Source: wire.StateSourceHook,
			At: base, Now: base,
		})
		if got := m.Snapshot().State; got != wire.StateWaitingPermission {
			t.Fatalf("setup: state = %q", got)
		}
		m.Apply(Event{
			Kind: kind, Source: wire.StateSourceHook,
			At: base.Add(time.Second), Now: base.Add(time.Second),
			CallID: "c1", Tool: "Bash", OK: boolp(true),
		})
		if got := m.Snapshot().State; got != wire.StateWorking {
			t.Errorf("%s: state = %q, want working", kind, got)
		}
	}
}

// TestPlanEventChangesNoState: revising a plan is not a transition.
func TestPlanEventChangesNoState(t *testing.T) {
	m, base := hooked(t)
	m.Apply(Event{
		Kind: KindWaitingInput, Source: wire.StateSourceHook,
		At: base, Now: base,
	})
	setPlanAt(m, base.Add(time.Second), wire.PlanItem{Text: "x", Status: wire.PlanStatusActive})
	if got := m.Snapshot().State; got != wire.StateWaitingInput {
		t.Errorf("state = %q, want waiting_input (a plan must not move it)", got)
	}
}

// TestPlanItemsNormalised: caps and status coercion at the daemon
// boundary, independent of whatever the reporter did.
func TestPlanItemsNormalised(t *testing.T) {
	m, base := hooked(t)
	long := ""
	for i := 0; i < wire.MaxPlanTextLen+50; i++ {
		long += "x"
	}
	items := []wire.PlanItem{{Text: long, Status: "not-a-status"}}
	for i := 0; i < wire.MaxPlanItems+10; i++ {
		items = append(items, wire.PlanItem{Text: "filler" + itoa(i), Status: wire.PlanStatusPending})
	}
	setPlanAt(m, base, items...)

	_, plan := m.Activity()
	if len(plan) != wire.MaxPlanItems {
		t.Errorf("plan has %d items, want the cap %d", len(plan), wire.MaxPlanItems)
	}
	if len(plan[0].Text) > wire.MaxPlanTextLen {
		t.Errorf("item text is %d bytes, want <= %d", len(plan[0].Text), wire.MaxPlanTextLen)
	}
	if plan[0].Status != wire.PlanStatusPending {
		t.Errorf("status = %q, want it coerced to pending (not dropped)", plan[0].Status)
	}
}

// itoa avoids pulling strconv into the test for one call site.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// --- Per-item plan updates: Claude's task tools ---

func mergeAt(m *Machine, at time.Time, items ...wire.PlanItem) {
	m.Apply(Event{
		Kind: KindPlanItem, Source: wire.StateSourceHook,
		At: at, Now: at, Items: items,
	})
}

// TestMergePlanItemsLifecycle walks one task through what a real
// session does: create, start, complete, rename, delete.
func TestMergePlanItemsLifecycle(t *testing.T) {
	m, base := hooked(t)
	tick := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }

	mergeAt(m, tick(1), wire.PlanItem{ID: "1", Text: "alpha", Status: wire.PlanStatusPending})
	mergeAt(m, tick(2), wire.PlanItem{ID: "2", Text: "beta", Status: wire.PlanStatusPending})
	_, plan := m.Activity()
	if len(plan) != 2 {
		t.Fatalf("after two creates: %d items, want 2", len(plan))
	}

	// Status-only update: the text must survive. Treating the empty
	// Text as "clear it" would blank the step on every update.
	mergeAt(m, tick(3), wire.PlanItem{ID: "1", Status: wire.PlanStatusActive})
	_, plan = m.Activity()
	if plan[0].Text != "alpha" || plan[0].Status != wire.PlanStatusActive {
		t.Errorf("after status update: %+v, want alpha/active", plan[0])
	}

	// Rename-only update: the status must survive.
	mergeAt(m, tick(4), wire.PlanItem{ID: "1", Text: "alpha renamed"})
	_, plan = m.Activity()
	if plan[0].Text != "alpha renamed" || plan[0].Status != wire.PlanStatusActive {
		t.Errorf("after rename: %+v, want alpha renamed/active", plan[0])
	}

	mergeAt(m, tick(5), wire.PlanItem{ID: "1", Status: wire.PlanStatusDone})
	if s := m.Snapshot(); s.PlanDone != 1 || s.PlanTotal != 2 {
		t.Errorf("summary = %d/%d, want 1/2", s.PlanDone, s.PlanTotal)
	}

	// Delete removes the step outright — it must not linger as a
	// pending item dragging the fraction down.
	mergeAt(m, tick(6), wire.PlanItem{ID: "2", Status: wire.PlanStatusDeleted})
	_, plan = m.Activity()
	if len(plan) != 1 || plan[0].ID != "1" {
		t.Errorf("after delete: %+v, want only task 1", plan)
	}
	if s := m.Snapshot(); s.PlanDone != 1 || s.PlanTotal != 1 {
		t.Errorf("summary after delete = %d/%d, want 1/1", s.PlanDone, s.PlanTotal)
	}
}

// TestMergePlanItemUnknownIDIsAdded: Hive attached after the agent
// created the task, or the create's hook event was lost. The step is
// still real; refusing it would under-count the plan.
func TestMergePlanItemUnknownIDIsAdded(t *testing.T) {
	m, base := hooked(t)
	mergeAt(m, base, wire.PlanItem{ID: "9", Status: wire.PlanStatusActive})
	_, plan := m.Activity()
	if len(plan) != 1 || plan[0].ID != "9" || plan[0].Status != wire.PlanStatusActive {
		t.Errorf("plan = %+v, want task 9 added as active", plan)
	}
}

// TestMergePlanItemDeleteUnknownIsHarmless: deleting a task the daemon
// never saw is a no-op, not a new "deleted" step.
func TestMergePlanItemDeleteUnknownIsHarmless(t *testing.T) {
	m, base := hooked(t)
	mergeAt(m, base, wire.PlanItem{ID: "9", Status: wire.PlanStatusDeleted})
	if _, plan := m.Activity(); len(plan) != 0 {
		t.Errorf("plan = %+v, want empty", plan)
	}
}

// TestMergePlanItemWithoutIDIgnored: it cannot be merged into anything.
func TestMergePlanItemWithoutIDIgnored(t *testing.T) {
	m, base := hooked(t)
	mergeAt(m, base, wire.PlanItem{Text: "orphan", Status: wire.PlanStatusActive})
	if _, plan := m.Activity(); len(plan) != 0 {
		t.Errorf("plan = %+v, want empty", plan)
	}
}

// TestMergePlanItemKeepsTally: the tally is the daemon's own count; an
// update from a reporter never resets or overwrites it.
func TestMergePlanItemKeepsTally(t *testing.T) {
	m, base := hooked(t)
	mergeAt(m, base, wire.PlanItem{ID: "1", Text: "work", Status: wire.PlanStatusActive})
	start(m, base.Add(time.Second), "c1", "Bash", "x")
	end(m, base.Add(2*time.Second), "c1", true)
	start(m, base.Add(3*time.Second), "c2", "Edit", "y")

	mergeAt(m, base.Add(4*time.Second), wire.PlanItem{ID: "1", Status: wire.PlanStatusDone, Tools: 99})
	_, plan := m.Activity()
	if plan[0].Tools != 2 {
		t.Errorf("tally = %d, want 2 (never taken from an update)", plan[0].Tools)
	}
}

// TestTaskListResyncKeepsTalliesByID: a TaskList wholesale resync must
// carry tallies by ID — exactly, even when a task was renamed, which
// text matching would lose.
func TestTaskListResyncKeepsTalliesByID(t *testing.T) {
	m, base := hooked(t)
	mergeAt(m, base, wire.PlanItem{ID: "1", Text: "old name", Status: wire.PlanStatusActive})
	start(m, base.Add(time.Second), "c1", "Bash", "x")
	end(m, base.Add(2*time.Second), "c1", true)

	setPlanAt(m, base.Add(3*time.Second),
		wire.PlanItem{ID: "1", Text: "new name", Status: wire.PlanStatusDone},
		wire.PlanItem{ID: "2", Text: "fresh", Status: wire.PlanStatusPending},
	)
	_, plan := m.Activity()
	if plan[0].Tools != 1 {
		t.Errorf("renamed task tally = %d, want 1 (matched by ID, not text)", plan[0].Tools)
	}
	if plan[1].Tools != 0 {
		t.Errorf("new task tally = %d, want 0", plan[1].Tools)
	}
}

// TestIDMatchNeverCrossesToTextMatch: an item WITH an ID must not steal
// the tally of an ID-less item that happens to share its text.
func TestIDMatchNeverCrossesToTextMatch(t *testing.T) {
	m, base := hooked(t)
	setPlanAt(m, base, wire.PlanItem{Text: "shared", Status: wire.PlanStatusActive})
	start(m, base.Add(time.Second), "c1", "Bash", "x")
	end(m, base.Add(2*time.Second), "c1", true)

	setPlanAt(m, base.Add(3*time.Second), wire.PlanItem{ID: "1", Text: "shared", Status: wire.PlanStatusActive})
	if _, plan := m.Activity(); plan[0].Tools != 0 {
		t.Errorf("tally = %d, want 0 (an ID item must not match an ID-less one)", plan[0].Tools)
	}
}

// TestMergePlanItemsCapped: a runaway agent cannot grow the plan past
// the wire limit one create at a time.
func TestMergePlanItemsCapped(t *testing.T) {
	m, base := hooked(t)
	for i := 0; i < wire.MaxPlanItems+20; i++ {
		mergeAt(m, base.Add(time.Duration(i)*time.Millisecond),
			wire.PlanItem{ID: itoa(i), Text: "t", Status: wire.PlanStatusPending})
	}
	if _, plan := m.Activity(); len(plan) != wire.MaxPlanItems {
		t.Errorf("plan has %d items, want the cap %d", len(plan), wire.MaxPlanItems)
	}
}

// TestPlanItemChangesNoState: like a wholesale plan, a per-item update
// is not a state transition.
func TestPlanItemChangesNoState(t *testing.T) {
	m, base := hooked(t)
	m.Apply(Event{Kind: KindWaitingInput, Source: wire.StateSourceHook, At: base, Now: base})
	mergeAt(m, base.Add(time.Second), wire.PlanItem{ID: "1", Text: "x", Status: wire.PlanStatusActive})
	if got := m.Snapshot().State; got != wire.StateWaitingInput {
		t.Errorf("state = %q, want waiting_input", got)
	}
}
