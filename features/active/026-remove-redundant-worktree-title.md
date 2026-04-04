# Feature: Remove redundant session title when worktree and session name match

- **GitHub Issue:** #26
- **Stage:** PLAN
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Branch:** —

## Description

When a worktree session's title matches the worktree branch name, the sidebar shows both redundantly (e.g. "my-branch ⎇ my-branch"). The grid view and tmux attach title already suppress the duplicate — the sidebar should do the same.

## Research

### Relevant Code
- `internal/tui/components/sidebar.go:355-359` — **The bug.** `renderItem()` unconditionally appends `⎇ <branch>` without checking if the branch matches the session title (`item.Label`), producing "my-branch ⎇ my-branch".
- `internal/tui/components/sidebar.go:111-123` — `Rebuild()` populates `SidebarItem` with `Label: sess.Title` and `WorktreeBranch: sess.WorktreeBranch` separately.
- `internal/tui/components/gridview.go:243-249` — **Correct pattern.** `renderCell()` checks `sess.WorktreeBranch != sess.Title` before appending branch; shows bare "⎇" when they match.
- `internal/tui/app.go:2872-2878` — `buildSessionHeader()` also correctly suppresses duplicates with the same check.
- `internal/tui/components/gridview_test.go:77-106` — `TestGridView_WorktreeBadge` verifies the expected behavior: badge-only when title==branch, badge+name when they differ.

### Constraints / Dependencies
- None. The fix is a one-line condition change mirroring the existing grid view pattern.

## Plan

<Filled during PLAN stage.>

### Files to Change
1. `path/to/file.go` — <what and why>

### Test Strategy
- <how to verify>

### Risks
- <what could go wrong>

## Implementation Notes

<Filled during IMPLEMENT stage.>

- **PR:** —
