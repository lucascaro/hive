# Feature: Selected session should persist across views and attach/detach

- **GitHub Issue:** #35
- **Stage:** PLAN
- **Type:** bug
- **Complexity:** M
- **Priority:** P2
- **Branch:** —

## Description

The currently selected session is lost when switching between the main view, grid view, or when attaching to and detaching from a session.

The selected session should remain consistent across:
- Switching from main view to grid view and back
- Attaching to a session and then detaching
- Any other view transitions

Currently, the selection resets when transitioning between views, forcing the user to re-navigate to the session they were working with.

## Research

### Root Cause

The sidebar and grid view have independent cursor indices. `AppState.ActiveSessionID`
is the canonical source of truth, but synchronization between components is incomplete:

1. **Grid → sidebar sync missing:** When exiting grid (esc/g/q), `gridView.Hide()` is
   called but the sidebar cursor is never synced back. The sidebar still points at
   whatever index it had before the grid was opened.

2. **Sidebar not rebuilt after attach/detach:** The `AttachDoneMsg` handler calls
   `restoreGrid()` (which correctly syncs the grid cursor) but never rebuilds or
   syncs the sidebar.

3. **Sidebar.Rebuild() clamps but doesn't sync:** When the sidebar rebuilds after
   state changes, if the cursor is out of bounds it clamps to the last item instead
   of syncing to `ActiveSessionID`.

### Selection Tracking Summary

| Component | Tracks | Entry Sync | Exit Sync |
|-----------|--------|-----------|-----------|
| Sidebar | `Cursor int` (index into Items) | No auto-rebuild | No sync on exit |
| GridView | `Cursor int` (index into sessions) | `SyncCursor()` called correctly | No sync on exit |
| AppState | `ActiveSessionID string` | Canonical source | Preserved |

### Relevant Code
- `internal/tui/components/sidebar.go:63` — `Cursor int` field
- `internal/tui/components/sidebar.go:71-149` — `Rebuild()` clamps cursor but doesn't sync to active session
- `internal/tui/components/sidebar.go:235-242` — `SyncActiveSession()` moves cursor to match a session ID
- `internal/tui/components/gridview.go:48` — `Cursor int` field
- `internal/tui/components/gridview.go:93-103` — `SyncCursor()` moves cursor to match session ID
- `internal/tui/components/gridview.go:115-116` — grid exit: `Hide()` with no sync back
- `internal/tui/app.go:340-353` — `AttachDoneMsg` handler: restores grid but not sidebar
- `internal/tui/app.go:162-173` — `restoreGrid()` syncs grid cursor but not sidebar
- `internal/tui/app.go:1011-1022` — grid toggle: correctly syncs grid on entry, no sidebar sync on exit
- `internal/tui/app.go:964` — `handleGridKey` processes grid exit but doesn't sync sidebar

4. **Grid attach doesn't update ActiveSessionID:** The `GridSessionSelectedMsg` handler
   (app.go:452-483) looks up the session to populate the attach message but **never sets
   `ActiveSessionID`**. After detach, `restoreGrid()` syncs the grid cursor to the old
   `ActiveSessionID`, not the session the user actually attached from.

### Constraints / Dependencies
- Sidebar cursor is an index into a flattened item list (projects + teams + sessions), not a direct session index — syncing by ID requires `SyncActiveSession()`
- `ActiveSessionID` must be updated when the grid cursor changes (e.g., user navigates in grid then attaches)

## Plan

<Filled during PLAN stage.>

### Files to Change
1. `path/to/file.go` — <what and why>

### Test Strategy
- <how to verify>

### Risks
- <what could go wrong>

## Implementation Notes

All four gaps identified in research were fixed as planned:

1. **Gap 4 — Grid attach sets ActiveSessionID:** Added `m.appState.ActiveSessionID = s.ID` in `GridSessionSelectedMsg` handler before building the attach message.
2. **Gap 2 — AttachDoneMsg syncs sidebar:** Added `sidebar.Rebuild()` + `sidebar.SyncActiveSession()` in `AttachDoneMsg` handler.
3. **Gap 1 — Grid exit syncs sidebar:** Capture grid selection before `gridView.Update()`, then sync `ActiveSessionID` and sidebar cursor if the grid was hidden.
4. **Gap 3 — Rebuild syncs to active session:** After clamping cursor bounds, `Rebuild()` now calls `SyncActiveSession(ActiveSessionID)`.

No deviations from the plan. Tests added:
- 4 unit tests for sidebar sync behavior (sidebar_test.go)
- 4 functional flow tests covering grid exit, grid attach, attach-done, and full round-trip (flow_grid_test.go)

- **PR:** —
