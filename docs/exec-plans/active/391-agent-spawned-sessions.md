# Agent-spawned sessions: a granted session can start a sibling

- **Spec:** [docs/product-specs/391-agent-spawned-sessions.md](../../product-specs/391-agent-spawned-sessions.md)
- **Design:** [docs/design-docs/agent-orchestration.md](../../design-docs/agent-orchestration.md)
- **Issue:** —
- **Branch:** `feature/391-agent-spawned-sessions`
- **PR:** —
- **Status:** active

## Summary

Let a granted `ModeSession` caller send `CREATE_SESSION`, with the
daemon forcing project, `orchestrator=false`, `spawned_by`, catalog
agent only and default permissions; cap live children; show the
spawner in the sidebar; offer to close children with the parent.
**Do not start until 389 and 390 have shipped and met their gates.**
This plan is deliberately thin: what an agent actually needs from
spawn is unknown until phases 1–2 have been used. Fill the gaps from
that evidence, not from this file.

## Research

- `internal/wire/control.go:40` `CreateSpec` — fields the daemon must
  override for restricted callers: `ProjectID`, `Cmd`, `Orchestrator`,
  and anything that alters the agent's permission mode (check
  `internal/agent/` `Def`/`SpawnArgs` for how bypass or extra args
  are expressed today — there may be none, which is the easy case).
- `internal/registry/create.go` — `deliveryFor` / `pendingPrompt` /
  `InitialPrompt` — the opening-prompt path the idea inbox uses; reuse
  it unchanged.
- `internal/daemon/daemon.go` — the `CREATE_SESSION` arm and
  `sessionModeFrames`; the 389 grant check is the model.
- `internal/registry/registry.go` `Entry`, `persist.go` `MetaFile` —
  `SpawnedBy`.
- `cmd/hivegui/frontend/src/components/SessionRow.tsx` (glyph),
  `components/modals/ChoiceDialog.tsx` (close-with-children), the
  kill-session path in `app/` and `store/`.
- `cmd/hived/idea.go`, 389's `msg.go` — client shape for `session new`.

## Approach

- Wire: `CreateSpec.SpawnedBy` is **not** a client field; the daemon
  sets `Entry.SpawnedBy` from `ownSessionID`. `SessionInfo.SpawnedBy
  string`. `ErrCodeTooManyChildren = "too_many_children"`.
  `DaemonContract++`.
- Daemon: `sessionModeFrames[FrameCreateSession] = true`; on a
  restricted connection require the grant (389's check), count live
  entries with `SpawnedBy == ownSessionID` against a constant
  (`maxChildrenPerOrchestrator = 4` — a constant, bump when someone
  needs more), then rewrite the spec: `ProjectID = ownProjectID`,
  `Orchestrator = false`, `Cmd = nil`, `Agent` must be a catalog id.
- Registry: `SpawnedBy` persisted; on `Close`/kill of an entry, the
  GUI (not the daemon) asks about live children and closes them by
  id — no cascade logic in the daemon.
- `hived session new --agent <id> [--name] [--worktree] <prompt…>`
  prints the new id. Since `CREATE_SESSION` is fire-and-forget, read
  the id off the `SESSION_EVENT(added)` fan-out the way `hived idea
  add` reads `IDEA_EVENT(added)`.
- GUI: glyph + tooltip from `spawned_by`; close-with-children
  `ChoiceDialog`.

### Files to change

- `internal/wire/control.go`, `internal/buildinfo/contract.go`.
- `internal/registry/registry.go`, `persist.go`.
- `internal/daemon/daemon.go` (create arm, allowlist, cap).
- `cmd/hived/main.go` and `session.go` (from 390) — `new` verb.
- Frontend `SessionRow.tsx`, kill path, `store.ts`, `bridge.ts`;
  `cmd/hivegui/app_calls.go` if the close-with-children needs a
  binding.

### Tests

- `internal/daemon`: restricted create without grant refused; forced
  fields override client values (table test over `ProjectID`, `Cmd`,
  `Orchestrator`); cap enforced and freed when a child exits.
- `internal/registry`: `SpawnedBy` round-trips through `MetaFile`.
- Frontend: glyph; both answers of the close-with-children dialog.

## Decision log

- **2026-09-10** — Cascade close is a GUI question, not daemon
  behaviour. Why: the daemon never closes a session the user did not
  name; a headless orchestrator's children outliving it is the
  conservative default.

## Progress

- **2026-09-10** — Plan written. Gated on 389 and 390.

## Open questions

- Whether any current agent `Def` exposes a permission-bypass or
  extra-args surface a restricted create could reach. Determines
  whether "force defaults" is a no-op or real code.
- Whether `--worktree` should be allowed from a session at all in v1.
  Default: allow, since worktrees are the natural unit for parallel
  work; drop if it complicates the cap or the close dialog.
- The child cap number.
