# Feature: Allow creating new sessions from grid view

- **GitHub Issue:** #48
- **Stage:** RESEARCH
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Branch:** —

## Description

When in the grid view, users should be able to create new sessions (with or without worktree) using the project from the currently selected session.

Currently, `t` (new session) and `W` (new worktree session) only work from the sidebar view. The grid view only supports navigation, attach, kill, rename, and toggle all projects.

### Proposed behavior

- `t` in grid view: create a new session using the selected session's project ID, then open the agent picker
- `W` in grid view: create a new worktree session using the selected session's project (with git repo validation), then open the agent picker followed by branch input

### Implementation notes

- Add `t` and `W` cases to `handleGridKey()` in `internal/tui/handle_keys.go`
- Use `m.gridView.Selected()` to get the session and its `ProjectID`
- Hide the grid before entering the agent picker flow
- The rest of the session creation flow is already shared and requires no changes

## Research

<Filled during RESEARCH stage.>

### Relevant Code
- `path/to/file.go` — <why it matters>

### Constraints / Dependencies
- <anything blocking or complicating this>

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
