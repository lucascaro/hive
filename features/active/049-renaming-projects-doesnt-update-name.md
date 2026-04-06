# Feature: Bug: Renaming projects doesn't update the project name

- **GitHub Issue:** #49
- **Stage:** IMPLEMENT
- **Type:** bug
- **Complexity:** S
- **Priority:** P3
- **Branch:** fix/49-rename-projects

## Description

When renaming a project, the project name doesn't actually change. The rename operation appears to complete but the original name persists.

### Steps to reproduce

1. Open an existing project
2. Attempt to rename it
3. Observe that the project name remains unchanged

### Expected behavior

The project name should update to the new name after renaming.

### Actual behavior

The project name stays the same after the rename operation.

## Research

### Root Cause

`handleTitleEdit()` in `handle_input.go:180` only dispatches a message when `sessionID != ""`. For projects, `sessionID` is empty, so the function returns `nil` — no state mutation occurs. The rename infrastructure was built only for sessions; projects and teams were missing state reducers, message types, a TitleEditor `ProjectID` field, and handler logic.

### Relevant Code
- `internal/tui/handle_input.go` — `startRename()` and `handleTitleEdit()` silently fail for non-session items
- `internal/tui/components/titleedit.go` — TitleEditor lacked `ProjectID` field
- `internal/state/store.go` — no `UpdateProjectName` or `UpdateTeamName` reducers
- `internal/tui/messages.go` — no message types for project/team renames
- `internal/tui/app.go` — no handler cases for project/team renames
- `internal/tui/views.go` — rename dialog title didn't distinguish projects

### Constraints / Dependencies
- None

## Plan

### Files to Change
1. `internal/tui/components/titleedit.go` — add `ProjectID` field, update `Start()`/`Stop()` signatures
2. `internal/state/store.go` — add `UpdateProjectName`, `UpdateTeamName` reducers
3. `internal/tui/messages.go` — add `ProjectNameChangedMsg`, `TeamNameChangedMsg`
4. `internal/tui/handle_input.go` — fix `startRename` to pass `ProjectID`, fix `handleTitleEdit` to dispatch correct message type
5. `internal/tui/app.go` — add handler cases for new message types
6. `internal/tui/views.go` — show "Rename Project" dialog title when renaming a project

### Test Strategy
- Unit tests for `UpdateProjectName` and `UpdateTeamName` in `store_test.go`
- Updated existing `TitleEditor` tests + new `StartWithProject` test
- Manual: run hive, create project, press `r`, rename, verify sidebar updates and persists

### Risks
- Low risk — small, additive changes following existing patterns

## Implementation Notes

All changes implemented. State reducers, message types, TitleEditor field, handler logic, dialog title, and tests all added.

- **PR:** —
