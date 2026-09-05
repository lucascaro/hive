# Feature: Session messaging: hand a session a message, get told when it idles

- **GitHub Issue:** —
- **Stage:** PLAN
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Branch:** —
- **PR:** —

## Description

`SEND_TO_SESSION` delivers a note to a running session through the
best channel its agent has (Claude inbox socket, Pi extension inbox,
typed-on-idle), plus a one-shot "notify me when idle" flag.
Spec: `docs/product-specs/338-session-messaging.md`.

## Research

See `docs/exec-plans/active/338-session-messaging.md` → Research.

### Relevant Code
- `internal/registry/registry.go` — state-change funnel from 336
- `internal/session/session.go` — PTY input path
- `internal/agent/pi/hive.ts` — extension from 336
- `internal/notify/` — desktop notification

### Constraints / Dependencies
- Hard dependency on spec 336 (state, `event` mode, Pi extension).
- Open question 1 (Claude inbox message shape) gates phase 3.
- Never read `~/.claude/sessions/*.key`.

## Plan

Four phases — see exec plan.

### Risks
- Claude's inbox protocol is undocumented at the message level; treat
  the captured fixture as version-pinned and fall back to typed.
- Typing into a TUI that is `idle` but mid-redraw; 300 ms grace.

## Implementation Notes

<Filled during IMPLEMENT stage.>

## PR convergence ledger

<Append-only.>

## QA verdict

<Append-only.>
