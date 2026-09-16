package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

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

// first is the sole event from a payload that maps to exactly one —
// which is every payload except a TodoWrite PostToolUse.
func first(t *testing.T, evs []wire.AgentEvent) wire.AgentEvent {
	t.Helper()
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	return evs[0]
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
		{"post_tool_use.json", wire.AgentEventToolEnd, ""},
		{"pre_tool_use.json", wire.AgentEventToolStart, ""},
		{"post_tool_use_failure.json", wire.AgentEventToolEnd, ""},
		{"permission_request_question.json", wire.AgentEventWaitingInput, ""},
		{"session_end.json", wire.AgentEventSessionEnd, ""},
		{"unknown_event.json", wire.AgentEventPing, ""},
		{"malformed.json", wire.AgentEventPing, ""},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			ev := first(t, mapHookPayload(readFixture(t, tc.fixture)))
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
	ev := first(t, mapHookPayload([]byte(`{"hook_event_name":"TotallyMadeUp"}`)))
	if ev.Kind != wire.AgentEventPing {
		t.Errorf("kind = %q, want ping", ev.Kind)
	}
}

func TestHookMalformedJSONIsPing(t *testing.T) {
	ev := first(t, mapHookPayload([]byte(`not json at all`)))
	if ev.Kind != wire.AgentEventPing {
		t.Errorf("kind = %q, want ping", ev.Kind)
	}
}

func TestHookEmptyStdinIsPing(t *testing.T) {
	ev := first(t, mapHookPayload(nil))
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

// --- Activity: tool events, plan extraction, and the privacy rule ---

// TestHookToolEventFields pins the tool fields on the split-out
// tool_start / tool_end events, including the ok pointer that
// distinguishes "absent" from "false".
func TestHookToolEventFields(t *testing.T) {
	start := first(t, mapHookPayload(readFixture(t, "pre_tool_use.json")))
	if start.Kind != wire.AgentEventToolStart {
		t.Errorf("kind = %q, want tool_start", start.Kind)
	}
	if start.Tool != "Bash" {
		t.Errorf("tool = %q, want Bash", start.Tool)
	}
	// "sleep 12; touch probe.txt" — the head, not the command.
	if start.Target != "sleep 12" {
		t.Errorf("target = %q, want %q", start.Target, "sleep 12")
	}
	if start.OK != nil {
		t.Errorf("OK = %v on a tool_start, want nil", start.OK)
	}

	end := first(t, mapHookPayload(readFixture(t, "post_tool_use.json")))
	if end.Kind != wire.AgentEventToolEnd {
		t.Errorf("kind = %q, want tool_end", end.Kind)
	}
	if end.OK == nil || !*end.OK {
		t.Errorf("OK = %v, want true", end.OK)
	}

	fail := first(t, mapHookPayload(readFixture(t, "post_tool_use_failure.json")))
	if fail.OK == nil || *fail.OK {
		t.Errorf("OK = %v on a failure, want false", fail.OK)
	}
}

// TestHookCallID pins tool_use_id → call_id, and the documented
// behaviour when it is absent: the event still reports, it just never
// pairs. Pairing by tool name is not a fallback — Claude runs tools in
// parallel and two concurrent Bash calls are indistinguishable by name.
func TestHookCallID(t *testing.T) {
	ev := first(t, mapHookPayload(readFixture(t, "pre_tool_use_windows_path.json")))
	if ev.CallID != "toolu_01WIN" {
		t.Errorf("call_id = %q, want toolu_01WIN", ev.CallID)
	}

	none := first(t, mapHookPayload(readFixture(t, "pre_tool_use_no_call_id.json")))
	if none.CallID != "" {
		t.Errorf("call_id = %q, want empty", none.CallID)
	}
	if none.Kind != wire.AgentEventToolStart {
		t.Errorf("an event without tool_use_id must still report; kind = %q", none.Kind)
	}
	if none.Target != "npm test" {
		t.Errorf("target = %q, want %q", none.Target, "npm test")
	}
}

// TestHookTodoWriteEmitsPlan pins the one payload that produces TWO
// events: a TodoWrite PostToolUse is both "a tool finished" and "here
// is the whole plan".
func TestHookTodoWriteEmitsPlan(t *testing.T) {
	evs := mapHookPayload(readFixture(t, "post_tool_use_todowrite.json"))
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2 (tool_end + plan)", len(evs))
	}
	if evs[0].Kind != wire.AgentEventToolEnd {
		t.Errorf("first kind = %q, want tool_end", evs[0].Kind)
	}
	plan := evs[1]
	if plan.Kind != wire.AgentEventPlan {
		t.Fatalf("second kind = %q, want plan", plan.Kind)
	}
	if plan.Source != wire.StateSourceHook || plan.At == "" {
		t.Errorf("plan event lost its source/At: %+v", plan)
	}
	want := []wire.PlanItem{
		{Text: "read the spec", Status: wire.PlanStatusDone},
		{Text: "write the code", Status: wire.PlanStatusActive},
		{Text: "run the tests", Status: wire.PlanStatusPending},
		// No "content": falls back to activeForm. Unknown status
		// "banana" is coerced to pending rather than dropping the step.
		{Text: "Filing the PR", Status: wire.PlanStatusPending},
	}
	if len(plan.Items) != len(want) {
		t.Fatalf("got %d items, want %d: %+v", len(plan.Items), len(want), plan.Items)
	}
	for i := range want {
		if plan.Items[i] != want[i] {
			t.Errorf("item %d = %+v, want %+v", i, plan.Items[i], want[i])
		}
	}

	// A PreToolUse TodoWrite is only a tool starting — the plan rides
	// the Post, where the call actually happened.
	pre := mapHookPayload(readFixture(t, "pre_tool_use_todowrite.json"))
	if len(pre) != 1 {
		t.Errorf("PreToolUse TodoWrite produced %d events, want 1", len(pre))
	}
}

// TestHookMalformedToolInput: tool_input that is not an object yields
// no label rather than a stringification of whatever it was — which is
// exactly how raw arguments would leak.
func TestHookMalformedToolInput(t *testing.T) {
	ev := first(t, mapHookPayload(readFixture(t, "post_tool_use_malformed_input.json")))
	if ev.Kind != wire.AgentEventToolEnd {
		t.Errorf("kind = %q, want tool_end", ev.Kind)
	}
	if ev.Target != "" {
		t.Errorf("target = %q, want empty for a non-object tool_input", ev.Target)
	}
}

// TestHookOversizedArgumentsCapped pins the label cap. An over-long
// label means the derivation over-captured, so the cap is a backstop
// against a leak as much as a size limit.
func TestHookOversizedArgumentsCapped(t *testing.T) {
	ev := first(t, mapHookPayload(readFixture(t, "post_tool_use_oversized.json")))
	if len(ev.Target) > wire.MaxTargetLen {
		t.Errorf("target is %d bytes, want <= %d", len(ev.Target), wire.MaxTargetLen)
	}
	if !utf8.ValidString(ev.Target) {
		t.Errorf("target is not valid UTF-8 after capping")
	}
}

// TestHookNeverLeaksToolInput is the spec's "asserted in a test, not by
// inspection" criterion.
//
// Two assertions, because the obvious one — "no tool_input value
// appears anywhere in the frame" — FALSE-FAILS on a correct
// implementation: post_tool_use.json is {"command":"ls"} and `ls` is
// the right derived label.
func TestHookNeverLeaksToolInput(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "hooks"))
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}

	// 1. Structural: no frame carries a tool_input key or any nested
	// object beyond the plan items. This is what actually fails if
	// someone ever passes tool_input through.
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		for _, ev := range mapHookPayload(readFixture(t, e.Name())) {
			b, err := json.Marshal(ev)
			if err != nil {
				t.Fatalf("%s: marshal: %v", e.Name(), err)
			}
			var generic map[string]any
			if err := json.Unmarshal(b, &generic); err != nil {
				t.Fatalf("%s: unmarshal: %v", e.Name(), err)
			}
			if _, bad := generic["tool_input"]; bad {
				t.Errorf("%s: frame carries tool_input", e.Name())
			}
			for k, v := range generic {
				if k == "items" {
					continue // the plan, which is meant to be shown
				}
				switch v.(type) {
				case map[string]any, []any:
					t.Errorf("%s: field %q carries structured data (%T); "+
						"only scalars and the plan may cross the socket", e.Name(), k, v)
				}
			}
		}
	}

	// 2. Secret-bearing: the material that must never appear, and the
	// label that must.
	secrets := []struct {
		fixture   string
		mustNot   string
		wantLabel string
	}{
		{"pre_tool_use_secret_header.json", "sk-live-SUPERSECRET", "curl"},
		{"pre_tool_use_token_url.json", "SUPERSECRETTOKEN", "api.example.com"},
		{"pre_tool_use_windows_path.json", "C:\\Users\\dev", "machine.go"},
	}
	for _, tc := range secrets {
		t.Run(tc.fixture, func(t *testing.T) {
			for _, ev := range mapHookPayload(readFixture(t, tc.fixture)) {
				b, err := json.Marshal(ev)
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				if strings.Contains(string(b), tc.mustNot) {
					t.Errorf("frame leaked %q: %s", tc.mustNot, b)
				}
				if ev.Target != tc.wantLabel {
					t.Errorf("target = %q, want %q", ev.Target, tc.wantLabel)
				}
			}
		})
	}
}
