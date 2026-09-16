package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/wire"
)

// subagentFixture returns the payloads of a captured subagent timeline,
// one per line, in arrival order. Captured from Claude Code 2.1.273 —
// see the exec plan's Phase 2 "Step 0 results".
func subagentFixture(t *testing.T, name string) [][]byte {
	t.Helper()
	var lines [][]byte
	sc := bufio.NewScanner(bytes.NewReader(readFixture(t, "subagent/"+name)))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) > 0 {
			lines = append(lines, append([]byte(nil), sc.Bytes()...))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", name, err)
	}
	return lines
}

// TestHookSubagentToolEventsCarryAgent: a tool call inside a subagent
// carries agent_id / agent_type onto the wire; a main-thread call
// carries neither. Without the tag the daemon cannot keep a subagent
// from overwriting the session's current tool, state and plan tally.
func TestHookSubagentToolEventsCarryAgent(t *testing.T) {
	var sub, main int
	for i, raw := range subagentFixture(t, "parallel-and-background.jsonl") {
		var p map[string]any
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		name, _ := p["hook_event_name"].(string)
		if name != "PreToolUse" && name != "PostToolUse" {
			continue
		}
		wantID, _ := p["agent_id"].(string)
		wantType, _ := p["agent_type"].(string)
		for _, ev := range mapHookPayload(raw) {
			if ev.AgentID != wantID || ev.AgentType != wantType {
				t.Errorf("line %d %s: agent = (%q, %q), want (%q, %q)",
					i+1, ev.Kind, ev.AgentID, ev.AgentType, wantID, wantType)
			}
		}
		if wantID != "" {
			sub++
		} else {
			main++
		}
	}
	if sub == 0 || main == 0 {
		t.Fatalf("fixture exercised %d subagent and %d main-thread tool events; want both", sub, main)
	}
}

// TestHookSubagentStartStop maps the lifecycle hooks to the kinds the
// daemon counts running subagents from.
func TestHookSubagentStartStop(t *testing.T) {
	seen := map[string]bool{}
	for i, raw := range subagentFixture(t, "parallel-and-background.jsonl") {
		var p map[string]any
		_ = json.Unmarshal(raw, &p)
		want := map[string]string{
			"SubagentStart": wire.AgentEventSubagentStart,
			"SubagentStop":  wire.AgentEventSubagentEnd,
		}[p["hook_event_name"].(string)]
		if want == "" {
			continue
		}
		ev := first(t, mapHookPayload(raw))
		if ev.Kind != want {
			t.Errorf("line %d: kind = %q, want %q", i+1, ev.Kind, want)
		}
		if ev.AgentID == "" || ev.AgentID != p["agent_id"] || ev.AgentType != p["agent_type"] {
			t.Errorf("line %d: agent = (%q, %q), want (%v, %v)", i+1, ev.AgentID, ev.AgentType, p["agent_id"], p["agent_type"])
		}
		if ev.RunningAgents != nil {
			t.Errorf("line %d: SubagentStop lists the stopping agent as running; it must not carry running_agents", i+1)
		}
		seen[want] = true
	}
	if !seen[wire.AgentEventSubagentStart] || !seen[wire.AgentEventSubagentEnd] {
		t.Fatalf("fixture did not exercise both kinds: %v", seen)
	}
}

// TestHookStopRunningAgents: Stop's background_tasks becomes the list
// of running subagent ids the daemon reconciles against. An absent key
// must stay nil (nothing to reconcile), an empty list must stay
// non-nil (every tracked subagent has ended).
func TestHookStopRunningAgents(t *testing.T) {
	var stops []wire.AgentEvent
	for _, raw := range subagentFixture(t, "parallel-and-background.jsonl") {
		if bytes.Contains(raw, []byte(`"hook_event_name":"Stop"`)) {
			stops = append(stops, first(t, mapHookPayload(raw)))
		}
	}
	if len(stops) != 4 {
		t.Fatalf("got %d Stop events, want 4", len(stops))
	}
	if stops[0].RunningAgents == nil || len(*stops[0].RunningAgents) != 1 || (*stops[0].RunningAgents)[0] != "a95b1a5907ccf5f00" {
		t.Errorf("first Stop running_agents = %v, want [a95b1a5907ccf5f00]", stops[0].RunningAgents)
	}
	if last := stops[3].RunningAgents; last == nil || len(*last) != 0 {
		t.Errorf("last Stop running_agents = %v, want a non-nil empty list", last)
	}

	legacy := first(t, mapHookPayload(readFixture(t, "stop.json")))
	if legacy.RunningAgents != nil {
		t.Errorf("a Stop without background_tasks must carry no running_agents, got %v", *legacy.RunningAgents)
	}

	mixed := first(t, mapHookPayload([]byte(`{"hook_event_name":"Stop","background_tasks":[
		{"id":"sub1","type":"subagent","status":"running"},
		{"id":"sub2","type":"subagent","status":"completed"},
		{"id":"sh1","type":"shell","status":"running"}]}`)))
	if mixed.RunningAgents == nil || len(*mixed.RunningAgents) != 1 || (*mixed.RunningAgents)[0] != "sub1" {
		t.Errorf("running_agents = %v, want only the running subagent [sub1]", mixed.RunningAgents)
	}
}

// TestHookSubagentPlanEventCarriesAgent: planEvent builds a fresh
// event, so without an explicit copy a subagent's planning call would
// reach the daemon looking like the main thread's.
func TestHookSubagentPlanEventCarriesAgent(t *testing.T) {
	raw := []byte(`{"hook_event_name":"PostToolUse","agent_id":"a1","agent_type":"general-purpose",
		"tool_name":"TaskCreate","tool_use_id":"toolu_1",
		"tool_input":{"subject":"child-task","description":"d"},
		"tool_response":{"task":{"id":"7","subject":"child-task"}}}`)
	evs := mapHookPayload(raw)
	if len(evs) != 2 {
		t.Fatalf("got %d events, want tool_end + plan_item", len(evs))
	}
	for _, ev := range evs {
		if ev.AgentID != "a1" || ev.AgentType != "general-purpose" {
			t.Errorf("%s: agent = (%q, %q), want (a1, general-purpose)", ev.Kind, ev.AgentID, ev.AgentType)
		}
	}
}

// replaySubagentTimeline feeds a captured fixture through mapHookPayload
// into a Machine, calling Apply directly: Registry.ApplyAgentEvent
// clamps future-dated stamps to now, which would collapse the synthetic
// stamps this needs. The registry's own copy of the subagent fields is
// covered by internal/daemon's socket tests.
//
// The fixture keeps arrival order but not receive times, so line n
// (1-based) is stamped base + 10ms×n. stamp lets a pass rewrite that,
// skip drops lines, and check runs after each applied line.
func replaySubagentTimeline(t *testing.T, stamp func(line int) int, skip map[int]bool, check func(line int, m *agentstate.Machine)) {
	t.Helper()
	base := time.Now().Add(-time.Minute)
	m := agentstate.New(base)
	for i, raw := range subagentFixture(t, "parallel-and-background.jsonl") {
		line := i + 1
		if skip[line] {
			continue
		}
		at := base.Add(time.Duration(stamp(line)) * 10 * time.Millisecond)
		for _, ev := range mapHookPayload(raw) {
			m.Apply(agentstate.Event{
				Kind: ev.Kind, Source: ev.Source, At: at, Now: at, Text: ev.Text,
				Tool: ev.Tool, Target: ev.Target, CallID: ev.CallID, OK: ev.OK, Items: ev.Items,
				AgentID: ev.AgentID, AgentType: ev.AgentType, RunningAgents: ev.RunningAgents,
			})
		}
		check(line, m)
	}
}

// TestFixtureTimelineParallelAndBackground replays a real session: two
// parallel subagents, the parent's Stop firing while one still runs, then
// a background subagent that outlives the parent's turn.
func TestFixtureTimelineParallelAndBackground(t *testing.T) {
	wantCount := map[int]int{10: 1, 14: 2, 17: 1, 24: 0, 26: 1, 37: 0, 39: 0}
	identity := func(line int) int { return line }
	replaySubagentTimeline(t, identity, nil, func(line int, m *agentstate.Machine) {
		snap := m.Snapshot()
		if want, ok := wantCount[line]; ok && snap.SubagentsRunning != want {
			t.Errorf("after line %d: SubagentsRunning = %d, want %d", line, snap.SubagentsRunning, want)
		}
		switch line {
		case 23:
			// Subagent B's Bash (lines 20-23) ran after the parent's Stops.
			if snap.State != wire.StateWaitingInput {
				t.Errorf("after line 23: state = %q, want waiting_input: subagent B's tools reopened the parent's finished turn", snap.State)
			}
			if snap.CurrentTool != "" {
				t.Errorf("after line 23: CurrentTool = %q, want empty", snap.CurrentTool)
			}
		case 39:
			if snap.State != wire.StateExited {
				t.Errorf("after SessionEnd: state = %q, want exited", snap.State)
			}
		}
	})

	// Delivery inversion: subagent A's SubagentStop (17) carries a stamp
	// later than the parent's Stop (18) and is applied first. Line 19,
	// the second Stop, is dropped so only the inverted one can end the
	// turn. If a subagent event advanced the ordering guard, that Stop
	// would be discarded as out of order and the session left working.
	swapped := func(line int) int {
		switch line {
		case 17:
			return 18
		case 18:
			return 17
		}
		return line
	}
	replaySubagentTimeline(t, swapped, map[int]bool{19: true}, func(line int, m *agentstate.Machine) {
		if line == 18 {
			if got := m.Snapshot().State; got != wire.StateWaitingInput {
				t.Errorf("inverted pass, after line 18: state = %q, want waiting_input: the parent's Stop was dropped", got)
			}
		}
	})
}
