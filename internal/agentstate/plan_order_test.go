package agentstate

import (
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// Parallel task-tool calls race. Claude runs TaskCreate / TaskUpdate calls
// from one message in parallel, each hook is its own process, and their
// plan_item events reach the daemon in any order. Found live: an agent
// 3 of 4 steps through its plan showed 1/4, because every inverted
// plan_item went down the late path and was dropped whole.

func planItemAt(m *Machine, at time.Time, id, text, status string) {
	m.Apply(Event{
		Kind: KindPlanItem, Source: wire.StateSourceHook, At: at, Now: at,
		Items: []wire.PlanItem{{ID: id, Text: text, Status: status}},
	})
}

func TestParallelPlanItemsSurviveInversion(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	// Four parallel creates, delivered 1, 3, 4, 2.
	planItemAt(m, ms(10), "1", "a", wire.PlanStatusPending)
	planItemAt(m, ms(12), "3", "c", wire.PlanStatusPending)
	planItemAt(m, ms(13), "4", "d", wire.PlanStatusPending)
	planItemAt(m, ms(11), "2", "b", wire.PlanStatusPending)
	planItemAt(m, ms(100), "1", "", wire.PlanStatusActive)
	// 1 done, 2 done, 3 active in parallel, delivered 1, 3, 2.
	planItemAt(m, ms(200), "1", "", wire.PlanStatusDone)
	planItemAt(m, ms(202), "3", "", wire.PlanStatusActive)
	planItemAt(m, ms(201), "2", "", wire.PlanStatusDone)
	// 3 done, 4 active in parallel, delivered 4, 3.
	planItemAt(m, ms(301), "4", "", wire.PlanStatusActive)
	planItemAt(m, ms(300), "3", "", wire.PlanStatusDone)

	if s := m.Snapshot(); s.PlanDone != 3 || s.PlanTotal != 4 {
		t.Errorf("plan = %d/%d, want 3/4", s.PlanDone, s.PlanTotal)
	}
}

// The case the late-drop existed for still holds: two updates to the SAME
// step, inverted, must not regress it.
func TestLatePlanItemDoesNotRegressSameStep(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	planItemAt(m, ms(10), "1", "a", wire.PlanStatusPending)
	planItemAt(m, ms(30), "1", "", wire.PlanStatusDone)
	planItemAt(m, ms(20), "1", "", wire.PlanStatusActive)
	if _, plan := m.Activity(); plan[0].Status != wire.PlanStatusDone {
		t.Errorf("status = %q, want done: an older update regressed the step", plan[0].Status)
	}
}

// An update delivered before its create: the create's text still lands,
// its older status does not.
func TestLateCreateFillsTextOnly(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	planItemAt(m, ms(20), "1", "", wire.PlanStatusActive)
	planItemAt(m, ms(10), "1", "write tests", wire.PlanStatusPending)
	_, plan := m.Activity()
	if len(plan) != 1 || plan[0].Text != "write tests" || plan[0].Status != wire.PlanStatusActive {
		t.Errorf("plan = %+v, want one active step named %q", plan, "write tests")
	}
}

// A delete wins over an older update delivered after it.
func TestLateUpdateDoesNotResurrectDeletedStep(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	planItemAt(m, ms(10), "1", "a", wire.PlanStatusPending)
	planItemAt(m, ms(30), "1", "", wire.PlanStatusDeleted)
	planItemAt(m, ms(20), "1", "", wire.PlanStatusActive)
	if _, plan := m.Activity(); len(plan) != 0 {
		t.Errorf("plan = %+v, want the deleted step to stay deleted", plan)
	}
}

// A late plan_item changes the plan, never state.
func TestLatePlanItemLeavesStateAlone(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	planItemAt(m, ms(10), "1", "a", wire.PlanStatusPending)
	m.Apply(Event{Kind: KindTurnEnd, Source: wire.StateSourceHook, At: ms(50), Now: ms(50)})
	planItemAt(m, ms(40), "1", "", wire.PlanStatusDone)
	s := m.Snapshot()
	if s.State != wire.StateWaitingInput {
		t.Errorf("state = %q, want waiting_input", s.State)
	}
	if s.PlanDone != 1 {
		t.Errorf("PlanDone = %d, want 1: the late completion applies", s.PlanDone)
	}
}
