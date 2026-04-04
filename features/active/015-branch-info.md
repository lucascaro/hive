# Feature: Branch info

- **GitHub Issue:** #15
- **Stage:** PLAN
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Branch:** —

## Description

If a session is opened in a git branch, display that branch info. This is already true for worktrees but should also work for regular branches.

## Research

Branch info is currently only displayed for **worktree sessions**. Regular sessions created in a git repo do not detect or store the current branch. The display infrastructure already exists — we just need to detect the branch for non-worktree sessions and feed it through the same rendering path.

### Relevant Code

**Git utilities:**
- `internal/git/git.go:15-29` — `IsGitRepo()` and `Root()` exist but there is **no `CurrentBranch()` helper**

**Session model:**
- `internal/state/model.go:134-135` — `WorktreePath` and `WorktreeBranch` fields on `Session`; branch display is gated on `WorktreePath != ""`

**Session creation:**
- `internal/tui/app.go:1718-1797` — `createSessionWithWorktree()` sets `WorktreeBranch` explicitly
- `internal/tui/app.go:1818-1886` — `createSession()` does **not** detect or store any branch info

**Display — sidebar:**
- `internal/tui/components/sidebar.go:355-358` — renders `⎇ <branch>` badge only when `IsWorktree && WorktreeBranch != ""`
- `internal/tui/components/sidebar.go:121-122` — populates `SidebarItem` from session fields

**Display — grid view:**
- `internal/tui/components/gridview.go:243-249` — renders `⎇ <branch>` badge only when `WorktreePath` is set

**Display — tmux window title:**
- `internal/tui/app.go:2872-2878` — includes `⎇ <branch>` in tmux title for worktree sessions

### Constraints / Dependencies
- Need a new `CurrentBranch()` helper in git package (`git symbolic-ref --short HEAD` or `git rev-parse --abbrev-ref HEAD`)
- Display logic currently gates on `WorktreePath != ""` — need to change condition to check branch presence instead
- Branch can change during session lifetime (user runs `git checkout`); initial detection at creation time is sufficient for v1, live tracking is a separate concern
- Detached HEAD state needs a fallback (show short SHA or nothing)

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
