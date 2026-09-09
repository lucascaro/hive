---
issue: 384
pr: 385
title: "Show a visual cue when sessions share a worktree"
type: enhancement
complexity: M
priority: P2
stage: REVIEW
---

# Show a visual cue when sessions share a worktree

- **Issue:** [#384](https://github.com/lucascaro/hive/issues/384)
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2

## Problem

When a session is created against an existing worktree the registry adopts that
worktree (`internal/registry/create.go:340-351`), so two or more sessions end up
backed by the same `WorktreePath`. The backend knows this — `WorktreeShared`
guards worktree removal on kill (`internal/registry/registry.go:1307`) — but the
GUI does not surface it: each `SessionRow` renders only its own branch glyph
(`cmd/hivegui/frontend/src/components/SessionRow.tsx:87,186`), so sessions that
share a working directory look exactly like unrelated ones. Users cannot tell
which sessions will step on each other's files, and closing one has
worktree-deletion semantics that depend on invisible state.

## Desired behavior

Sessions backed by the same worktree are visually linked in the sidebar (and in
grid mode), so the grouping is obvious at a glance, and the link's interaction
with sorting and manual reordering is defined rather than accidental.

## Success criteria

- When two or more sessions in a project have the same non-empty worktree path,
  every one of those rows shows a coloured 3px bar on its right edge, and all
  rows in that group show the same colour.
- A session that adopts another session's worktree inherits that session's
  colour, so the group shares a colour without the user doing anything.
- The worktree glyph on those rows carries the group size as a visible count,
  and the glyph's accessible label states the sharing, so the cue is not
  colour-only.
- A solo session, and a session with no worktree, shows neither bar nor count.
- Sessions sharing a worktree paint adjacently inside their project, whatever
  their stored `.order`.
- Dragging any member of a shared group moves the whole group; after the drop
  the group's members hold contiguous `.order` values in the daemon's list.
- The worktrees modal names the sessions occupying each worktree, not just how
  many.

## Non-goals

- Grid-mode tiles (`TileChrome.tsx`) and minimized project chips
  (`MinimizedTray.tsx`) keep their current appearance.
- Sessions that merely share a plain project cwd (no git worktree) are not
  marked.
- No new theme tokens: the cue reuses the existing per-session colour.
- No new wire field, no daemon-side ordering change: `worktree_path` is already
  broadcast and spec 305 non-goaled touching `moveInOrder`/`reindexLocked`.
- No explicit "attach this session to that worktree" action; sharing stays a
  side effect of duplicating a session.

## Notes

Open design questions carried from the request: how sharing sessions are linked
visually, and what that implies for sidebar sorting / drag reordering.
