# Feature: Improve selected session visibility in grid view

- **GitHub Issue:** #68
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Branch:** feature/68-grid-selected-visibility

## Description

In grid view, the visual difference between the selected cell and unselected cells is too subtle — the only distinction is a purple vs gray border. With many sessions open, it's hard to tell at a glance which one is selected.

Improve the selected cell's visual treatment to make it stand out more clearly. Possible approaches include a thicker or double border, a background tint, a glow/highlight effect, or a bold header — while keeping the design clean.

## Research

### Relevant Code

- `internal/tui/components/gridview.go:235-381` — `renderCell()` method. The selected cell is distinguished **only** by border color: `ColorAccent` (#7C3AED purple) vs `ColorBorder` (#374151 gray). The border style itself (rounded, single-line) is identical. Title gets `Bold(true)` when selected (line 314), but only in the flat-header path (no session color gradient). No background tint, no glow, no thickness change.
- `internal/tui/components/gridview.go:176-233` — `View()` method. Composes grid rows from `renderCell()` outputs. No per-cell wrapper or highlight layer exists.
- `internal/tui/styles/theme.go:12-23` — Color constants. Key values:
  - `ColorAccent` (#7C3AED) — selected border
  - `ColorBorder` (#374151) — unselected border
  - `ColorGridSelected` (#151520) — used in sidebar for row background but **not used in grid view at all**
  - `ColorBg` (#111827) — base background
- `internal/tui/styles/theme.go:47-50` — `PreviewFocusedStyle` shows the pattern for focused border (accent color on rounded border) — grid cells follow the same pattern.
- `internal/tui/styles/theme.go:79-81` — `SessionSelectedStyle` uses `ColorSelected` background in sidebar. Grid view does not use this.
- `internal/tui/components/sidebar.go:415-418` — Sidebar selected row gets a `ColorSelected` (#1E3A5F dark blue) background + lighter text. Much more visible than the grid's border-only approach.
- `internal/tui/styles/theme.go:254-309` — `GradientBg()` / `GradientFg()` functions used for per-session color rendering. The `GradientBg` function already receives a `bold bool` parameter (used when selected).

### Constraints / Dependencies

- **lipgloss border limitations:** lipgloss does not support double/thick borders or border width. The `RoundedBorder()`, `NormalBorder()`, `ThickBorder()` are the available options, but they all render as single-character-width borders.
- **Per-session color (PR #70):** Recently merged. The header already uses gradient backgrounds for sessions with custom colors. Any new selected-cell styling must not conflict with or obscure the gradient header.
- **Content preview area:** The cell body shows terminal output preview. Session output is mostly light text on dark/black backgrounds, so a dark background tint on the content area is safe. A new color `ColorGridSelected` (#151520) will be used — a near-black with a slight purple lean, much darker than the sidebar's `ColorGridSelected` (#151520). Subtle enough to not distract from terminal content while remaining distinguishable from a transparent background. All standard ANSI foreground colors remain legible. Apply to content area only, not the header (which already has project/session color). Edge case: sessions with blue background output — rare in practice.
- **Minimal diff preferred:** This is an S-complexity feature. The fix should be surgical — ideally just a few lines in `renderCell()` and possibly `theme.go`.

## Plan

Add a `ColorGridSelected` (#151520 — near-black with slight purple lean) background tint to the **content preview area** and **subtitle line** of the selected grid cell. The header is left untouched (it already has project/session color). Combined with the existing accent border and bold title, this gives three reinforcing cues for the selected cell.

### Files to Change

1. `internal/tui/components/gridview.go` — In `renderCell()`:
   - **Content area (lines 359-362, 364-368):** When `selected`, add `.Background(styles.ColorGridSelected)` to the content lipgloss style in both the has-content and empty-content branches.
   - **Subtitle line (lines 334-338):** When `selected`, add `.Background(styles.ColorGridSelected)` to the subtitle lipgloss style.

### Test Strategy

**Unit tests** in `internal/tui/components/gridview_test.go`:
1. `TestGridView_SelectedCellHasBackground` — Render a 2-session grid, verify the selected cell's output contains the ANSI escape for `ColorGridSelected` (#151520) background, and the unselected cell does not.
2. `TestGridView_SelectedCellSubtitleHasBackground` — Render a selected cell with a tall enough height (≥8) and a pane title set, verify the subtitle line includes the `ColorSelected` background.

**Golden tests** in `internal/tui/golden_test.go`:
3. Update golden snapshots — the existing `TestGolden_GridView_ProjectScope` and `TestGolden_GridView_AllProjects` will need their `.golden` files regenerated since the selected cell rendering changes.

**Flow tests** — No new flow tests needed. The change is purely visual (styling), not behavioral. Existing flow tests (`TestFlow_GridNavigateAndSelect`, `TestFlow_GridAttachDetachRestoresGrid`, etc.) already verify selection mechanics and will continue to pass since the logic is unchanged.

### Risks

- **ANSI color stacking:** If terminal content already has background colors set via ANSI escapes, the lipgloss `Background()` may not override them per-character — it sets a default background. In practice this is fine: most terminal output uses default background, and characters with explicit backgrounds will keep their own color, which is acceptable.
- **Golden test churn:** Two golden files need updating. This is expected and low-risk.
- **Contrast edge case:** Sessions outputting blue-backgrounded text may blend slightly with `ColorGridSelected` (#151520). This is rare and the border+bold title still provide selection cues.

## Implementation Notes

- Added `ColorGridSelected` (#151520) to `theme.go` — separate from sidebar's `ColorSelected`
- Applied background to both content area and subtitle line in `renderCell()` via a shared `contentStyle`
- Tests use `lipgloss.SetColorProfile(termenv.TrueColor)` to force color output since test runners don't have a TTY
- Golden tests unaffected (use `TERM=dumb` which strips all color)
- No deviations from plan

- **PR:** #71
