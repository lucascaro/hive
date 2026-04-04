# Feature: Grid view previews not updating after detach/reattach

- **GitHub Issue:** #28
- **Stage:** IMPLEMENT
- **Type:** bug
- **Complexity:** S
- **Priority:** P1
- **Branch:** —

## Description

After detaching from a full-screen session and returning to grid view, the session previews stop updating. The grid tiles show stale content instead of live output from the running sessions.

Steps to reproduce:
1. Start hive with multiple sessions in grid view
2. Attach to a session (full-screen)
3. Detach back to grid view
4. Observe that previews are no longer updating

Expected: grid view previews should resume updating live after returning from a detached/full-screen session.

## Research

### Root Cause
The `AttachDoneMsg` handler (`internal/tui/app.go:340-349`) calls `restoreGrid()` to restore the grid UI but does **not** call `scheduleGridPoll()` to restart the preview polling chain. Without the initial schedule, `PollGridPreviews` never fires and content stays stale.

This was a gap in the #22 fix (commit 2388766) which added `restoreGrid()` but forgot to restart polling.

### How Grid Preview Polling Works
- `PollGridPreviews()` (`internal/tui/components/gridview.go:28-43`) — uses `tea.Tick()` to capture pane content for all sessions, returns `GridPreviewsUpdatedMsg`
- `scheduleGridPoll()` (`internal/tui/app.go:2535-2541`) — helper that calls `PollGridPreviews()` with current sessions and interval
- `GridPreviewsUpdatedMsg` handler (`internal/tui/app.go:441-446`) — updates grid contents and reschedules if `gridView.Active`
- The chain is self-sustaining once started, but needs an initial `scheduleGridPoll()` call

### Relevant Code
- `internal/tui/app.go:340-349` — **The bug.** `AttachDoneMsg` handler restores grid but doesn't schedule polling.
- `internal/tui/app.go:161-173` — `restoreGrid()` sets up grid UI but not polling.
- `internal/tui/app.go:2535-2541` — `scheduleGridPoll()` helper that needs to be called.
- `internal/tui/app.go:209` — `Init()` correctly schedules grid poll when `gridView.Active`.
- `internal/tui/app.go:1007-1012` — "g" key handler correctly schedules grid poll.
- `internal/tui/components/gridview.go:28-43` — `PollGridPreviews()` polling function.
- `internal/tui/flow_grid_test.go:320-367` — Existing grid restore tests (don't verify polling).

### Constraints / Dependencies
- None. One-line fix: add `m.scheduleGridPoll()` to the `tea.Batch` in the `AttachDoneMsg` handler.

## Plan

### Files to Change
1. `internal/tui/app.go:349` — Add `m.scheduleGridPoll()` to the `tea.Batch` in the `AttachDoneMsg` handler, conditioned on `m.gridView.Active`.
2. `internal/tui/flow_grid_test.go` — Add/extend test to verify `AttachDoneMsg` with grid restore returns a command batch that includes grid polling (produces `GridPreviewsUpdatedMsg`).

### Test Strategy
- Unit test: verify returned `tea.Cmd` from `AttachDoneMsg` produces a `GridPreviewsUpdatedMsg`
- Manual: attach from grid, detach, confirm previews resume updating

### Risks
- Minimal. Same pattern used in `Init()` and "g" key handler.

## Implementation Notes

- No deviations from plan. Added `scheduleGridPoll()` to the `AttachDoneMsg` handler's `tea.Batch`, guarded by `gridView.Active`.
- Test executes the `tea.BatchMsg` sub-commands and verifies one produces `GridPreviewsUpdatedMsg`.

- **PR:** —
