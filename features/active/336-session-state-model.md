# Feature: Session state model: know what every agent is doing

- **GitHub Issue:** —
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P1
- **Branch:** —
- **PR:** —

## Description

Every session gets a daemon-owned state (`working` / `idle` /
`waiting_input` / `waiting_permission` / `exited` / `error`) plus
"what it was asked" and "what it last said", from PTY heuristics for
all agents, Claude hooks and a Pi extension for the two agents that
cooperate. Spec: `docs/product-specs/336-session-state-model.md`.
Design: `docs/design-docs/control-plane.md`.

## Research

See the exec plan's Research section:
`docs/exec-plans/active/336-session-state-model.md`.

### Relevant Code
- `internal/registry/registry.go` — `Entry`, `noteBell`, `SetAttention`, `broadcastLocked`
- `internal/session/session.go` — `Options.Env`, `noteBell`, `SetBellHook`
- `internal/registry/create.go` — the one `spawn(...)` call, `SessionIDFlag` append
- `internal/daemon/daemon.go` — HELLO mode switch
- `internal/agent/agent.go`, `claude.go` — `Def`, adapters
- `cmd/hivegui/frontend/src/app/state.ts`, `store/store.ts`, `components/SessionRow.tsx`, `TileChrome.tsx`

### Constraints / Dependencies
- Open question 1 in the exec plan (Claude `--settings` hooks merge) blocks phase 2.
- `DaemonContract` bump ⇒ Restart Daemon on update, not GUI reload.

## Plan

Four phases, one PR each — see exec plan "Phasing". Files, tests and
risks are enumerated there.

### Risks
- Heuristic thresholds (2 s quiet, 30 s hook-stale) are guesses; make
  them package constants and tune from real use before widening.
- Per-chunk output hook must be O(1) and never broadcast per chunk.
- `hived hook` runs on Claude's turn boundary; a slow dial would slow
  Claude. Hard 2 s timeout, always exit 0.

## Implementation Notes

<Filled during IMPLEMENT stage.>

## PR convergence ledger

<Append-only. One entry per `/ralph-loop` iteration.>

## QA verdict

<Filled by `/feature-qa` after PR merges. Append-only.>
