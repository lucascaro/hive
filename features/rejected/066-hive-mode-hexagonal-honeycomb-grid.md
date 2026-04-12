# Feature: Hive mode — hexagonal honeycomb grid layout

- **GitHub Issue:** #66
- **Stage:** REJECTED
- **Rejected:** 2026-04-11
- **Reason:** Implementation explored (pointy-top and flat-top variants, multiple tessellation attempts, variable slope sizing). Cannot produce a visually convincing honeycomb on a character grid while also preserving usable content area per cell and reasonable layout density. The 1:2 glyph aspect and limited diagonal char palette (`╱` `╲` only) fight each other — cells end up either too tall with empty interiors, or too short to read as hexagons. Branch: `feature/66-hex-grid-honeycomb` (not merged; discarded).
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P5
- **Branch:** —

## Description

Add an alternative grid layout that displays sessions as hexagons in a honeycomb pattern. Pressing `h` or `H` in grid view toggles between the current square grid and the hexagonal honeycomb layout. Thematic fit — the project is called Hive, so a honeycomb layout is natural.

## Research

Deep research doc: [`research/hex-grid/RESEARCH.md`](../../research/hex-grid/RESEARCH.md)

### Relevant Code

- `internal/tui/components/gridview.go:47-59` — `GridView` struct (Cursor, Width, Height, sessions, project/sessionColors, paneTitles). Add `LayoutMode` field here.
- `internal/tui/components/gridview.go:504-549` — `gridColumns()` layout-scoring algorithm. Hex needs a parallel `hexLayout()`.
- `internal/tui/components/gridview.go:245-404` — `renderCell()` builds the square cell (header, subtitle, preview, lipgloss `RoundedBorder`). Hex needs `renderHexCell()` built from `[]string` rows since lipgloss has no polygon primitive.
- `internal/tui/components/gridview.go:186-243` — `View()` composes the grid and clamps to `gv.Height` (critical Bubble Tea invariant).
- `internal/tui/components/gridview.go:136-183` — `Update()` keyboard navigation (linear cursor, step by `cols`). Hex needs row-parity-aware neighbor logic.
- `internal/tui/components/gridview.go:457-484` — `CellAt(x,y)` mouse mapping. Hex variant needs point-in-polygon (or bounding-rect approximation).
- `internal/tui/handle_keys.go:76-189` — `handleGridKey()`. Add `H` (capital) toggle case here, *before* delegation to `gridView.Update()` (lowercase `h` is vim-left nav).
- `internal/tui/styles/theme.go:269-295` — `GradientBg()`, reusable for hex header gradient (per-session color from #54).
- `internal/tui/components/gridview_test.go` — existing test patterns: `TestGridView_ExactHeight`, `TestGridView_NoLineExceedsWidth`, `TestGridView_CursorWrap`, `TestGridView_SelectedCellHasBackground`. Mirror each for hex.

### Constraints / Dependencies

- **Terminal glyph aspect ratio is ~2:1 tall:wide** — naive hex looks squat. Bias bounding box ~1.2–1.3× wider than tall (in cells).
- **No lipgloss polygon support** — must hand-draw outline with Unicode box chars + `╱` (U+2571) `╲` (U+2572). Diagonal char palette is sparse → edges will be jaggy (stylistic, accepted).
- **`h` key conflicts with vim-left navigation in current `Update()`.** Recommend `H` (shift) as the toggle key, not lowercase `h` — needs confirmation in PLAN.
- **Frame-height invariant** (`View()` must output exactly `gv.Height` lines) is more fragile with offset rows. Compute total hex grid height up front.
- **Selected-cell visibility under per-session colors (#54).** Memory note: session color must remain visible when selected. Hex outline color must contrast against gradient header.
- **Min terminal size:** very narrow terminals (<60 cols) may fit only 2–3 hex cells/row. Need graceful fallback.
- **No persistence needed initially** — `LayoutMode` is view-state only, not saved to AppState/config.

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
