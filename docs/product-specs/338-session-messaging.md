---
issue: null
title: "Session messaging: hand a session a message, get told when it idles"
type: enhancement
complexity: M
priority: P2
stage: PLAN
---

# Session messaging: hand a session a message, get told when it idles

- **Issue:** —
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/338-session-messaging.md](../exec-plans/active/338-session-messaging.md)
- **Design:** [docs/design-docs/control-plane.md](../design-docs/control-plane.md)

## Problem

Coordinating sessions today means clicking into one, typing, clicking
into the next. Two things are missing. First, a way to hand a running
session a note without leaving the one you are in — "the schema landed
on main, rebase" — that arrives as a proper message the agent can act
on at its next turn boundary, not as keystrokes jammed into whatever
it is rendering. Second, a way to say "tell me when *that* one is done"
and go back to work instead of glancing at it every minute.

## Desired behavior

**Message a session.** From the sidebar context menu or ⇧⌘M on a
focused session, a one-line sheet: "Message *codex-refactor*…". Enter
sends. Delivery depends on the target's tier (spec 336):

- Claude: delivered over Claude Code's own cross-session inbox socket,
  so it arrives as "message from another session", read between tool
  calls or starting a new turn if idle. Never fakes typing.
- Pi: delivered to the Hive extension's per-session inbox; the
  extension injects it with `pi.sendUserMessage`, which queues while
  streaming and triggers a turn when idle.
- Everything else: typed into the PTY followed by Enter, **only when
  the session is `idle`**. If it is `working` the sheet says so and
  offers "queue until idle" (Hive holds it and types it at the next
  `idle` transition) or cancel.

The sender sees a one-line confirmation ("delivered" / "queued") in the
status bar. No message history UI.

**Ping me when idle.** Sidebar context menu / ⌥⌘B on a session toggles
a bell glyph on its row. When that session next transitions from
`working` to `idle`, `exited` or `error`, Hive fires a desktop
notification naming the session and its last summary line, then clears
the toggle. One-shot, per session.

## Success criteria

- `SEND_TO_SESSION{session_id, text, queue}` control frame; response
  `SENT{delivery: socket|extension|typed|queued}` or
  `ERROR{code: session_busy|session_dead|delivery_failed}`.
- Claude path: a Go test with a fake inbox socket asserts the exact
  line Hive writes; manual checklist: message arrives in a real Claude
  session as `› Message from @hive:` and Claude reacts.
- Pi path: extension unit test (node) shows a message written to the
  inbox socket surfaces via `sendUserMessage`; manual checklist against
  real `pi`.
- Typed path: e2e with a shell session shows the text + newline landing
  in the PTY only after `idle`; a `working` session returns
  `session_busy` unless `queue: true`.
- Ping-when-idle: registry test that the notification fires exactly once
  on the first qualifying transition and the flag clears; GUI test that
  the glyph toggles and clears.
- Hive never reads `~/.claude/sessions/*.key` files; a test greps the
  source tree for `.key` under that path pattern and fails on a match.

## Non-goals

- Agent-to-agent routing or Hive as a message broker (Claude already
  has cross-session messaging; Pi sessions can use `hived msg` from Bash
  if they want — that is the same frame, documented, not new work).
- Message history, threads, read receipts.
- Broadcasting to all sessions in a project.
- Windows for the Pi inbox socket in v1: Pi on Windows falls back to
  the typed path.

## Notes

- Claude inbox socket: `messagingSocketPath` in
  `~/.claude/sessions/<pid>.json`, matched by `sessionId ==` Hive's
  session id. Protocol: newline-delimited JSON; on macOS/Linux the auth
  line is optional. Message class: Hive sends no permission class, so a
  Claude session in bypass mode will *hold* it for approval — expected
  and documented in the UI ("held for approval in that session").
  Reference: https://code.claude.com/docs/en/cross-session-messaging
- Depends on spec 336 (`idle` state, `event` mode, Pi extension
  scaffold).
