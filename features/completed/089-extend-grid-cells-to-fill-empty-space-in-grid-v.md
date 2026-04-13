# Feature: Extend grid cells to fill empty space in grid view

- **GitHub Issue:** #89
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Branch:** feature/89-extend-grid-cells-fill-empty-space

## Description

In grid mode, when the number of sessions does not evenly fill the grid layout, empty cells are left at the bottom of the screen. Instead of leaving blank space, the cell(s) directly above the empty space should be extended vertically to fill the remaining screen area. This ensures the full terminal height is always utilized and improves the visual density of the grid layout.

## Research

The entire rendering path lives in one function.

### Relevant Code
- `internal/tui/components/gridview.go:219–261` — `View()` method: computes `cols`, `rows`, `cellH`, renders the row loop, clamps output to `gv.Height` lines
- `internal/tui/components/gridview.go:229–240` — the loop where empty cells (`idx >= n`) are rendered at the same `cellH` as real cells; this is where empty space is wasted
- `internal/tui/components/gridview_test.go:67–100` — `TestGridView_ExactHeight`: verifies output always equals `gv.Height` lines; any fix must keep this passing

### Constraints / Dependencies
- The hard-clamp at lines 254–260 ensures output is exactly `gv.Height` lines regardless of integer-division rounding. The fix must continue to satisfy this invariant (i.e., the clamp is the safety net, not the mechanism).
- Empty cells in the last row must simply not be rendered — replacing them with taller real cells is the goal.

## Plan

In `View()`, after computing `cellH`, derive a `lastCellH` for the final row:

```go
totalH := gv.Height - hintH
lastCellH := totalH - (rows-1)*cellH
if lastCellH < cellH {
    lastCellH = cellH  // safety: never shrink the last row
}
```

In the render loop, when we're on the last row (`r == rows-1`), use `lastCellH` instead of `cellH` for real cells, and skip rendering empty cells entirely (no append for `idx >= n` on the last row).

### Files to Change
1. `internal/tui/components/gridview.go` — modify `View()`: add `lastCellH` derivation after line 226; in the row loop pass `lastCellH` to `renderCell` when `r == rows-1`; skip empty cells on the last row

### Test Strategy
- `internal/tui/components/gridview_test.go` — `TestGridView_LastRowFillsHeight`: for layouts where sessions don't evenly fill the grid (e.g. 3 sessions in a 2-col grid), assert that the last real cell's rendered height fills to the bottom (i.e., the total grid output is still `gv.Height` lines AND no empty-cell padding lines appear after the last session)
- Existing `TestGridView_ExactHeight` and `TestGridView_ExactHeight_VariousCounts` must continue to pass — they are the height-invariant safety net

### Risks
- `lastCellH < 5` (the minimum): the safety clamp `if cellH < 5 { cellH = 5 }` applies to `cellH`; `lastCellH` should get the same treatment
- Integer division can make `lastCellH` slightly larger than needed; the existing hard-clamp at lines 254–260 handles any overshoot

## Implementation Notes

- Added `totalH` variable to avoid recomputing `gv.Height - hintH`.
- `lastCellH = totalH - (rows-1)*cellH` gives the last row all remaining space after integer division.
- Empty cells in the last row are skipped entirely (`continue` before appending); rows that end up with no `cellViews` are also skipped to avoid empty `JoinHorizontal` calls.
- Applied the `< 5` minimum clamp to `lastCellH` for consistency with `cellH`.
- All existing height-invariant tests pass; new `TestGridView_LastRowFillsHeight` verifies both the height invariant and that all sessions remain visible across partial-fill layouts.

- **PR:** —
