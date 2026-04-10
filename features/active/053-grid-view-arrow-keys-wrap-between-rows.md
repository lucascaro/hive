# Feature: Grid view: arrow keys should wrap between rows

- **GitHub Issue:** #53
- **Stage:** PLAN
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Branch:** —

## Description

In grid view, pressing right-arrow on the rightmost cell in a row does nothing. Similarly, pressing left-arrow on the leftmost cell does nothing.

### Expected behavior
- **Right-arrow on last cell in a row**: move to the first cell of the next row (if it exists).
- **Left-arrow on first cell in a row**: move to the last cell of the previous row (if it exists).

## Research

### Summary

Single-file fix. The grid cursor is a flat integer index into a sessions slice. Row/col are derived on the fly from `cols` (dynamically computed by `gridColumns()`). The `left` and `right` key branches in `Update()` clamp to row boundaries but never cross them. Up/down also don't wrap — they clamp at the top/bottom.

### Relevant Code

- `internal/tui/components/gridview.go:47-58` — `GridView` struct. `Cursor int` is the flat index. `sessions []*state.Session` is the backing list.
- `internal/tui/components/gridview.go:120-163` — `Update(msg tea.KeyMsg)`. The switch block with `case "left"`, `case "right"`, `case "up"`, `case "down"` branches. **This is where the fix goes.**
- `internal/tui/components/gridview.go:140-144` — **Left:** computes `rowStart := (gv.Cursor / cols) * cols`. Decrements cursor only if `gv.Cursor > rowStart` (clamps at left edge).
- `internal/tui/components/gridview.go:145-152` — **Right:** computes `rowEnd` (min of row's theoretical end and `n-1`). Increments cursor only if `gv.Cursor < rowEnd` (clamps at right edge).
- `internal/tui/components/gridview.go:457-502` — `gridColumns(w, h, n)` — dynamically computes optimal column count from terminal size and session count. Called every update cycle.
- `internal/tui/components/gridview_test.go` — existing tests cover rendering, `SyncCursor`, height invariants. **No tests for key-based cursor movement.** New unit tests needed.
- `internal/tui/flow_grid_test.go:211-228` — `TestFlow_GridNavigateAndSelect` sends `"l"` (right) but only asserts grid visibility, not cursor position.

### Constraints / Dependencies

1. **Last row may be partial.** With 5 sessions and 3 cols: row 0 = `[0,1,2]`, row 1 = `[3,4]`. Wrapping right from index 2 → index 3 is fine. Wrapping left from index 3 → index 2 (last cell of prev row) is fine. But wrapping right from index 4 must stop (no row 2).
2. **Up/down don't wrap.** This feature only adds horizontal wrap. Vertical wrap is not part of this issue and should be kept consistent (no wrap).
3. **No existing unit tests for cursor movement** — new tests must be added from scratch.
4. **Cols can change between renders** (terminal resize), but the `SyncCursor()` method already clamps the cursor into bounds, so resize safety is covered.

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
