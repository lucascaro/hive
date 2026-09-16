// hived hook is the Claude Code hook-tier client: Claude invokes it as
// `<hivedPath> hook` for every hook event Hive wires (see
// internal/agent/claude.go's claudeSpawnArgs), and it reports one
// AgentEvent to the daemon over a ModeEvent connection before exiting.
//
// This file is deliberately paranoid about never surfacing anything to
// Claude: no stdout output (Claude parses hook stdout for some event
// types), and it always exits 0 — a user running `claude` outside Hive
// with a copied --settings file, or the daemon being down, must look
// exactly like no hook ran at all.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/wire"
)

// hookDialTimeout / hookWriteDeadline bound the one thing that could
// otherwise hang: a wedged or gone daemon. The happy path is well
// under 100ms; these are the hard ceiling, not the expected latency.
const (
	hookDialTimeout   = 2 * time.Second
	hookWriteDeadline = 2 * time.Second
)

// runHook implements `hived hook`. Always returns normally; the caller
// (main) exits 0 unconditionally afterward.
func runHook(stdin io.Reader) {
	// Recover rather than let a bug here ever reach Claude as a nonzero
	// exit or stderr noise beyond the opt-in debug log — this command
	// runs on every keystroke-adjacent hook event of every Hive Claude
	// session, and "the hook crashed" must never be how a user finds
	// that out.
	defer func() {
		if r := recover(); r != nil {
			hookDebugf("panic: %v", r)
		}
	}()

	sessionID := os.Getenv("HIVE_SESSION_ID")
	sock := os.Getenv("HIVE_SOCKET")
	if sessionID == "" || sock == "" {
		// Not running under Hive (or a copied settings file outside
		// it): silently inert, by design.
		return
	}

	raw, err := io.ReadAll(stdin)
	if err != nil {
		hookDebugf("read stdin: %v", err)
		raw = nil
	}
	evs := mapHookPayload(raw)
	for i := range evs {
		evs[i].SessionID = sessionID
	}

	if err := sendHookEvents(sock, evs); err != nil {
		hookDebugf("send: %v", err)
	}
}

// hookDebugf logs to stderr only when HIVE_HOOK_DEBUG=1 — never
// otherwise, since Claude's hook runner treats hook stderr as
// diagnostic noise a normal user should never see.
func hookDebugf(format string, args ...any) {
	if os.Getenv("HIVE_HOOK_DEBUG") != "1" {
		return
	}
	fmt.Fprintf(os.Stderr, "hived hook: "+format+"\n", args...)
}

// hookPayload is Claude's hook input JSON, decoded loosely: the exact
// field set differs per event and Claude Code's own schema has moved
// under us before (see the plan's decision log on hooks churn), so we
// read what we need out of a generic map rather than a fixed struct —
// an unrecognised or missing field degrades to "no text", never to a
// parse error.
type hookPayload map[string]any

// mapHookPayload turns Claude's hook JSON into the AgentEvents to
// report. Malformed/empty stdin, or a payload missing hook_event_name,
// maps to KindPing — same tolerant-parsing rule as an unknown event
// name: it keeps the hook tier alive (refreshes the machine's
// staleness clock) without changing state, rather than dropping the
// session back to the heuristic tier over a hook Hive doesn't
// recognise yet.
//
// It returns a SLICE because one payload can be two observations: a
// TodoWrite PostToolUse is both "a tool finished" and "here is the
// agent's whole plan". Every other payload yields exactly one event.
func mapHookPayload(raw []byte) []wire.AgentEvent {
	ev := wire.AgentEvent{Source: wire.StateSourceHook, At: time.Now().UTC().Format(time.RFC3339Nano)}

	var p hookPayload
	if len(raw) == 0 || json.Unmarshal(raw, &p) != nil {
		ev.Kind = wire.AgentEventPing
		return []wire.AgentEvent{ev}
	}
	name, _ := p["hook_event_name"].(string)
	switch name {
	case "UserPromptSubmit":
		ev.Kind = wire.AgentEventPrompt
		// Field name per the published schema is "prompt"; the extra
		// candidates are a hedge against the field being renamed
		// between Claude Code releases (this integration has already
		// been rewritten once for that reason — see the plan).
		ev.Text = firstString(p, "prompt", "user_message", "message")
	case "Stop":
		ev.Kind = wire.AgentEventTurnEnd
		ev.Text = firstString(p, "last_assistant_message")
	case "StopFailure":
		ev.Kind = wire.AgentEventError
		ev.Text = firstString(p, "error_type", "error", "reason")
	case "Notification":
		switch nt, _ := p["notification_type"].(string); nt {
		case "permission_prompt":
			ev.Kind = wire.AgentEventWaitingPermission
		case "idle_prompt":
			ev.Kind = wire.AgentEventWaitingInput
		default:
			// Every other notification_type (auth_success,
			// elicitation_*, agent_needs_input, quota_auto_resume_*,
			// ...) says nothing about working/waiting, so it's a ping:
			// alive, no state change.
			ev.Kind = wire.AgentEventPing
		}
	case "PermissionRequest":
		// The question tool is not a permission: the agent wants an
		// answer, not a yes. Rendered as waiting_input so the glyph says
		// what the user will find.
		if firstString(p, "tool_name") == "AskUserQuestion" {
			ev.Kind = wire.AgentEventWaitingInput
		} else {
			ev.Kind = wire.AgentEventWaitingPermission
		}
	case "PreToolUse", "PostToolUse", "PostToolUseFailure":
		// A tool is starting, ran, or failed: whatever the agent was
		// waiting on has been answered. PreToolUse is the one that
		// matters for latency — a tool that runs for a minute is
		// "working", not "waiting for you" — and the other two cover a
		// build of Claude Code that does not fire it.
		//
		// These three used to collapse into one permission_resolved,
		// dropping tool_name and tool_input on the floor. They now
		// carry the tool through — see deriveToolTarget for why the
		// arguments themselves never leave this process — while
		// keeping exactly the working-state effect the collapsed form
		// had, so the split changes no glyph.
		return toolEvents(ev, p, name)
	case "SessionEnd":
		ev.Kind = wire.AgentEventSessionEnd
	case "SessionStart":
		// No state change; only promotes the session to the hook tier
		// (stamps hookSeenAt) before its first real event.
		ev.Kind = wire.AgentEventPing
	default:
		ev.Kind = wire.AgentEventPing
	}
	return []wire.AgentEvent{ev}
}

// toolEvents builds the events for one Pre/Post tool hook.
//
// base carries Source and At, already stamped by mapHookPayload.
func toolEvents(base wire.AgentEvent, p hookPayload, name string) []wire.AgentEvent {
	base.Tool = firstString(p, "tool_name")
	base.Target = deriveToolTarget(p["tool_input"])
	// tool_use_id is Claude's own identifier for the call, present on
	// both PreToolUse and PostToolUse. It is what lets the daemon pair
	// the two ends and time the call. Pairing by tool_name is NOT a
	// fallback — Claude runs tools in parallel, and two concurrent
	// Bash calls are indistinguishable by name — so when this is
	// absent the event still reports, it simply never pairs.
	base.CallID = firstString(p, "tool_use_id")

	if name == "PreToolUse" {
		base.Kind = wire.AgentEventToolStart
		return []wire.AgentEvent{base}
	}

	base.Kind = wire.AgentEventToolEnd
	ok := name != "PostToolUseFailure"
	base.OK = &ok

	// A failed planning call did not change the agent's list, so it
	// must not change Hive's. It still reports as a tool call.
	if !ok {
		return []wire.AgentEvent{base}
	}
	if plan, found := planEvent(base, p); found {
		return []wire.AgentEvent{base, plan}
	}
	return []wire.AgentEvent{base}
}

// planEvent derives the plan update carried by a successful planning
// tool call, if base.Tool is one. The call is still a tool call and is
// reported as one; the plan rides alongside it.
//
// Every shape here was captured from a live Claude Code session
// (2.1.273), not taken from docs — the planning tools changed under
// this feature once already:
//
//   - TaskCreate  input {subject, description}. The task's ID is
//     assigned by Claude and appears ONLY in the PostToolUse
//     tool_response.task.id, which is why this runs on Post alone.
//   - TaskUpdate  input {taskId, status?, subject?} — only the fields
//     that changed. status "deleted" removes the task.
//   - TaskList    tool_response.tasks is the COMPLETE list, so it is
//     sent as a wholesale plan: a free resync that heals any update the
//     daemon missed.
//   - TodoWrite   input.todos is the whole list, for configurations
//     that still use it (CLAUDE_CODE_ENABLE_TASKS=0 on older models).
func planEvent(base wire.AgentEvent, p hookPayload) (wire.AgentEvent, bool) {
	ev := wire.AgentEvent{Source: base.Source, At: base.At}
	input, _ := p["tool_input"].(map[string]any)
	response, _ := p["tool_response"].(map[string]any)

	switch base.Tool {
	case "TodoWrite":
		ev.Kind = wire.AgentEventPlan
		ev.Items = derivePlan(p["tool_input"])
		return ev, len(ev.Items) > 0

	case "TaskList":
		ev.Kind = wire.AgentEventPlan
		ev.Items = deriveTaskList(response)
		// An empty list is a real answer — every task was completed and
		// cleared, or none were made — so it is sent, unlike TodoWrite.
		return ev, response != nil && response["tasks"] != nil

	case "TaskCreate":
		task, _ := response["task"].(map[string]any)
		id := idString(task["id"])
		if id == "" {
			// Without the ID nothing can ever update this step, and a
			// step that can never complete is worse than no step.
			return ev, false
		}
		text := capPlanText(stringField(input, "subject"))
		if text == "" {
			text = capPlanText(stringField(task, "subject"))
		}
		ev.Kind = wire.AgentEventPlanItem
		ev.Items = []wire.PlanItem{{ID: id, Text: text, Status: wire.PlanStatusPending}}
		return ev, true

	case "TaskUpdate":
		id := idString(input["taskId"])
		if id == "" {
			id = idString(response["taskId"])
		}
		if id == "" {
			return ev, false
		}
		item := wire.PlanItem{ID: id, Text: capPlanText(stringField(input, "subject"))}
		if raw, present := input["status"].(string); present {
			item.Status = taskStatus(raw)
		}
		// An update that touched neither the text nor the status — a
		// description edit, a dependency change — moves nothing the
		// plan shows.
		if item.Text == "" && item.Status == "" {
			return ev, false
		}
		ev.Kind = wire.AgentEventPlanItem
		ev.Items = []wire.PlanItem{item}
		return ev, true
	}
	return ev, false
}

// deriveTaskList reads a TaskList response into a whole plan.
func deriveTaskList(response map[string]any) []wire.PlanItem {
	raw, _ := response["tasks"].([]any)
	items := make([]wire.PlanItem, 0, min(len(raw), wire.MaxPlanItems))
	for _, r := range raw {
		if len(items) == wire.MaxPlanItems {
			break
		}
		t, ok := r.(map[string]any)
		if !ok {
			continue
		}
		id := idString(t["id"])
		status := taskStatus(stringField(t, "status"))
		if id == "" || status == wire.PlanStatusDeleted {
			continue
		}
		if status == "" {
			status = wire.PlanStatusPending
		}
		items = append(items, wire.PlanItem{
			ID:     id,
			Text:   capPlanText(stringField(t, "subject")),
			Status: status,
		})
	}
	return items
}

// taskStatus maps Claude's task statuses onto the wire vocabulary. The
// empty string means "no status given", which a TaskUpdate needs to
// tell apart from pending. An unrecognised value becomes pending, the
// same coercion the daemon applies: mislabelled beats missing.
func taskStatus(s string) string {
	switch s {
	case "":
		return ""
	case "in_progress":
		return wire.PlanStatusActive
	case "completed":
		return wire.PlanStatusDone
	case "deleted":
		return wire.PlanStatusDeleted
	default:
		return wire.PlanStatusPending
	}
}

// idString reads a task ID. Claude sends a string ("1"), but a JSON
// number is accepted too, since that is the obvious way for the field
// to change shape.
func idString(v any) string {
	switch id := v.(type) {
	case string:
		return id
	case float64:
		return strconv.FormatFloat(id, 'f', -1, 64)
	}
	return ""
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func capPlanText(s string) string {
	if len(s) > wire.MaxPlanTextLen {
		s = strings.ToValidUTF8(s[:wire.MaxPlanTextLen], "")
	}
	return s
}

// derivePlan reads TodoWrite's todo list.
//
// TodoWrite's payload shape is Claude-internal and undocumented, so
// this reads defensively: the step text comes from "content" with
// "activeForm" as a fallback, and an unrecognised status is coerced to
// pending rather than dropping the step. A plan that renders one step
// with the wrong colour is better than a plan missing a step.
//
// Unlike tool arguments, the todo text is the whole point of the
// feature and is meant to be shown — it is the agent's own summary of
// what it set out to do, not a command line.
func derivePlan(input any) []wire.PlanItem {
	m, ok := input.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := m["todos"].([]any)
	if !ok {
		return nil
	}
	if len(raw) > wire.MaxPlanItems {
		raw = raw[:wire.MaxPlanItems]
	}
	items := make([]wire.PlanItem, 0, len(raw))
	for _, r := range raw {
		t, ok := r.(map[string]any)
		if !ok {
			continue
		}
		text, _ := t["content"].(string)
		if text == "" {
			text, _ = t["activeForm"].(string)
		}
		if text == "" {
			continue
		}
		status, _ := t["status"].(string)
		items = append(items, wire.PlanItem{Text: capPlanText(text), Status: planStatus(status)})
	}
	return items
}

// planStatus maps Claude's todo statuses onto the wire vocabulary.
func planStatus(s string) string {
	switch s {
	case "in_progress":
		return wire.PlanStatusActive
	case "completed":
		return wire.PlanStatusDone
	default:
		// "pending", and anything unrecognised.
		return wire.PlanStatusPending
	}
}

// firstString returns the first key present in p whose value is a
// non-empty string.
// The daemon caps Text again on receipt, but capping here is what
// keeps the frame under wire.MaxPayload: a prompt with a pasted file
// in it can run to megabytes, and an oversized frame is refused whole,
// losing the Kind — the part that actually moves the state — not just
// the text. Cheaper to send 512 bytes than to drop the event.
func firstString(p hookPayload, keys ...string) string {
	for _, k := range keys {
		if s, ok := p[k].(string); ok && s != "" {
			if len(s) > wire.MaxSummaryLen {
				s = strings.ToValidUTF8(s[:wire.MaxSummaryLen], "")
			}
			return s
		}
	}
	return ""
}

// sendHookEvents dials sock, speaks HELLO{mode:event} + one
// AGENT_EVENT per event, and closes without waiting for a reply — the
// daemon sends none in ModeEvent.
//
// All events share ONE connection. A TodoWrite reports two, and
// dialing twice would double the handshake cost on the hottest path
// the daemon has, for no benefit: the daemon reads frames in order off
// the same connection.
func sendHookEvents(sock string, evs []wire.AgentEvent) error {
	if len(evs) == 0 {
		return nil
	}
	// The hook reports what the user is doing. Check the socket
	// directory before handing that to whatever is listening: the path
	// arrives in an inherited environment variable.
	if err := daemon.CheckSocketDir(sock); err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", sock, hookDialTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetWriteDeadline(time.Now().Add(hookWriteDeadline)); err != nil {
		return err
	}
	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION,
		Client:  "hived-hook",
		Mode:    wire.ModeEvent,
	}); err != nil {
		return err
	}
	for _, ev := range evs {
		if err := wire.WriteJSON(conn, wire.FrameAgentEvent, ev); err != nil {
			return err
		}
	}
	return nil
}
