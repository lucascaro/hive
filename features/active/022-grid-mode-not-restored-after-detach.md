# Feature: Grid mode not restored after attach → detach

- **GitHub Issue:** #22
- **Stage:** IMPLEMENT
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Branch:** —

## Description

When in grid mode, attaching to a session and then detaching does not return to grid mode. Instead, the main view is shown. The RestoreGridMode mechanism exists but is not working correctly for this flow.

## Research

### Root Cause

The `AttachDoneMsg` handler in `internal/tui/app.go:334-342` sets `m.appState.RestoreGridMode` but **never calls `m.gridView.Show()`** to actually restore the grid UI. The grid restoration logic (calling `gridView.Show()`) only exists in `New()` at `internal/tui/app.go:154-163`, which is only called on the native backend path (where the TUI restarts). With the tmux backend (`tea.ExecProcess`), the TUI continues in the same `Model` instance, so `New()` is never called again after detach.

**Why native backend works:** Uses `tea.Quit` → `cmd/start.go` calls `New()` again → `New()` checks `RestoreGridMode` and calls `gridView.Show()`.

**Why tmux backend fails:** Uses `tea.ExecProcess` → TUI continues in same instance → `AttachDoneMsg` sets state but never shows grid.

### Relevant Code
- `internal/tui/app.go:334-342` — **THE BUG**: `AttachDoneMsg` handler sets `RestoreGridMode` but doesn't restore grid
- `internal/tui/app.go:154-163` — Grid restoration logic in `New()` that should be reused
- `internal/tui/app.go:441-472` — `GridSessionSelectedMsg` handler captures `gridView.Mode` into `SessionAttachMsg`
- `internal/tui/app.go:2741-2764` — `doAttach()` passes `RestoreGridMode` to `AttachDoneMsg` callback (line 2762)
- `internal/tui/components/gridview.go:58-68` — `GridView.Show()` method
- `internal/state/model.go:72-79` — `GridRestoreMode` enum definition
- `internal/state/model.go:167-170` — `RestoreGridMode` field in `AppState`
- `internal/tui/messages.go:22-34` — `SessionAttachMsg` with `RestoreGridMode`
- `internal/tui/messages.go:45-48` — `AttachDoneMsg` with `RestoreGridMode`
- `cmd/start.go:99-127` — Native backend re-entry loop (working path)
- `internal/tui/flow_grid_test.go:14-79` — Existing test for native backend grid restore
- `internal/tui/app_test.go:484-529` — `AttachDoneMsg` tests (missing grid restore case)

### Constraints / Dependencies
- Fix is small: add grid restoration calls in the `AttachDoneMsg` handler, mirroring the logic in `New()`
- Need a test for `AttachDoneMsg` with `RestoreGridMode` set (tmux backend path)

## Plan

Add grid restoration logic to the `AttachDoneMsg` handler, mirroring what `New()` already does at lines 154-163. Extract the shared logic into a helper method to avoid duplication.

### Files to Change
1. `internal/tui/app.go` — Extract grid restoration logic from `New()` (lines 154-163) into a new method `restoreGrid()` on `*Model`. Call it from both `New()` and the `AttachDoneMsg` handler (line 338). In the `AttachDoneMsg` handler, after setting `m.appState.RestoreGridMode`, call `m.restoreGrid()` before returning.
2. `internal/tui/flow_grid_test.go` — Add `TestFlow_GridAttachDoneRestoresGrid` that sends an `AttachDoneMsg` with `RestoreGridMode: GridRestoreProject` and asserts the grid is active afterwards (tmux backend path). Also test with `GridRestoreAll`.

### Test Strategy
- New unit test: send `AttachDoneMsg{RestoreGridMode: state.GridRestoreProject}` to a model, assert `gridView.Active == true` and `gridView.Mode == GridRestoreProject`
- Verify existing tests still pass (`go test ./internal/tui/...`)
- Manual: open grid → attach → detach → verify grid is restored

### Risks
- Low risk. The restoration logic is well-understood and already working in the `New()` path. We're just calling it from the additional code path.

## Implementation Notes

<Filled during IMPLEMENT stage.>

- **PR:** —
