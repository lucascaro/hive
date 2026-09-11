---
issue: null
title: "Agent-spawned sessions: a granted session can start a sibling"
type: enhancement
complexity: M
priority: P3
stage: PLAN
---

# Agent-spawned sessions: a granted session can start a sibling

- **Issue:** —
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P3
- **Exec plan:** [docs/exec-plans/active/391-agent-spawned-sessions.md](../exec-plans/active/391-agent-spawned-sessions.md)
- **Design:** [docs/design-docs/agent-orchestration.md](../design-docs/agent-orchestration.md)
- **Depends on:** [389](389-orchestrator-grant-and-session-msg.md) and [390](390-session-observe-list-and-wait.md) shipped, gates met.

## Problem

With msg and wait a granted session can coordinate siblings that
already exist. It cannot create one. "Split this into three and run
them in parallel" still means the user opening three launchers.

## Desired behavior

**`hived session new --agent <id> [--name <name>] [--worktree] <prompt…>`**
from inside a granted session starts a sibling in the caller's project
and prints its id. The prompt is delivered the way the idea inbox
delivers an opening prompt: as argv for agents that take one, offered
via the paste bar for the rest.

**What the daemon forces**, regardless of what the client sends:
project = caller's project; `orchestrator = false`; `spawned_by` =
caller; agent from the catalog only (no raw `cmd`); the agent's default
permission mode. A caller at the child cap gets `ERROR{code:
too_many_children}`.

**Visible.** The sidebar row of a spawned session carries a glyph and
a tooltip naming its spawner. Closing an orchestrator that has live
children asks whether to close them too.

**Composable.** `hived session new … && hived wait <id>` is the
expected shape; nothing new is needed for it.

## Success criteria

- `CREATE_SESSION` on a `ModeSession` connection: granted ⇒ created
  with the forced fields; not granted ⇒ `not_orchestrator`; at cap ⇒
  `too_many_children`.
- Daemon test: a client-supplied `cmd`, `project_id`, `orchestrator`,
  or any bypass-permission flag on the create is overridden, not
  honored.
- `Entry.SpawnedBy` persisted and on `SessionInfo`; sidebar glyph
  rendered from it.
- Close-with-children dialog: GUI test for both answers.

## Non-goals

- Children that are themselves orchestrators (user can grant one by
  hand).
- Any notion of a "task" or "result" beyond what `wait` reports.
- Cross-project spawning.
