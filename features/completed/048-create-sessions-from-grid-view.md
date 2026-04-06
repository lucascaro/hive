# Feature: Allow creating new sessions from grid view

- **GitHub Issue:** #48
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Branch:** feat/48-grid-new-session

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

### Relevant Code
- `internal/tui/handle_keys.go` — `handleGridKey()` (line 169) handles all grid keybindings; sidebar `t`/`W` handlers at lines 282-323
- `internal/tui/components/gridview.go` — `Selected()` returns `*state.Session` with `ProjectID`
- `internal/tui/handle_system.go` — `handleAgentPicked()` handles the agent picker result and session creation flow

### Constraints / Dependencies
- None — the session creation flow (agent picker → create session / worktree branch input) is fully shared and needs no changes

## Plan

### Files to Change
1. `internal/tui/handle_keys.go` — Add `t` and `W` cases to `handleGridKey()`, mirroring sidebar logic but using `gridView.Selected()`
2. `internal/tui/flow_grid_test.go` — Add tests for both new keybindings

### Test Strategy
- `TestFlow_GridNewSession`: verify `t` hides grid, opens agent picker, sets correct `pendingProjectID` and `pendingWorktree=false`
- `TestFlow_GridNewWorktreeSession`: verify `W` validates git repo and opens agent picker with `pendingWorktree=true`

### Risks
- Minimal — follows exact same pattern as existing `x` and `r` grid key handlers

## Implementation Notes

- Added `t` case: gets selected session, validates ProjectID is non-empty, hides grid, sets pending state, shows agent picker
- Added `W` case: same as `t` but also validates project directory is a git repo before proceeding
- Both follow the hide-grid-first pattern used by `x` (kill) and `r` (rename)
- All 17 grid tests pass including 2 new ones; full test suite green

- **PR:** —
