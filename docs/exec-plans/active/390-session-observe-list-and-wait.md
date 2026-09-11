# Observe from a session: `hived session list` and `hived wait`

- **Spec:** [docs/product-specs/390-session-observe-list-and-wait.md](../../product-specs/390-session-observe-list-and-wait.md)
- **Design:** [docs/design-docs/agent-orchestration.md](../../design-docs/agent-orchestration.md)
- **Issue:** —
- **Branch:** `feature/390-session-observe`
- **PR:** —
- **Status:** active

## Summary

Let any `ModeSession` connection list its project's sessions with a
narrow field set, and add `hived wait` that blocks until a sibling
reaches a state. Read-only; no grant. **Do not start until 389's gate
is met** (or 389's decision log records folding this in). Research
below is a starting point, not a finished survey: verify against
`main` and fill the gaps.

## Research

- `internal/daemon/daemon.go` — `ownSessionOnly` narrows
  `LIST_SESSIONS` for restricted callers to the caller's own id +
  project, with the comment explaining which fields are withheld and
  why. This plan widens *which sessions*, not *which fields* beyond
  `agent`, `alive`, `state`, `state_source`.
- `internal/agentstate/machine.go` — states; `wire.State*` constants
  (`control.go:~253`). `waiting_input` / `waiting_permission` are the
  states that never time out — `wait --state idle` on a session stuck
  at a permission prompt waits forever, which is correct and should be
  visible in the help text.
- How a restricted connection receives `SESSION_EVENT` broadcasts —
  `serveControl` subscribes every connection before `WELCOME`; check
  whether restricted ones are filtered. If events already flow,
  `hived wait` is a subscribe-and-filter loop. If not, either filter
  broadcasts to `ownProjectID` for restricted callers (preferred — it
  is the same scoping the list gets) or poll `LIST_SESSIONS` at 1 s.
- `cmd/hived/idea.go` — client shape to copy for `session.go` and
  `wait.go`.

## Approach

- Daemon: on a restricted connection `LIST_SESSIONS` returns every
  session with `ProjectID == ownProjectID`, projected to `{id, name,
  agent, alive, state, state_source}`. Replace `ownSessionOnly` with
  `ownProjectOnly`; keep `hived idea list` working (it reads its
  project id from the first entry).
- Daemon: restricted connections receive `SESSION_EVENT` only for
  sessions in `ownProjectID`, with the same projection.
- `hived session list` — prints one line per session; `hived wait
  <session> [--state a,b] [--timeout d]` — resolves the target the way
  389's `msg` does, then blocks on events (or polls; see research).
  Exit 0 on state, 1 on timeout, 2 on bad args / not in a session.
- One log line per wait completion.
- No wire frame is added if events already reach restricted
  connections. If a `WAIT_SESSION` frame turns out cleaner, that is
  the implementor's call; record it.

### Files to change

- `internal/daemon/daemon.go` — projection, event filter.
- `cmd/hived/main.go` — `session`, `wait` subcommands.

### New files

- `cmd/hived/session.go`, `cmd/hived/wait.go` and tests.

### Tests

- `internal/daemon`: restricted list is project-scoped and projected;
  restricted connection sees no events from other projects.
- `cmd/hived`: `wait` returns on first qualifying `AGENT_EVENT`-driven
  transition; returns immediately when already satisfied; times out.

## Decision log

- **2026-09-10** — No grant for read-only verbs. Why: same-user
  processes can already read this from the control socket; a grant
  would add friction and no safety.

## Progress

- **2026-09-10** — Plan written. Gated on 389.

## Open questions

- Events vs polling for `wait` (see research).
- Whether `last_summary` belongs in the projection. Left out until an
  implementor or the phase-3 gate shows an agent needing it.
