---
issue: 395
title: "Name a worktree group by double-clicking its title"
type: enhancement
complexity: L
priority: P2
pr: 396
stage: REVIEW
---

# Name a worktree group by double-clicking its title

- **Issue:** #395
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/395-rename-sessions-from-worktree-group-title.md](../exec-plans/active/395-rename-sessions-from-worktree-group-title.md)

## Problem

The sidebar already supports double-click-to-rename on a session row and on a tile name, but a worktree group header is inert. A group of sessions sharing a worktree is the unit the operator actually thinks in — "the auth refactor", "the flaky-test hunt" — and there is nowhere to write that down. The header can only state the branch, which is what git called it, not what the work is.

Sessions that share a worktree are also often byte-identical rows: registry `create.go` names a worktree session after its branch without uniquifying. The sidebar already copes with that by hiding a member's name when it is still the branch-derived default (`titleOnly`), leaving the window title to tell rows apart. That mechanism works and is not the thing to change.

## Desired behavior

Double-clicking a worktree group's title opens the same inline rename editor used elsewhere in the sidebar. Committing sets a **name on the group itself** — a persisted, daemon-side label attached to the worktree — and the header then shows that name with the branch beside it.

Session names are never touched. A member still carrying the branch-derived default keeps having it hidden, exactly as today.

## Success criteria

- Double-clicking a worktree group's title opens an inline rename editor with the same behaviour as the session and project renames: Enter and blur commit, Escape cancels, focus returns to the active terminal.
- The editor is seeded with the group's current name when it has one, and with the branch name otherwise.
- Committing persists the name against the worktree in the daemon's state, so it survives a GUI reload, a daemon restart, and is visible to any client that reads the worktree label — not only the GUI that set it.
- Committing performs **no** session rename: no `UpdateSession` call is made, and every member's `name` is byte-identical before and after.
- The group header shows the name when the group has one, with the branch beside it; an unnamed group shows only the branch, exactly as it does today.
- An open sidebar repaints when the label changes, without a session having changed.
- Members whose name is still the branch-derived default continue to render `titleOnly` — naming the group does not change the member rows at all.
- Double-clicking the chevron, the member count, or the collapsed-state attention badge does not open the editor.
- Clearing the name (committing an empty value) removes the label and returns the header to branch-only.
- The header does not overflow and the branch stays visible at the 220px sidebar floor, verified in a real browser rather than by reading the CSS.
- Existing group behaviour is unchanged: sticky header offsets, collapse animation, the shared colour bar, the collapsed-state attention count, and drag-to-reorder.
- The daemon contract is bumped, and all three wire clients are updated in lock-step, per `AGENTS.md`.

## Non-goals

- Renaming sessions. This spec explicitly does not modify any session's name; the earlier fan-out design was withdrawn.
- Changing the `titleOnly` rule that hides a member's branch-derived default name.
- Renaming the git branch or the worktree directory; that is what `RenameWorktree` already does.
- Deriving a label from member names. The label is real state, set explicitly.
- Surfacing the label anywhere outside the sidebar group header (tile chrome, launcher, menu bar) in this spec.
- A per-session label, or a label on a worktree that has fewer than two sessions — the header only exists for groups.

## Notes

Storage was chosen deliberately over a GUI-local `localStorage` key: the operator asked for a name that any client can see, which rules out per-webview storage. The cost is a new wire frame, registry persistence, a `DaemonContract` bump and the three-client lock-step `AGENTS.md` requires — which is why this is `L` and not `M`.
