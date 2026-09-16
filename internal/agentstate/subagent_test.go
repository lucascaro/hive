package agentstate

import (
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// Subagent attribution (spec 416, phase 1b). Claude fires tool hooks
// for calls made inside subagents, tagged with agent_id, and on the
// PARENT's session_id. An event with AgentID is recorded, but never
// moves the session's state, CurrentTool, plan or plan-step tally.

func subStart(m *Machine, at time.Time, agent, callID, tool string) {
	m.Apply(Event{
		Kind: KindToolStart, Source: wire.StateSourceHook, At: at, Now: at,
		CallID: callID, Tool: tool, AgentID: agent, AgentType: "general-purpose",
	})
}

func subEnd(m *Machine, at time.Time, agent, callID string) {
	m.Apply(Event{
		Kind: KindToolEnd, Source: wire.StateSourceHook, At: at, Now: at,
		CallID: callID, OK: boolp(true), AgentID: agent, AgentType: "general-purpose",
	})
}

func lifecycle(m *Machine, kind string, at time.Time, agent string) {
	m.Apply(Event{Kind: kind, Source: wire.StateSourceHook, At: at, Now: at, AgentID: agent, AgentType: "general-purpose"})
}

func turnEnd(m *Machine, at time.Time, running *[]string) {
	m.Apply(Event{Kind: KindTurnEnd, Source: wire.StateSourceHook, At: at, Now: at, RunningAgents: running})
}

func ids(s ...string) *[]string { return &s }

func TestSubagentToolKeepsMainCurrentTool(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	start(m, ms(0), "main-agent", "Agent", "")
	subStart(m, ms(10), "A", "a1", "Bash")
	subStart(m, ms(20), "B", "b1", "Read")
	subEnd(m, ms(30), "A", "a1")
	subStart(m, ms(40), "A", "a2", "Grep")
	if got := m.Snapshot().CurrentTool; got != "Agent" {
		t.Errorf("CurrentTool = %q, want Agent: subagent calls must not take over the main thread's", got)
	}
}

func TestSubagentToolDoesNotTallyPlanStep(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	setPlanAt(m, ms(0), wire.PlanItem{ID: "1", Text: "step", Status: wire.PlanStatusActive})
	start(m, ms(1), "m1", "Bash", "")
	end(m, ms(2), "m1", true)
	subStart(m, ms(3), "A", "a1", "Bash")
	subEnd(m, ms(4), "A", "a1")
	// Unpaired: a PostToolUse whose start was never seen.
	subEnd(m, ms(5), "A", "never-started")

	events, plan := m.Activity()
	if plan[0].Tools != 1 {
		t.Errorf("step tally = %d, want 1 (the main-thread call only)", plan[0].Tools)
	}
	if len(events) != 3 {
		t.Fatalf("ring holds %d, want 3", len(events))
	}
	for _, ev := range events[1:] {
		if ev.AgentID != "A" || ev.AgentType != "general-purpose" {
			t.Errorf("ring entry %s: agent = (%q, %q), want (A, general-purpose)", ev.CallID, ev.AgentID, ev.AgentType)
		}
		if ev.PlanIdx != -1 {
			t.Errorf("ring entry %s: PlanIdx = %d, want -1 for a subagent call", ev.CallID, ev.PlanIdx)
		}
	}
	if events[0].PlanIdx != 0 || events[0].AgentID != "" {
		t.Errorf("main entry = %+v, want PlanIdx 0 and no agent", events[0])
	}
}

// Risk 5 from the plan, confirmed by the capture: Claude fires the
// parent's Stop while subagents are still running.
func TestSubagentToolAfterStopKeepsWaiting(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	lifecycle(m, KindSubagentStart, ms(0), "B")
	turnEnd(m, ms(10), ids("B"))
	subStart(m, ms(20), "B", "b1", "Bash")
	subEnd(m, ms(70), "B", "b1")

	if got := m.Snapshot().State; got != wire.StateWaitingInput {
		t.Errorf("state = %q, want waiting_input: a subagent working must not reopen the parent's finished turn", got)
	}
	events, _ := m.Activity()
	if len(events) != 1 || events[0].DurationMS != 50 {
		t.Errorf("ring = %+v, want one paired 50ms call", events)
	}
}

func TestTurnEndKeepsSubagentOpenCalls(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	lifecycle(m, KindSubagentStart, ms(0), "B")
	subStart(m, ms(5), "B", "b1", "Bash")
	turnEnd(m, ms(10), ids("B"))
	subEnd(m, ms(105), "B", "b1")
	events, _ := m.Activity()
	if len(events) != 1 || events[0].StartedAt == "" || events[0].DurationMS != 100 {
		t.Errorf("ring = %+v, want the subagent call to pair across the parent's turn end", events)
	}
}

func TestLateSubagentStartIsOpenNotFinishedTurn(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	lifecycle(m, KindSubagentStart, ms(0), "B")
	turnEnd(m, ms(50), ids("B"))
	// Stamped before the parent's Stop, delivered after it.
	m.Apply(Event{Kind: KindToolStart, Source: wire.StateSourceHook, At: ms(40), Now: ms(60), CallID: "b1", Tool: "Bash", AgentID: "B"})
	subEnd(m, ms(90), "B", "b1")
	events, _ := m.Activity()
	if len(events) != 1 || events[0].StartedAt == "" {
		t.Errorf("ring = %+v, want one paired call: a subagent start is not bound to the parent's turn", events)
	}
}

func TestSubagentPlanEventIgnored(t *testing.T) {
	m, base := hooked(t)
	setPlanAt(m, base, wire.PlanItem{ID: "1", Text: "parent", Status: wire.PlanStatusActive})
	m.Apply(Event{Kind: KindPlan, Source: wire.StateSourceHook, At: base.Add(time.Millisecond), AgentID: "A",
		Items: []wire.PlanItem{{ID: "9", Text: "child", Status: wire.PlanStatusPending}}})
	m.Apply(Event{Kind: KindPlanItem, Source: wire.StateSourceHook, At: base.Add(2 * time.Millisecond), AgentID: "A",
		Items: []wire.PlanItem{{ID: "1", Status: wire.PlanStatusDone}}})
	_, plan := m.Activity()
	if len(plan) != 1 || plan[0].Text != "parent" || plan[0].Status != wire.PlanStatusActive {
		t.Errorf("plan = %+v, want the parent's plan untouched", plan)
	}
}

func TestSubagentsRunningCount(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	step := func(kind string, n int, agent string, want int) {
		t.Helper()
		lifecycle(m, kind, ms(n), agent)
		if got := m.Snapshot().SubagentsRunning; got != want {
			t.Errorf("after %s %s: SubagentsRunning = %d, want %d", kind, agent, got, want)
		}
	}
	step(KindSubagentStart, 1, "A", 1)
	step(KindSubagentStart, 2, "B", 2)
	step(KindSubagentEnd, 3, "A", 1)
	step(KindSubagentStart, 4, "B", 1) // duplicate
	step(KindSubagentEnd, 5, "B", 0)
	step(KindSubagentEnd, 6, "B", 0) // duplicate
	if got := m.Snapshot().State; got != wire.StateIdle {
		t.Errorf("state = %q, want idle: subagent lifecycle is not a transition", got)
	}
}

func TestLateSubagentStartAfterEndStaysEnded(t *testing.T) {
	m, base := hooked(t)
	lifecycle(m, KindSubagentEnd, base.Add(10*time.Millisecond), "A")
	lifecycle(m, KindSubagentStart, base, "A")
	if got := m.Snapshot().SubagentsRunning; got != 0 {
		t.Errorf("SubagentsRunning = %d, want 0: a late start must not resurrect an ended subagent", got)
	}
}

func TestTurnEndReconcilesSubagents(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	lifecycle(m, KindSubagentStart, ms(0), "A")
	subStart(m, ms(1), "A", "a1", "Bash")
	lifecycle(m, KindSubagentStart, ms(20), "C") // started after the Stop was generated
	turnEnd(m, ms(10), ids())

	if got := m.Snapshot().SubagentsRunning; got != 1 {
		t.Errorf("SubagentsRunning = %d, want 1 (A reconciled away, C kept)", got)
	}
	subEnd(m, ms(30), "A", "a1")
	if events, _ := m.Activity(); len(events) != 1 || events[0].StartedAt != "" {
		t.Errorf("ring = %+v, want A's call forgotten at reconcile, so its end is unpaired", events)
	}
	lifecycle(m, KindSubagentStart, ms(2), "A")
	if got := m.Snapshot().SubagentsRunning; got != 1 {
		t.Errorf("SubagentsRunning = %d, want 1: a reconciled subagent must not come back", got)
	}

	turnEnd(m, ms(40), nil)
	if got := m.Snapshot().SubagentsRunning; got != 1 {
		t.Errorf("SubagentsRunning = %d after a Stop that reported nothing, want 1", got)
	}
}

func TestSubagentsClearedOnSessionEndAndExit(t *testing.T) {
	m, base := hooked(t)
	lifecycle(m, KindSubagentStart, base, "A")
	m.Apply(Event{Kind: KindSessionEnd, Source: wire.StateSourceHook, At: base.Add(time.Millisecond)})
	if got := m.Snapshot().SubagentsRunning; got != 0 {
		t.Errorf("after session_end: SubagentsRunning = %d, want 0", got)
	}

	m, base = hooked(t)
	lifecycle(m, KindSubagentStart, base, "A")
	m.Exit()
	if got := m.Snapshot().SubagentsRunning; got != 0 {
		t.Errorf("after Exit: SubagentsRunning = %d, want 0", got)
	}
}

func TestSubagentStartAfterSessionEndIgnored(t *testing.T) {
	m, base := hooked(t)
	m.Apply(Event{Kind: KindSessionEnd, Source: wire.StateSourceHook, At: base})
	lifecycle(m, KindSubagentStart, base.Add(time.Millisecond), "A")
	if got := m.Snapshot().SubagentsRunning; got != 0 {
		t.Errorf("SubagentsRunning = %d on an exited session, want 0", got)
	}
}

func TestOpenCapEvictsSubagentCallsFirst(t *testing.T) {
	m, base := hooked(t)
	start(m, base, "main", "Agent", "")
	for i := 0; i < activityOpenCap+5; i++ {
		subStart(m, base.Add(time.Duration(i+1)*time.Millisecond), "A", "a"+itoa(i), "Bash")
	}
	if got := m.Snapshot().CurrentTool; got != "Agent" {
		t.Errorf("CurrentTool = %q, want Agent: a fan-out must not evict the main thread's running call", got)
	}
}

func TestSubagentCountIsCapped(t *testing.T) {
	m, base := hooked(t)
	for i := 0; i < wire.MaxRunningAgents+10; i++ {
		lifecycle(m, KindSubagentStart, base.Add(time.Duration(i)*time.Millisecond), "s"+itoa(i))
	}
	if got := m.Snapshot().SubagentsRunning; got != wire.MaxRunningAgents {
		t.Errorf("SubagentsRunning = %d, want the cap %d", got, wire.MaxRunningAgents)
	}
}

// The ordering-guard hole the plan's second opinion found: a subagent
// event stamped just after the parent's Stop, applied first, must not
// get the Stop dropped as out of order.
func TestSubagentEventDoesNotDropParentStop(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	start(m, ms(0), "m1", "Agent", "")
	end(m, ms(1), "m1", true)
	lifecycle(m, KindSubagentStart, ms(2), "A")
	subEnd(m, ms(105), "A", "a1")
	turnEnd(m, ms(100), ids())

	snap := m.Snapshot()
	if snap.State != wire.StateWaitingInput {
		t.Errorf("state = %q, want waiting_input: the parent's Stop was dropped", snap.State)
	}
	if snap.SubagentsRunning != 0 {
		t.Errorf("SubagentsRunning = %d, want 0: the Stop's reconcile did not run", snap.SubagentsRunning)
	}
}

func TestSubagentEventsKeepHookTierTrusted(t *testing.T) {
	m, base := hooked(t)
	sec := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }
	turnEnd(m, sec(0), nil)
	subStart(m, sec(25), "A", "a1", "Bash")
	subEnd(m, sec(50), "A", "a1")
	if !m.trusted(sec(55)) {
		t.Error("hook tier went stale while a subagent was still reporting")
	}
}

func TestClockStepBackAfterSubagentEvent(t *testing.T) {
	m, base := hooked(t)
	sec := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }
	subStart(m, sec(100), "A", "a1", "Bash")
	// The host clock stepped back 40s; the main thread's next report wins.
	m.Apply(Event{Kind: KindPrompt, Source: wire.StateSourceHook, At: sec(60)})
	if got := m.Snapshot().State; got != wire.StateWorking {
		t.Fatalf("state = %q, want working: a report HookStaleAfter or more behind is a clock step and applies", got)
	}
	if m.trusted(sec(60).Add(HookStaleAfter + time.Second)) {
		t.Error("trusted() is still measured from the future subagent stamp; clock-step recovery is broken")
	}
}

func TestPermissionInsideSubagentStillWaits(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	start(m, ms(0), "m1", "Agent", "")
	subStart(m, ms(1), "A", "a1", "Bash")
	m.Apply(Event{Kind: KindWaitingPermission, Source: wire.StateSourceHook, At: ms(2)})
	if got := m.Snapshot().State; got != wire.StateWaitingPermission {
		t.Errorf("state = %q, want waiting_permission", got)
	}
}

// An inverted pair from a subagent that has already ended: its end was
// recorded unpaired, its subagent_end has passed, and nothing will ever
// close a call opened now. The start reaches the ring but never opens.
func TestLateStartForEndedSubagentIsNotOpened(t *testing.T) {
	m, base := hooked(t)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	lifecycle(m, KindSubagentStart, ms(0), "A")
	subEnd(m, ms(20), "A", "a1")
	lifecycle(m, KindSubagentEnd, ms(30), "A")
	subStart(m, ms(10), "A", "a1", "Bash")

	if n := len(m.act.open); n != 0 {
		t.Errorf("open calls = %d, want 0: a start for an ended subagent must not open", n)
	}
	events, _ := m.Activity()
	if len(events) != 2 || events[1].AgentID != "A" || events[1].StartedAt == "" {
		t.Errorf("ring = %+v, want the unpaired end then the start, both recorded", events)
	}
}
