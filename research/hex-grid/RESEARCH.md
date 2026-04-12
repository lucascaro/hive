# Hex Grid (Honeycomb) Layout — Research

Issue: #66 · Stage: RESEARCH → PLAN · Complexity: L

Add a hexagonal honeycomb layout as an alternative to the existing square grid view. `h`/`H` toggles between layouts while in grid view. Thematic fit: project is named Hive.

## Current grid architecture

Single GridView component with linear cursor index. View() composes a 2D grid by dividing terminal width/height by computed cols/rows, renders each cell with a lipgloss rounded border, joins horizontally then vertically, then clamps output to exactly `gv.Height` lines.

### Key locations

| Concern | File | Lines | Notes |
|---|---|---|---|
| Struct | `internal/tui/components/gridview.go` | 47–59 | `GridView` — Cursor (linear int), Width, Height, sessions, projectColors, sessionColors, paneTitles |
| Layout math | `gridview.go` | 504–549 | `gridColumns(w,h,n)` — scoring fn: `waste*5 + |cols-rows|*2 + ratioDiff` (ideal cellW/cellH ≈ 2.5 for terminal 2:1 glyphs); min cell 24×6; reserves 2 rows for hint bar |
| Cell render | `gridview.go` | 245–404 | `renderCell()` — header (status dot + title + project suffix + branch), optional subtitle (pane title) when h≥8, content preview (sanitized ANSI), lipgloss `RoundedBorder()` 2-col 2-row chrome; selected uses accent color |
| Compose | `gridview.go` | 186–243 | `View()` — builds rows×cols, joins, appends hint bar, clamps to `gv.Height` (critical invariant — Bubble Tea frame corruption otherwise) |
| Navigation | `gridview.go` | 136–183 | `Update()` — arrow + hjkl + wasd; up/down step by `cols`; left/right wrap across rows |
| Mouse | `gridview.go` | 457–484 | `CellAt(x,y)` — linear `row*cols + col` mapping; rejects clicks in hint bar |
| Key dispatch | `internal/tui/handle_keys.go` | 76–189 | `handleGridKey()` — handles `g`/`G` (grid mode toggle), `x`/`r`/`t`/`c`/`C`/`v`/`V`/`W`, then delegates to GridView.Update() |
| View stack | `internal/tui/viewstack.go` | 169–174 | `openGrid()` — pushes ViewGrid; SyncState passes sessions, project/session color maps, cursor sync |
| Gradient render | `internal/tui/styles/theme.go` | 269–295 | `GradientBg(text, colorA, colorB, bold)` — per-rune linear-interp background with WCAG-contrasting fg; used for session-color titles (#54) |
| Tests | `internal/tui/components/gridview_test.go` | 522 lines | `TestGridView_ExactHeight`, `TestGridView_NoLineExceedsWidth`, `TestGridView_CursorWrap`, `TestGridView_SelectedCellHasBackground` patterns |

## Hex-specific challenges

### 1. Terminal aspect ratio (2:1 tall:wide glyphs)

A regular hexagon is √3:1 (≈1.73). On a terminal grid, characters are roughly 2:1 tall:wide. A naive hex drawn cell-by-cell will look squat or stretched depending on orientation. **Decision needed in PLAN:** pointy-top vs flat-top, and how to bias cell dimensions so the drawn shape reads as a hex.

Recommended bias: pointy-top hex with bounding box ~1.2–1.3× wider than tall in *cells*, which approximates visual regularity given the 2:1 glyph ratio.

### 2. Lipgloss has no polygon primitive

All built-in borders are rectangular (`NormalBorder`, `RoundedBorder`, `ThickBorder`, `DoubleBorder`). Hex outline must be drawn manually with Unicode box-drawing + diagonals (`╱` U+2571, `╲` U+2572). Diagonal char availability is sparse — only one slope per direction, no half-cell variants — so edges will be coarse.

Sketch (pointy-top):
```
   ╱─────╲
  │       │
  │       │
   ╲─────╱
```

Build cells as `[]string` row-by-row in a `strings.Builder` rather than relying on lipgloss border styles.

### 3. Offset row layout

Honeycomb requires alternating rows shifted by half a cell width. Implementation options:
- **Offset coordinates** (recommended): row 0, 2, 4 at column 0; row 1, 3, 5 shifted by `cellW/2`. Maps cleanly to terminal columns; neighbor math depends on row parity.
- **Axial / cube coords**: cleaner neighbor math but more conversion code.

Offset is simplest for TTY rendering and parses well into lines for height-clamping.

### 4. Navigation: 8 keys → 6 neighbors

Square grid has 8-way movement (4 cardinal in code today, diagonals implicit via wrap). Hex has 6 neighbors. Mapping for pointy-top, offset coords:

```
even row (r,c) neighbors: (r-1,c-1) (r-1,c)   (r,c-1) (r,c+1) (r+1,c-1) (r+1,c)
odd row  (r,c) neighbors: (r-1,c)   (r-1,c+1) (r,c-1) (r,c+1) (r+1,c)   (r+1,c+1)
```

Map arrow/hjkl to: ↑→ up-left/up-right pair, ↓→ down-left/down-right pair, ←/→ same-row. Bias choice (left vs right diagonal on plain ↑) needs PLAN decision. One reasonable convention: ↑/↓ keep same column index when possible (visually-vertical traversal), with ←/→ unchanged.

### 5. Mouse hit-testing

Current `CellAt` is `x/cellW, y/cellH`. Hex needs point-in-polygon. Cheap approximation: test the bounding rectangle, then for the corner-triangles (top-left, top-right, bottom-left, bottom-right) reassign to the diagonally adjacent cell using the slope of the diagonal edge. Acceptable for keyboard-first UI; can ship rectangle-only first if needed.

### 6. Content fit

Hex inner area is narrower than rectangle inner area (diagonal edges eat horizontal space at top/bottom rows). Header line in middle of cell where width is full; preview lines above/below have 2–4 cols less. Reduce `innerW` by ~15% near the diagonal-affected rows, or accept content clipping at corners.

### 7. Frame height invariant

Existing clamp logic at `View()` end will work, but offset-row rendering can produce lines that aren't aligned to the cell grid. Test heavily — easiest to compute total hex grid height up front (`rows*cellH + cellH/2` for the offset bottom of last odd row) and pad/truncate deterministically.

## State / integration

- **No new ViewID needed.** Add `LayoutMode string` ("square" | "hex") field to `GridView`. Toggle via new `h`/`H` case in `handleGridKey()`.
- **Cursor stays linear** (int index into sessions slice). Conversion to (row,col) happens inside hex layout/nav functions.
- **Color system unchanged.** `GradientBg()` reusable for hex header (apply to title segment within hex cell middle row).
- **Persistence:** `LayoutMode` is view-state only — no need to save in AppState/config initially. Could persist preference in config later.
- **Mouse:** dispatch from `CellAt()` based on LayoutMode.

## Constraints / Gotchas

- **Min terminal size:** Hex cells likely need at least ~16 cols × 8 rows each; very narrow terminals (<60 cols) may fit only 2–3 cells per row. Need graceful fallback (force square mode? show single column of hexes? at minimum, don't crash).
- **Diagonal char palette is limited.** Edges will be jaggy; this is a stylistic constraint, not a bug. Accept it.
- **Selected-cell highlighting:** Currently a colored border. For hex, color the diagonal+side chars of the outline. Verify visibility with the recently-added per-session color gradient (#54) — selected hex must remain distinguishable when sessions have varied colors (memory: "session color must be visible when selected").
- **Reorder keys (Shift+Left/Right) — memory note: grid uses horizontal keys for reorder.** In hex this remains intuitive for same-row reorder; cross-row reorder semantics need a PLAN decision.

## Recommendations for PLAN

1. **Single GridView with `LayoutMode` field** — do not split into separate type. Keeps state sync, color updates, and key dispatch unchanged.
2. **Pointy-top, offset coords** — easiest TTY mapping.
3. **Build hex cell as `[]string`** assembled manually — don't fight lipgloss borders.
4. **Ship in two passes if needed:**
   - Pass A: rendering + keyboard nav + toggle. Mouse uses bounding-rect approximation. Square remains default.
   - Pass B: precise point-in-hex mouse, persisted preference, polish on selected-cell highlight under gradient colors.
5. **Test parity:** mirror every existing `gridview_test.go` test for hex mode (height invariant, no-overflow, cursor wrap, selected rendering).

## Open questions for PLAN

- Pointy-top vs flat-top? (Recommend pointy-top.)
- Persist `LayoutMode` across sessions? (Recommend: not initially.)
- ↑/↓ semantics in offset hex — favor same-column or same-diagonal? (Recommend same-column.)
- Hint bar text update (mention `h` toggle) — yes.
- Should `h` only toggle when in hex/square (i.e., reserved when grid is open) — yes; `h` is currently the "left" vim key in `Update()`, so the toggle must be intercepted in `handleGridKey()` *before* delegation. **This conflicts with vim-left navigation.** Options: (a) use only `H` (shift) for toggle, leaving `h` as left-nav; (b) use a different key (`y` for honeycomb? `Tab`?). **Recommend: `H` only**, document in hint bar.
