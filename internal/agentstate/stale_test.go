package agentstate

import (
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// TestStaleAtUsesDaemonClock: the deadline is measured from the daemon's
// reading (Event.Now), never the reporter's stamp — a reporter an hour
// behind must not make a live session read as stale.
func TestStaleAtUsesDaemonClock(t *testing.T) {
	m, base := hooked(t)
	now := base.Add(time.Hour)
	m.Apply(Event{Kind: KindPing, Source: wire.StateSourceHook, At: base, Now: now})
	got, ok := m.StaleAt()
	if !ok {
		t.Fatal("StaleAt not set after a hook event")
	}
	if want := now.Add(HookStaleAfter); !got.Equal(want) {
		t.Errorf("StaleAt = %v, want %v (daemon clock + HookStaleAfter)", got, want)
	}
}

// TestStaleAtAdvancesOnPingAndSubagent: every accepted tier report moves
// the deadline, a subagent's included; a rejected one does not.
func TestStaleAtAdvancesOnPingAndSubagent(t *testing.T) {
	m, base := hooked(t)
	m.Apply(Event{Kind: KindPrompt, Source: wire.StateSourceHook, At: base, Now: base})
	m.TakeAccepted()

	t1 := base.Add(5 * time.Second)
	m.Apply(Event{Kind: KindPing, Source: wire.StateSourceHook, At: t1, Now: t1})
	if got, _ := m.StaleAt(); !got.Equal(t1.Add(HookStaleAfter)) {
		t.Errorf("after ping StaleAt = %v, want %v", got, t1.Add(HookStaleAfter))
	}

	t2 := base.Add(10 * time.Second)
	m.Apply(Event{
		Kind: KindToolStart, Source: wire.StateSourceHook, At: t2, Now: t2,
		CallID: "s1", Tool: "Read", AgentID: "agent-1", AgentType: "Explore",
	})
	if got, _ := m.StaleAt(); !got.Equal(t2.Add(HookStaleAfter)) {
		t.Errorf("after subagent event StaleAt = %v, want %v", got, t2.Add(HookStaleAfter))
	}
	m.TakeAccepted()

	// A main-thread ping stamped before the last main-thread event, inside
	// the ordering window, is dropped by the late path.
	t3 := base.Add(20 * time.Second)
	m.Apply(Event{Kind: KindPing, Source: wire.StateSourceHook, At: base.Add(time.Second), Now: t3})
	if got, _ := m.StaleAt(); !got.Equal(t2.Add(HookStaleAfter)) {
		t.Errorf("a rejected event moved StaleAt to %v", got)
	}
}

// TestStaleAtAbsentOnHeuristic: a session no tier ever reported for has
// no deadline at all.
func TestStaleAtAbsentOnHeuristic(t *testing.T) {
	m, _ := hooked(t)
	if _, ok := m.StaleAt(); ok {
		t.Error("fresh heuristic machine reports a StaleAt")
	}
}

// TestTakeAcceptedFalseForOutOfOrderEvent: the registry broadcasts only
// what the machine accepted, so an out-of-order drop must leave the flag
// clear — for a plan kind as much as a tool kind.
func TestTakeAcceptedFalseForOutOfOrderEvent(t *testing.T) {
	m, base := hooked(t)
	later := base.Add(5 * time.Second)
	m.Apply(Event{Kind: KindPrompt, Source: wire.StateSourceHook, At: later, Now: later})
	if !m.TakeAccepted() {
		t.Fatal("a normal event was not marked accepted")
	}
	if m.TakeAccepted() {
		t.Fatal("TakeAccepted did not consume the flag")
	}
	for _, kind := range []string{KindPlan, KindPing, KindTurnEnd} {
		m.Apply(Event{
			Kind: kind, Source: wire.StateSourceHook, At: base, Now: later,
			Items: []wire.PlanItem{{Text: "x", Status: wire.PlanStatusActive}},
		})
		if m.TakeAccepted() {
			t.Errorf("out-of-order %s marked accepted", kind)
		}
	}
	// The late path keeps tool kinds: accepted.
	m.Apply(Event{Kind: KindToolStart, Source: wire.StateSourceHook, At: base, Now: later, CallID: "c", Tool: "Bash"})
	if !m.TakeAccepted() {
		t.Error("late tool_start applied but not marked accepted")
	}
}
