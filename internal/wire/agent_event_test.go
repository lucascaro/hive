package wire

import (
	"bytes"
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
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestAgentEventKindsAllowlist pins the exact kind vocabulary the
// daemon validates against — a typo here silently drops every event
// of that kind at the ModeEvent arm.
func TestAgentEventKindsAllowlist(t *testing.T) {
	want := []string{
		AgentEventPrompt, AgentEventTurnEnd, AgentEventWaitingInput,
		AgentEventWaitingPermission, AgentEventPing,
		AgentEventPermissionResolved, AgentEventError, AgentEventSessionEnd,
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
