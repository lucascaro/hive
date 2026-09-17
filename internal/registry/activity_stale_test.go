package registry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

func stamp(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }

// nextActivity returns the next delta, or ok=false if none arrives soon.
func nextActivity(ch ActivityListener) (wire.ActivityMsg, bool) {
	select {
	case msg := <-ch:
		return msg, true
	case <-time.After(200 * time.Millisecond):
		return wire.ActivityMsg{}, false
	}
}

// TestActivityDeltaForEveryTierEvent: staleness needs a fresh stale_at
// whenever the tier reports, so every accepted kind fans out — the kinds
// that carry no activity send a bare liveness frame. A rejected event of
// any kind sends nothing.
func TestActivityDeltaForEveryTierEvent(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{Name: "c", Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ch, unsub := r.SubscribeActivity()
	defer unsub()

	base := time.Now().Add(-time.Minute)
	kinds := []wire.AgentEvent{
		{Kind: wire.AgentEventPrompt},
		{Kind: wire.AgentEventPing},
		{Kind: wire.AgentEventSubagentStart, AgentID: "a1", AgentType: "Explore"},
		{Kind: wire.AgentEventSubagentEnd, AgentID: "a1", AgentType: "Explore"},
		{Kind: wire.AgentEventTurnEnd},
	}
	for i, ev := range kinds {
		ev.SessionID, ev.Source = e.ID, wire.StateSourceHook
		ev.At = stamp(base.Add(time.Duration(i) * time.Second))
		if err := r.ApplyAgentEvent(e.ID, ev); err != nil {
			t.Fatalf("%s: %v", ev.Kind, err)
		}
		msg, ok := nextActivity(ch)
		if !ok {
			t.Fatalf("%s: no ACTIVITY delta", ev.Kind)
		}
		if msg.StaleAt == "" {
			t.Errorf("%s: delta has no stale_at", ev.Kind)
		}
		if len(msg.Events) != 0 || msg.Plan != nil {
			t.Errorf("%s: liveness delta carries events/plan: %+v", ev.Kind, msg)
		}
	}

	// Stamped well before the last main-thread event, inside the ordering
	// window: dropped. (Late tool kinds are applied by design, so they are
	// not in this list; agentstate covers that split.)
	old := stamp(base.Add(-10 * time.Second))
	for _, ev := range []wire.AgentEvent{
		{Kind: wire.AgentEventPing},
		{Kind: wire.AgentEventPlan, Items: []wire.PlanItem{{Text: "x", Status: wire.PlanStatusActive}}},
	} {
		ev.SessionID, ev.Source, ev.At = e.ID, wire.StateSourceHook, old
		if err := r.ApplyAgentEvent(e.ID, ev); err != nil {
			t.Fatalf("%s: %v", ev.Kind, err)
		}
		if msg, ok := nextActivity(ch); ok {
			t.Errorf("rejected %s broadcast %+v", ev.Kind, msg)
		}
	}
}

// TestActivitySnapshotCarriesStaleAt: GET_ACTIVITY carries the deadline
// too, so a panel opened between reports is not blind until the next one.
func TestActivitySnapshotCarriesStaleAt(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{Name: "c", Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	snap, err := r.ActivitySnapshot(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.StaleAt != "" {
		t.Errorf("heuristic session snapshot has stale_at %q", snap.StaleAt)
	}
	if err := r.ApplyAgentEvent(e.ID, wire.AgentEvent{
		SessionID: e.ID, Kind: wire.AgentEventPing, Source: wire.StateSourceHook, At: stamp(time.Now()),
	}); err != nil {
		t.Fatal(err)
	}
	snap, _ = r.ActivitySnapshot(e.ID)
	at, err := time.Parse(time.RFC3339Nano, snap.StaleAt)
	if err != nil {
		t.Fatalf("stale_at %q: %v", snap.StaleAt, err)
	}
	if d := time.Until(at); d <= 0 || d > time.Minute {
		t.Errorf("stale_at %v is not ~HookStaleAfter ahead", at)
	}
}

// TestActivityPlanEmptiedMarshalsEmptyArray: an emptied plan must be
// distinguishable from "plan unchanged" on the wire.
func TestActivityPlanEmptiedMarshalsEmptyArray(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{Name: "c", Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ch, unsub := r.SubscribeActivity()
	defer unsub()
	base := time.Now()
	apply := func(i int, ev wire.AgentEvent) string {
		t.Helper()
		ev.SessionID, ev.Source, ev.At = e.ID, wire.StateSourceHook, stamp(base.Add(time.Duration(i)*time.Second))
		if err := r.ApplyAgentEvent(e.ID, ev); err != nil {
			t.Fatal(err)
		}
		msg, ok := nextActivity(ch)
		if !ok {
			t.Fatalf("%s: no delta", ev.Kind)
		}
		b, _ := json.Marshal(msg)
		return string(b)
	}
	apply(0, wire.AgentEvent{Kind: wire.AgentEventPlan, Items: []wire.PlanItem{{Text: "a", Status: wire.PlanStatusActive}}})
	if got := apply(1, wire.AgentEvent{Kind: wire.AgentEventPlan}); !strings.Contains(got, `"plan":[]`) {
		t.Errorf("emptied plan frame = %s, want \"plan\":[]", got)
	}
	if got := apply(2, wire.AgentEvent{Kind: wire.AgentEventToolStart, Tool: "Bash", CallID: "c1"}); strings.Contains(got, `"plan":[`) {
		t.Errorf("tool delta claims a plan change: %s", got)
	}
}
