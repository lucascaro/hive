package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "hooks", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// TestHookMapsEveryEvent pins the mapping table in the plan: every
// Claude hook event Hive wires maps to the exact AgentEvent kind (and,
// where applicable, text) the design specifies.
func TestHookMapsEveryEvent(t *testing.T) {
	cases := []struct {
		fixture  string
		wantKind string
		wantText string
	}{
		{"session_start.json", wire.AgentEventPing, ""},
		{"user_prompt_submit.json", wire.AgentEventPrompt, "reply pong"},
		{"stop.json", wire.AgentEventTurnEnd, "pong"},
		{"stop_failure.json", wire.AgentEventError, "overloaded"},
		{"notification_permission.json", wire.AgentEventWaitingPermission, ""},
		{"notification_idle.json", wire.AgentEventWaitingInput, ""},
		{"permission_request.json", wire.AgentEventWaitingPermission, ""},
		{"post_tool_use.json", wire.AgentEventPermissionResolved, ""},
		{"pre_tool_use.json", wire.AgentEventPermissionResolved, ""},
		{"post_tool_use_failure.json", wire.AgentEventPermissionResolved, ""},
		{"permission_request_question.json", wire.AgentEventWaitingInput, ""},
		{"session_end.json", wire.AgentEventSessionEnd, ""},
		{"unknown_event.json", wire.AgentEventPing, ""},
		{"malformed.json", wire.AgentEventPing, ""},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			ev := mapHookPayload(readFixture(t, tc.fixture))
			if ev.Kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", ev.Kind, tc.wantKind)
			}
			if ev.Text != tc.wantText {
				t.Errorf("text = %q, want %q", ev.Text, tc.wantText)
			}
			if ev.Source != wire.StateSourceHook {
				t.Errorf("source = %q, want hook", ev.Source)
			}
			if ev.At == "" {
				t.Errorf("At is empty")
			}
		})
	}
}

func TestHookUnknownEventIsPing(t *testing.T) {
	ev := mapHookPayload([]byte(`{"hook_event_name":"TotallyMadeUp"}`))
	if ev.Kind != wire.AgentEventPing {
		t.Errorf("kind = %q, want ping", ev.Kind)
	}
}

func TestHookMalformedJSONIsPing(t *testing.T) {
	ev := mapHookPayload([]byte(`not json at all`))
	if ev.Kind != wire.AgentEventPing {
		t.Errorf("kind = %q, want ping", ev.Kind)
	}
}

func TestHookEmptyStdinIsPing(t *testing.T) {
	ev := mapHookPayload(nil)
	if ev.Kind != wire.AgentEventPing {
		t.Errorf("kind = %q, want ping", ev.Kind)
	}
}

// TestHookNoEnvExitsZero pins the "not running under Hive" contract:
// runHook must do nothing observable (no dial attempt, no panic) when
// either env var is missing — a user running `claude` outside Hive with
// a copied --settings file must see nothing.
func TestHookNoEnvExitsZero(t *testing.T) {
	t.Setenv("HIVE_SESSION_ID", "")
	t.Setenv("HIVE_SOCKET", "")
	// If this dials anything it will hang or error; the test's own
	// timeout (go test default) is the safety net. A more direct
	// assertion isn't available without a network seam, but the run
	// completing quickly is exactly the "did nothing" we're pinning.
	runHook(strings.NewReader(`{"hook_event_name":"Stop","last_assistant_message":"x"}`))
}
