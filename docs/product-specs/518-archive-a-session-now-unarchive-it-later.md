---
issue: 518
title: "Archive a session now, unarchive it later"
type: enhancement
complexity: L
priority: P2
stage: DONE
pr: 519
shipped: 2026-10-15
---

# Archive a session now, unarchive it later

- **Issue:** #518
- **Type:** enhancement
- **Complexity:** L
- **Exec plan:** [docs/exec-plans/completed/518-archive-a-session-now-unarchive-it-later.md](../exec-plans/completed/518-archive-a-session-now-unarchive-it-later.md)
- **Priority:** P2

## Problem

Closing a Hive session is the only way to get one out of the way for good, and a closed session isn't kept. The #304 tombstone is pruned at 20 entries or 7 days, and only the most recent one can be reopened. The alternative, minimize (#202), leaves the process running. Someone who has dozens of long-running agent sessions across projects can't put one away and return to it weeks later with its name, project, worktree and agent conversation intact.

## Desired behavior

- An explicit **Archive** action, separate from Close, stops the session's process and removes it from the running set.
- The session's worktree is left on disk and is never reclaimed or deleted by archiving.
- Archived sessions persist indefinitely, with no count or age expiry.
- A palette command opens a searchable modal list of archived sessions, grouped by project, showing name, agent, project and archive date.
- **Unarchive** restores the session the way #304 restores one: it returns with its name, colour, project, agent binding and worktree; the conversation resumes where the agent supports per-id resume; the UI reports anything lost (scrollback, conversation, a missing worktree, or the agent falling back to a shell).
- Close, and its 20/7d undo, behave exactly as before.
- This works for every agent kind, including Pi.

## Success criteria

1. Archiving a session writes a durable record under the state dir before the process stops, and the record survives a daemon restart.
2. After archive, the session is gone from the running set, and its process has exited.
3. The archived record is never pruned by age or count, and `pruneTombstones` leaves it alone.
4. The worktree directory still exists after archive, and boot-time orphan-worktree reclaim skips it.
5. Archiving a session with a dirty worktree shows no destructive-confirmation prompt, and uncommitted files are untouched.
6. The palette command lists every archived session, not just the latest, and the list filters by name and project.
7. Unarchiving rebuilds the entry with name, colour, project, agent and worktree, and revives the conversation for agents with per-id resume.
8. If the worktree or conversation is gone, unarchive still succeeds and reports what was degraded.
9. The archived entry is removed from the list once unarchived.
10. Deleting an archived entry permanently is a separate, confirmed action that offers worktree cleanup.
11. Existing Close and ⌘Z reopen behaviour is unchanged, and its tests still pass.

## Non-goals

- Searching inside archived conversations (#431). Filtering is by name and project only.
- Bulk archive or automatic archive rules, such as "archive idle for N days."
- Cross-machine sync. Archive is local to the state dir.
- Persisting terminal scrollback. It still isn't restored.
- Changing minimize (#202) or Close's retention and confirmations.
- A permanent sidebar "Archived" section (the modal list is the v1 surface).

## Notes

Open question: whether the delete-archived action (criterion 10) belongs in v1 or a follow-up. It's the only way to clear the list, so lean toward v1.

Related: #304 (reopen closed session; tombstones in `internal/registry/closed.go`, cap 20 / 7 days), #202 (minimize), #431 (transcript search).
