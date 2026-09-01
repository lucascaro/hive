---
issue: 304
title: "Reopen a closed session (undo close)"
type: enhancement
complexity: M
priority: P2
stage: DONE
pr: 306
shipped: 2026-08-31
---

# Reopen a closed session (undo close)

- **Issue:** #304
- **PR:** #306
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Stage:** DONE
- **Exec plan:** [docs/exec-plans/completed/304-reopen-a-closed-session-undo-close.md](../exec-plans/completed/304-reopen-a-closed-session-undo-close.md)

## Problem

Closing a session is a one-way teardown. `registry.kill()` deletes the entry,
kills the PTY child, disposes of the worktree, and removes
`<stateDir>/sessions/<id>/` — with nothing written down first. An accidental
`⌘W` on the wrong tile, or a confirm dialog dismissed too fast, has no recovery
path at all: the session, its name, its project and worktree binding, and its
agent conversation are simply gone.

The entry's persisted state is small and fully described by
`registry.MetaFile`, and `Revive()` already resumes agent conversations by the
pinned `agent_session_id`. Everything needed to bring a session back already
exists; nothing captures it before the teardown runs.

## Desired behavior

Closing a session leaves a recoverable trace. Immediately after a close, an
undo affordance is offered inline; later, the most recently closed session can
be reopened from a keybinding or the File menu. A reopened session comes back
with its name, colour, project, agent binding and worktree, and — for agents
that support per-id resume — its conversation.

Undo is honest about what it cannot restore. Terminal scrollback and any
in-flight agent state that was never written to the agent's rollout file do not
come back, and the UI says so rather than reporting a silent success.

Closing remains exactly as protected as it is today: every live-session close
still prompts before a dirty worktree can be touched, and destroying
uncommitted work still requires explicitly picking the danger choice. Undo does
not make close less careful.

## Success criteria

- Closing a session writes a tombstone under `<stateDir>/closed/<id>.json`
  before any teardown step runs, and the tombstone survives a daemon restart.
- A `RESTORE_SESSION` wire frame rebuilds the entry: name, colour, project,
  agent, worktree binding and `agent_session_id` all return, and the session is
  revived with its conversation when the agent supports per-id resume.
- Restore reports what was degraded (worktree lost or recreated, conversation
  lost, agent fell back to a shell, recovery patch skipped) rather than a bare
  success, and the UI surfaces it.
- A worktree that still exists on disk is re-adopted; one that was pruned is
  recreated from its surviving branch; one whose branch is also gone restores
  without a worktree instead of failing.
- An undo banner appears after a close initiated by this client, and `⌘Z` /
  File ▸ Reopen Closed Session / the command palette reopen the most recently
  closed session.
- Before the destructive `Close and delete worktree` path deletes anything, a
  capped recovery patch of the uncommitted state is written next to the
  tombstone, and its path is surfaced on restore.
- Boot-time orphan-worktree reclaim no longer deletes a worktree that a live
  tombstone still refers to.
- Every existing close confirmation still fires; no close path becomes quieter.

## Non-goals

- Restoring terminal scrollback. That needs disk-backed scrollback, which does
  not exist yet.
- Auto-applying the recovery patch during restore. The patch is written and its
  path surfaced; running `git apply` stays the user's call.
- Undoing a project close as a single unit. Sessions closed by a project kill
  are individually reopenable, but there is no one-shot project undo.
- A full "recently closed" browser UI. The wire call returns a list so one can
  be added later without another protocol change.
- Loosening any close confirmation.

## Notes

Scaffolded via plan-first mode; the approved design, the irreversibility audit
and the resolved decisions live in the exec plan.
