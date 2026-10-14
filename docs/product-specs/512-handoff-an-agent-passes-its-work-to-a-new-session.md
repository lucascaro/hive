---
issue: 512
title: "Handoff: an agent passes its work to a new session"
type: enhancement
complexity: L
priority: P3
stage: RESEARCH
---

# Handoff: an agent passes its work to a new session

- **Issue:** #512
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P3
- **Depends on:** [391 — agent-spawned sessions](391-agent-spawned-sessions.md) (and transitively [389](389-orchestrator-grant-and-session-msg.md), [390](390-session-observe-list-and-wait.md))
- **Design:** [docs/design-docs/agent-orchestration.md](../design-docs/agent-orchestration.md)

## Problem

A granted agent that is running low on context, hits a task better suited to another agent (Claude to pi, say), or wants to isolate follow-up work can only start a cold sibling with a prompt (391). It cannot carry its context, choose whether the successor shares its checkout, or retire itself. So the user relays by hand: copy the summary, open a launcher, pick a worktree, close the old session.

## Desired behavior

`hived session handoff --agent <id> [--name <n>] --worktree same|new --mode fresh|context|takeover <prompt…>`, run from a granted session. It creates a child session (391's forced fields apply) and prints its id.

- **Worktree:** `new` creates a worktree branched from `origin/main`. `same` runs the child in the caller's checkout. `same` is refused if the checkout has uncommitted changes and `--mode` is not `takeover`.
- **Mode `fresh`:** the child gets the prompt only. The source keeps running.
- **Mode `context`:** the child also gets a brief (from stdin or `--brief-file`) labelled as written by session "<name>". The source keeps running.
- **Mode `takeover`:** like `context`, but once the child confirms it started, the source is closed. The child runs `--worktree same` by default. The child does not inherit the orchestrator grant; the user can grant it by hand.
- **Visible:** the child's sidebar row shows a handoff glyph naming its source and mode. A takeover shows the source as closed by handoff, and it can be reopened (spec 304).

## Success criteria

- Wire and daemon: a handoff request on a `ModeSession` connection takes `worktree`, `mode`, `brief`. A daemon test asserts the 391 forced fields are still overridden: project, `orchestrator=false`, `spawned_by`, catalog agent, default permission mode.
- Worktree rules: `new` yields a branch from `origin/main`. `same` with a dirty tree and a non-takeover mode returns an error and creates no session.
- Takeover ordering: the source is closed only after the child's first state report. If the child fails to start, the source stays alive and the caller gets an error.
- Provenance: the child sees the brief marked as coming from the source session, set by the daemon rather than the client.
- GUI: a test covers the handoff glyph and the reopen-source affordance after a takeover.
- Evidence: the daemon logs one line per handoff: `handoff from=<id> to=<id> mode=<m> worktree=<w>`.

## Non-goals

- Hive generating the brief or summarizing the transcript.
- Moving an in-flight process or PTY state between agents. Handoff starts a new agent.
- Cross-project handoff.
- Chains where the child hands off onward (no grant is inherited, and 391's child cap applies).
- A user-initiated handoff from the GUI (agent-initiated only for now).
- Merging, rebasing or cleaning up worktrees after the handoff.

## Notes

Open questions:

1. Does `takeover` ship with `fresh` and `context`, or as a later phase? Brainstorm recommendation: later phase, since it carries the concurrent-writer and trust-transfer risk.
2. If the grant doesn't transfer, a takeover child can't continue the orchestration chain. Is that acceptable?

Related: 389, 390, 391, `docs/design-docs/agent-orchestration.md`, 192 (worktrees branch from origin/main), 304 (reopen closed session), 384.
