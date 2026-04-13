# Feature: Fix arrow key navigation when grid cells are expanded

- **GitHub Issue:** #101
- **Stage:** DONE
- **Type:** bug
- **Complexity:** S
- **Priority:** P1
- **Branch:** —

## Description

When in grid view with a session cell expanded downward, arrow key navigation does not account for the visual layout of expanded cells. For example, with 5 sessions where session 3 is expanded (occupying extra rows), pressing right arrow on session 5 should navigate to the expanded session 3 (which appears after session 5 in the visual grid reading order). Pressing left arrow from there should return to session 3. Currently arrow navigation ignores expanded cell geometry, making navigation confusing and non-intuitive.

## Research

Arrow key navigation lives entirely in `internal/tui/components/gridview.go`.
Extended cells are cells whose column has no session in the last row — the last
real session in that column absorbs the empty space below it (rendered taller).

### Relevant Code
- `internal/tui/components/gridview.go:157-201` — `Update()` handles left/right/up/down. Right is bounded by `n-1` and never considers extended visual slots. This is the root of the bug.
- `internal/tui/components/gridview.go:206-266` — `View()` renders the grid column-by-column; extended cell logic at lines 242-266: when `emptyInLastRow && r == rows-2`, the cell height is `cellH + lastCellH`.
- `internal/tui/components/gridview.go:506-565` — `CellAt()` already maps mouse clicks to extended cells correctly; the same "owner" formula can be used for keyboard nav: `owner = (rows-2)*cols + col`.
- `internal/tui/components/gridview_test.go:341-385` — `TestGridView_CursorWrap`: the test case `{"right at last session stays", 4, ..., 4}` will need updating (correct new target is idx=2, the extended cell owner).

### Constraints / Dependencies
- `atExtended bool` field must be added to `GridView` struct to track when the cursor is in the "virtual last row" of an extended cell; without this, left-from-extended and right-from-extended can't distinguish the extended visual position from the cell's real row-0 position.
- `SyncCursor`, `MoveUp`, `MoveDown` must reset `atExtended = false` to stay consistent when the cursor is externally repositioned.

## Plan

Add `atExtended bool` to `GridView` to track when the cursor is visually at the
extended (lower) portion of a cell. Use it in `left`/`right` to route navigation
through the "virtual last row" instead of the session's real row-0 position.

### Files to Change
1. `internal/tui/components/gridview.go`
   - Add `atExtended bool` field to `GridView` struct (after `bellBlinkOn`)
   - Rewrite `case "right"` in `Update()`: compute `nextIdx = row*cols + nextCol`; if `nextIdx >= n` and the slot is an extended column (`n%cols != 0 && nextCol >= n%cols && row == rows-1`), navigate to owner `(rows-2)*cols + nextCol` and set `atExtended=true`; otherwise existing wrap logic
   - Rewrite `case "left"` in `Update()`: when `atExtended`, navigate via `(rows-1)*cols + (col-1)` (real cell) or its owner (extended cell); else existing logic
   - Add `gv.atExtended = false` at start of `case "up"` and `case "down"`
   - `SyncCursor()`: add `gv.atExtended = false` after setting cursor
   - `MoveUp()` / `MoveDown()`: add `gv.atExtended = false`
   - `Show()`: add `gv.atExtended = false` — resets stale extended state when session count changes (e.g. spawning a new session from the grid)

2. `internal/tui/components/gridview_test.go`
   - Update `TestGridView_CursorWrap`: change `{"right at last session stays", 4, …, 4}` → wantCursor=2 (navigates to extended cell owner) and rename to `"right navigates to extended cell"`
   - Add `TestGridView_CursorWrap_ExtendedCell`: covers right→extended, left←extended, and up/down resetting atExtended

3. `internal/tui/flow_test.go`
   - Add `AssertGridCursor(want int)` helper
   - Add `TestFlow_GridExtendedCellNavigation`: 5-session flow; open grid; set cursor to idx 4 (session 5); press right; assert cursor=2 (session 3); press left; assert cursor=4 (session 5)

### Test Strategy
- `TestGridView_CursorWrap` (updated): `right navigates to extended cell` — cursor=4 → cursor=2 after right
- `TestGridView_CursorWrap_ExtendedCell/right_to_extended` — cursor=4 → cursor=2, atExtended=true
- `TestGridView_CursorWrap_ExtendedCell/left_from_extended` — cursor=2, atExtended=true → cursor=4, atExtended=false
- `TestGridView_CursorWrap_ExtendedCell/up_clears_extended` — cursor=2, atExtended=true, press up → atExtended=false
- `TestGridView_CursorWrap_ExtendedCell/down_clears_extended` — cursor=4, press down → does nothing (already last row); atExtended stays false
- `TestFlow_GridExtendedCellNavigation` — end-to-end right→session3, left→session5

### Risks
- `atExtended=true` persists if cursor is moved externally without going through Update. Mitigated by resetting in SyncCursor/MoveUp/MoveDown.
- Multi-extended-column case (n=4, cols=3): columns 1 and 2 are both extended. Right from idx=3 goes to idx=1 (atExtended=true), then right again goes to idx=2 (atExtended=true). This is correct but untested. Adding a comment is sufficient.

## Implementation Notes

Added `atExtended bool` to `GridView`. Rewrote `left`/`right` in `Update()` to route through virtual-last-row indices when the flag is set. `right` sets it when navigating into an extended slot; `left` clears it when leaving. `up`/`down`, `Show()`, `SyncCursor()`, `MoveUp()`, `MoveDown()` all clear the flag. No plan deviations.

- **PR:** #104
