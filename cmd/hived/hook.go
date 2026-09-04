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
	"time"

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
	ev := mapHookPayload(raw)
	ev.SessionID = sessionID

	if err := sendHookEvent(sock, ev); err != nil {
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

// mapHookPayload turns Claude's hook JSON into the AgentEvent to
// report. Malformed/empty stdin, or a payload missing hook_event_name,
// maps to KindPing — same tolerant-parsing rule as an unknown event
// name: it keeps the hook tier alive (refreshes the machine's
// staleness clock) without changing state, rather than dropping the
// session back to the heuristic tier over a hook Hive doesn't
// recognise yet.
func mapHookPayload(raw []byte) wire.AgentEvent {
	ev := wire.AgentEvent{Source: wire.StateSourceHook, At: time.Now().UTC().Format(time.RFC3339Nano)}

	var p hookPayload
	if len(raw) == 0 || json.Unmarshal(raw, &p) != nil {
		ev.Kind = wire.AgentEventPing
		return ev
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
		ev.Kind = wire.AgentEventWaitingPermission
	case "PostToolUse":
		// A tool ran, so whatever the agent was waiting on just got
		// answered. Cheap and exact — no need to track which specific
		// permission was outstanding.
		ev.Kind = wire.AgentEventPermissionResolved
	case "SessionEnd":
		ev.Kind = wire.AgentEventSessionEnd
	case "SessionStart":
		// No state change; only promotes the session to the hook tier
		// (stamps hookSeenAt) before its first real event.
		ev.Kind = wire.AgentEventPing
	default:
		ev.Kind = wire.AgentEventPing
	}
	return ev
}

// firstString returns the first key present in p whose value is a
// non-empty string.
func firstString(p hookPayload, keys ...string) string {
	for _, k := range keys {
		if s, ok := p[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// sendHookEvent dials sock, speaks HELLO{mode:event} + AGENT_EVENT, and
// closes without waiting for a reply — the daemon sends none in
// ModeEvent.
func sendHookEvent(sock string, ev wire.AgentEvent) error {
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
	return wire.WriteJSON(conn, wire.FrameAgentEvent, ev)
}
