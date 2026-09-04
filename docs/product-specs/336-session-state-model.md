---
issue: null
title: "Session state model: know what every agent is doing"
type: enhancement
complexity: L
priority: P1
stage: IMPLEMENT
---

# Session state model: know what every agent is doing

- **Issue:** —
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/active/336-session-state-model.md](../exec-plans/active/336-session-state-model.md)
- **Design:** [docs/design-docs/control-plane.md](../design-docs/control-plane.md)

## Problem

With ten or more agent sessions across several projects, the user loses
track of who is working, who is blocked on a permission prompt, who
finished an hour ago, and what each one was asked to do. Hive today
knows only "the program rang the bell" (`NeedsAttention`), which
conflates "done", "needs a yes/no", and "a build failed". Everything
else requires clicking into every tile and reading. The other tools in
this space solve this for one agent CLI at a time; Hive runs six.

## Desired behavior

Every session in the sidebar, grid tile header, and menu bar shows a
**state** at a glance:

| State | Meaning | Glyph (sidebar / tile) |
|-------|---------|------------------------|
| `working` | the agent is mid-turn (streaming, running tools) | animated dot |
| `idle` | nothing running, nothing pending; the agent is waiting for the *next* prompt | hollow dot |
| `waiting_input` | the agent asked the user a question and stopped (Claude `Notification(idle_prompt)`, bell on a heuristic session) | filled dot, pulse |
| `waiting_permission` | the agent is blocked on a tool-permission prompt | filled dot, pulse, distinct colour |
| `exited` | the child process ended | hollow grey dot |
| `error` | the last turn ended in an API/CLI error | red dot |

Hovering a session (sidebar tooltip, tile header) shows **what it was
asked** (the first user prompt of the conversation, truncated) and
**what it last said** (the last assistant message, truncated), when the
agent exposes them. A session also shows which tier produced its state
(`hook` / `extension` / `heuristic`) so a plain hollow dot on a Codex
session reads as "Hive can't tell" rather than "idle for sure".

The existing attention machinery generalises: `NeedsAttention` becomes
"state is `waiting_*` or the session finished while unfocused", the
⌘B / ⇧⌘B jump cycles through those, the desktop notification fires on
the transition into a `waiting_*` state and says which kind, and
`hivebar` shows the count of sessions waiting on the user.

None of this requires the user to configure anything in Claude or Pi.
Hive injects what it needs at spawn time and leaves the user's own
settings untouched.

## Success criteria

- `wire.SessionInfo` carries `state`, `state_source`, `last_prompt`,
  `last_summary`; a `SESSION_EVENT(state)` is broadcast on every
  transition. `PROTOCOL_VERSION` is unchanged; `DaemonContract` is bumped.
- A Claude session launched from Hive reports `working` within one
  second of the user pressing Enter, `waiting_permission` when a tool
  permission prompt appears, `waiting_input` when Claude stops with a
  question, `idle` after a turn ends, `error` after an API failure —
  verified by a Go integration test driving `hived hook` with recorded
  hook payloads, and by a manual checklist against a real `claude`.
- A Pi session launched from Hive reports `working` / `idle` /
  `waiting_input` through the shipped extension; `pi --help` still works
  from a Hive shell (the extension must not be globally installed).
- A shell / Codex / Gemini session reports `working` while output
  flows, `idle` after two seconds of silence, `waiting_input` on a bell,
  `exited` on exit — verified by a Go test feeding a fake PTY.
- A session whose hook stops firing (simulated by killing the event
  connection path) falls back to heuristic state within the quiet
  threshold; `state_source` flips to `heuristic`.
- Sidebar rows and tile headers render the six states; the tooltip
  shows prompt + summary when present. Playwright mock e2e covers the
  glyphs; unit tests cover the store reducers.
- `hivebar` shows "N waiting on you" using the new state, not the bell.
- A daemon restart starts every revived session at `idle` /
  `heuristic` with empty prompt/summary, and no persisted file grew a
  new field.

## Non-goals

- Task ledgers, dependencies between sessions, fan-out/fan-in
  (future orchestration layer).
- Sending anything *to* a session — spec 338.
- Hook-tier parity for Codex, Gemini, Copilot, Aider, or custom agents.
- Persisting state across daemon restart.
- Per-tool detail (which tool is running, token counts). Only the
  states above and the two text fields.
- Replacing the terminal bell. The bell stays as a heuristic input and
  as the notification for shells.

## Notes

- Claude hooks reference: https://code.claude.com/docs/en/hooks —
  events used: `SessionStart`, `UserPromptSubmit`, `Stop`, `StopFailure`,
  `Notification` (types `permission_prompt`, `idle_prompt`),
  `PermissionRequest`, `SessionEnd`. `--settings '<json>'` merges into the
  user's settings for that session only; verify at implementation that
  the `hooks` key concatenates rather than replaces a user's own hooks
  (the docs do not say). If it replaces, Hive must read
  `~/.claude/settings.json` hooks and re-emit them alongside its own.
- Pi extension reference: `docs/extensions.md` in the installed
  `@earendil-works/pi-coding-agent` package — events `session_start`,
  `input`, `agent_start`, `agent_settled`, `session_shutdown`;
  `ctx.isIdle()`; loaded via `pi -e <path>`.
- Prior art inside Hive: `NeedsAttention` (bell → registry → wire →
  sidebar pulse, spec 240), `Title` (OSC → in-memory → wire).
