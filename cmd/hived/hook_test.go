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
	// "sleep 12; touch probe.txt" — the head, not the command. `12` is an
	// argument, and digits never pass as a subcommand, so it is just
	// `sleep`.
	if start.Target != "sleep" {
		t.Errorf("target = %q, want %q", start.Target, "sleep")
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

// TestHookFailureCarriesToolAndCallID pins the PostToolUseFailure shape
// against a payload captured from a live session.
//
// PostToolUseFailure fires INSTEAD of PostToolUse when a tool fails —
// verified by running `exit 3` through a real Claude session with a
// capture hook: PreToolUse then PostToolUseFailure, no PostToolUse. It
// carries the same tool_use_id as its PreToolUse, and — unlike the
// hand-written fixture this replaced, which had only `error` — it
// carries tool_input as well. So the ok=false arm is reachable, it
// pairs, and it derives a label like any other tool event.
func TestHookFailureCarriesToolAndCallID(t *testing.T) {
	start := first(t, mapHookPayload(readFixture(t, "pre_tool_use.json")))
	fail := first(t, mapHookPayload(readFixture(t, "post_tool_use_failure.json")))

	if fail.CallID == "" {
		t.Error("a failure must carry tool_use_id, or it can never pair with its start")
	}
	if fail.Tool != "Bash" {
		t.Errorf("tool = %q, want Bash", fail.Tool)
	}
	// tool_input is present on the failure too, so the label is derived
	// from it exactly as on the success path.
	if fail.Target != "npm test" {
		t.Errorf("target = %q, want %q", fail.Target, "npm test")
	}
	// And the live capture showed the id is byte-identical across the
	// pair; the fixtures use different ids only because they describe
	// two different calls.
	if start.CallID == "" {
		t.Error("PreToolUse must carry tool_use_id")
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
		{"pre_tool_use_env_prefix_secret.json", "ghp_SUPERSECRETPREFIX", "gh api"},
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

// --- Claude's task tools: the plan source on current Claude Code ---
//
// TodoWrite is disabled by default in favour of TaskCreate / TaskUpdate
// / TaskList, and on current models neither is provided unless the
// session opts in. The fixtures below mirror payloads captured from a
// live Claude Code 2.1.273 session, not the documentation.

// planOf returns the plan event a payload produced, failing the test if
// it did not produce exactly a tool_end followed by one.
func planOf(t *testing.T, fixture string) wire.AgentEvent {
	t.Helper()
	evs := mapHookPayload(readFixture(t, fixture))
	if len(evs) != 2 {
		t.Fatalf("%s: got %d events, want 2 (tool_end + plan)", fixture, len(evs))
	}
	if evs[0].Kind != wire.AgentEventToolEnd {
		t.Errorf("%s: first event = %q, want tool_end", fixture, evs[0].Kind)
	}
	return evs[1]
}

// noPlan asserts a payload reported the tool call and nothing else.
func noPlan(t *testing.T, fixture string) {
	t.Helper()
	evs := mapHookPayload(readFixture(t, fixture))
	if len(evs) != 1 {
		t.Fatalf("%s: got %d events, want only the tool event", fixture, len(evs))
	}
}

// TestHookTaskCreate: the task's ID exists only in the PostToolUse
// response — Claude assigns it — so that is where it must be read.
func TestHookTaskCreate(t *testing.T) {
	ev := planOf(t, "post_tool_use_taskcreate.json")
	if ev.Kind != wire.AgentEventPlanItem {
		t.Fatalf("kind = %q, want plan_item", ev.Kind)
	}
	want := wire.PlanItem{ID: "1", Text: "Create one.txt", Status: wire.PlanStatusPending}
	if len(ev.Items) != 1 || ev.Items[0] != want {
		t.Errorf("items = %+v, want [%+v]", ev.Items, want)
	}
}

// TestHookTaskCreateWithoutIDEmitsNoPlan: a step nothing can ever
// update is worse than no step.
func TestHookTaskCreateWithoutIDEmitsNoPlan(t *testing.T) {
	evs := mapHookPayload([]byte(`{"hook_event_name":"PostToolUse","tool_name":"TaskCreate",
		"tool_use_id":"toolu_1","tool_input":{"subject":"x"},"tool_response":{}}`))
	if len(evs) != 1 {
		t.Errorf("got %d events, want only the tool event", len(evs))
	}
}

// TestHookTaskCreatePreToolUseHasNoPlan: the ID is not known yet.
func TestHookTaskCreatePreToolUseHasNoPlan(t *testing.T) {
	evs := mapHookPayload([]byte(`{"hook_event_name":"PreToolUse","tool_name":"TaskCreate",
		"tool_use_id":"toolu_1","tool_input":{"subject":"x"}}`))
	if len(evs) != 1 || evs[0].Kind != wire.AgentEventToolStart {
		t.Errorf("got %+v, want a lone tool_start", evs)
	}
}

// TestHookTaskUpdate covers each thing a TaskUpdate carries. It sends
// only what changed, so an absent field must stay absent on the event
// — the daemon reads empty as "unchanged".
func TestHookTaskUpdate(t *testing.T) {
	cases := []struct {
		fixture string
		want    wire.PlanItem
	}{
		{"post_tool_use_taskupdate_status.json", wire.PlanItem{ID: "1", Status: wire.PlanStatusActive}},
		{"post_tool_use_taskupdate_rename.json", wire.PlanItem{ID: "1", Text: "alpha renamed"}},
		{"post_tool_use_taskupdate_delete.json", wire.PlanItem{ID: "2", Status: wire.PlanStatusDeleted}},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			ev := planOf(t, tc.fixture)
			if ev.Kind != wire.AgentEventPlanItem {
				t.Fatalf("kind = %q, want plan_item", ev.Kind)
			}
			if len(ev.Items) != 1 || ev.Items[0] != tc.want {
				t.Errorf("items = %+v, want [%+v]", ev.Items, tc.want)
			}
		})
	}
}

// TestHookTaskUpdateDescriptionOnlyEmitsNoPlan: nothing the plan shows
// changed.
func TestHookTaskUpdateDescriptionOnlyEmitsNoPlan(t *testing.T) {
	noPlan(t, "post_tool_use_taskupdate_description_only.json")
}

// TestHookTaskListResyncs: the response is the complete list, so it is
// sent wholesale — which heals any update the daemon missed.
func TestHookTaskListResyncs(t *testing.T) {
	ev := planOf(t, "post_tool_use_tasklist.json")
	if ev.Kind != wire.AgentEventPlan {
		t.Fatalf("kind = %q, want plan (wholesale)", ev.Kind)
	}
	want := []wire.PlanItem{
		{ID: "1", Text: "alpha renamed", Status: wire.PlanStatusDone},
		{ID: "3", Text: "gamma", Status: wire.PlanStatusActive},
		{ID: "4", Text: "delta", Status: wire.PlanStatusPending},
	}
	if len(ev.Items) != len(want) {
		t.Fatalf("items = %+v, want %+v", ev.Items, want)
	}
	for i := range want {
		if ev.Items[i] != want[i] {
			t.Errorf("item %d = %+v, want %+v", i, ev.Items[i], want[i])
		}
	}
}

// TestHookTaskListEmptyStillResyncs: an empty list is a real answer —
// every task cleared — and must reach the daemon, or a stale plan would
// outlive the tasks it described.
func TestHookTaskListEmptyStillResyncs(t *testing.T) {
	evs := mapHookPayload([]byte(`{"hook_event_name":"PostToolUse","tool_name":"TaskList",
		"tool_use_id":"toolu_1","tool_input":{},"tool_response":{"tasks":[]}}`))
	if len(evs) != 2 || evs[1].Kind != wire.AgentEventPlan || len(evs[1].Items) != 0 {
		t.Errorf("got %+v, want tool_end + an empty plan", evs)
	}
}

// TestHookFailedPlanningCallChangesNoPlan: a failed call did not change
// the agent's list, so it must not change Hive's — for the task tools
// and for TodoWrite alike.
func TestHookFailedPlanningCallChangesNoPlan(t *testing.T) {
	for _, f := range []string{
		"post_tool_use_failure_taskupdate.json",
		"post_tool_use_failure_todowrite.json",
	} {
		t.Run(f, func(t *testing.T) {
			evs := mapHookPayload(readFixture(t, f))
			if len(evs) != 1 {
				t.Fatalf("got %d events, want only the failed tool_end", len(evs))
			}
			if evs[0].OK == nil || *evs[0].OK {
				t.Errorf("OK = %v, want false", evs[0].OK)
			}
		})
	}
}

// TestIDStringAcceptsNumbers: Claude sends "1", but a number is the
// obvious way for that field to change shape.
func TestIDStringAcceptsNumbers(t *testing.T) {
	for in, want := range map[any]string{"7": "7", float64(7): "7", nil: "", true: ""} {
		if got := idString(in); got != want {
			t.Errorf("idString(%v) = %q, want %q", in, got, want)
		}
	}
}

// TestHookEmptyTodoWriteClearsPlan: an agent that clears its todo list
// must clear Hive's too. Suppressing the event (the earlier behaviour)
// left the last plan on screen describing work that no longer existed.
func TestHookEmptyTodoWriteClearsPlan(t *testing.T) {
	evs := mapHookPayload([]byte(`{"hook_event_name":"PostToolUse","tool_name":"TodoWrite",
		"tool_use_id":"toolu_1","tool_input":{"todos":[]}}`))
	if len(evs) != 2 {
		t.Fatalf("got %d events, want tool_end + an empty plan", len(evs))
	}
	if evs[1].Kind != wire.AgentEventPlan || len(evs[1].Items) != 0 {
		t.Errorf("plan event = %+v, want an empty wholesale plan", evs[1])
	}

	// But a TodoWrite with no todos array at all carries no plan, and
	// must not wipe one.
	none := mapHookPayload([]byte(`{"hook_event_name":"PostToolUse","tool_name":"TodoWrite",
		"tool_use_id":"toolu_2","tool_input":{}}`))
	if len(none) != 1 {
		t.Errorf("got %d events for a TodoWrite with no todos, want only the tool event", len(none))
	}
}
