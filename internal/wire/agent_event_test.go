package wire

import (
	"bytes"
	"reflect"
	"testing"
)

// TestAgentEventRoundTrip pins the FrameAgentEvent JSON round trip that
// `hived hook` and the daemon's ModeEvent arm depend on.
func TestAgentEventRoundTrip(t *testing.T) {
	want := AgentEvent{
		SessionID: "sess-1",
		Kind:      AgentEventPrompt,
		Source:    StateSourceHook,
		Text:      "say pong",
		At:        "2026-09-04T12:00:00.123456789Z",
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, FrameAgentEvent, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got AgentEvent
	ft, err := ReadJSON(&buf, &got)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ft != FrameAgentEvent {
		t.Errorf("frame type = %s, want AGENT_EVENT", ft)
	}
	// reflect.DeepEqual, not ==: AgentEvent carries Items []PlanItem
	// since the activity kinds landed, so the struct is no longer
	// comparable.
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestAgentEventActivityRoundTrip pins the tool/plan fields. They are
// all omitempty, which is what lets an AgentEvent from a reporter that
// predates them round-trip unchanged — asserted by the plain round
// trip above, which sets none of them.
func TestAgentEventActivityRoundTrip(t *testing.T) {
	ok := true
	want := AgentEvent{
		SessionID: "sess-1",
		Kind:      AgentEventToolEnd,
		Source:    StateSourceHook,
		At:        "2026-09-04T12:00:00.123456789Z",
		Tool:      "Bash",
		Target:    "npm test",
		CallID:    "toolu_01ABC",
		OK:        &ok,
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, FrameAgentEvent, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got AgentEvent
	if _, err := ReadJSON(&buf, &got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Tool != want.Tool || got.Target != want.Target || got.CallID != want.CallID {
		t.Errorf("tool fields: got %+v, want %+v", got, want)
	}
	// A pointer so "absent" and "false" stay distinguishable — the
	// whole reason OK is not a plain bool.
	if got.OK == nil || *got.OK != true {
		t.Errorf("OK = %v, want true", got.OK)
	}

	var absent AgentEvent
	buf.Reset()
	if err := WriteJSON(&buf, FrameAgentEvent, AgentEvent{Kind: AgentEventToolStart}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadJSON(&buf, &absent); err != nil {
		t.Fatalf("read: %v", err)
	}
	if absent.OK != nil {
		t.Errorf("absent OK = %v, want nil", absent.OK)
	}
}

// TestPlanItemRoundTrip pins the plan payload's wire spelling.
func TestPlanItemRoundTrip(t *testing.T) {
	want := AgentEvent{
		SessionID: "sess-1",
		Kind:      AgentEventPlan,
		Source:    StateSourceHook,
		Items: []PlanItem{
			{Text: "read the spec", Status: PlanStatusDone, Tools: 3},
			{Text: "write the code", Status: PlanStatusActive},
		},
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, FrameAgentEvent, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got AgentEvent
	if _, err := ReadJSON(&buf, &got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !reflect.DeepEqual(got.Items, want.Items) {
		t.Errorf("items: got %+v, want %+v", got.Items, want.Items)
	}
}

// TestAgentEventKindsAllowlist pins the exact kind vocabulary the
// daemon validates against — a typo here silently drops every event
// of that kind at the ModeEvent arm.
func TestAgentEventKindsAllowlist(t *testing.T) {
	want := []string{
		AgentEventPrompt, AgentEventTurnEnd, AgentEventIdle, AgentEventWaitingInput,
		AgentEventWaitingPermission, AgentEventPing,
		AgentEventPermissionResolved, AgentEventError, AgentEventSessionEnd,
		AgentEventToolStart, AgentEventToolEnd, AgentEventPlan,
	}
	if len(AgentEventKinds) != len(want) {
		t.Fatalf("AgentEventKinds has %d entries, want %d", len(AgentEventKinds), len(want))
	}
	for _, k := range want {
		if !AgentEventKinds[k] {
			t.Errorf("AgentEventKinds missing %q", k)
		}
	}
	if AgentEventKinds["bogus"] {
		t.Errorf("AgentEventKinds accepted an unknown kind")
	}
}

// TestModeEventValue pins the wire spelling of the new mode — a typo
// here means the daemon's HELLO switch and the hook's Hello never
// agree on what to call it.
func TestModeEventValue(t *testing.T) {
	if ModeEvent != "event" {
		t.Errorf("ModeEvent = %q, want %q", ModeEvent, "event")
	}
}
