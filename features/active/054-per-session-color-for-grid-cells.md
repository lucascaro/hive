# Feature: Per-session color for grid cells

- **GitHub Issue:** #54
- **Stage:** PLAN
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P5
- **Branch:** —

## Description

In grid view, all sessions within a project look the same — only the project color is used. With many sessions in a single project it's hard to tell them apart at a glance.

Allow setting a color per session. Use both the project color and session color together in grid cells (e.g. project color as border, session color as background, or vice-versa) so sessions within the same project are visually distinguishable.

## Research

### Architecture Overview

Sessions currently inherit color from their parent project. The `Session` struct has no `Color` field — grid cells use `gv.projectColors[sess.ProjectID]` to look up the project color. The feature needs to add a per-session color that works alongside the project color.

### Relevant Code

**Data model:**
- `internal/state/model.go:121-139` — `Session` struct. Needs a new `Color string` field. Already has a `Meta map[string]string` but a dedicated field is cleaner for rendering hot-paths.
- `internal/state/model.go:92-104` — `Project` struct with existing `Color string` field (line 96). Session color follows same pattern.

**Grid cell rendering:**
- `internal/tui/components/gridview.go:229-361` — `renderCell()` is the key function. Lines 253-264 apply the project color as the header background. This is where session color needs to be integrated — e.g. project color stays on the header, session color becomes the border or a tint.
- `internal/tui/components/gridview.go:86-88` — `SetProjectColors()` provides the `projectID→color` map. Need an analogous `SetSessionColors()` or pass session colors inline.
- `internal/tui/components/gridview.go:230-236` — Border color logic (purple for selected, gray otherwise). Session color could replace gray for unselected borders.

**Color palette & cycling:**
- `internal/tui/styles/theme.go:120-131` — `ProjectPalette` (10 colors). Can reuse for sessions or define a separate lighter/pastel palette for session-level differentiation.
- `internal/tui/styles/theme.go:134-151` — `NextFreeColor()`, `NextProjectColor()` — reusable patterns for session color assignment.
- `internal/tui/styles/theme.go:153-167` — `CycleColor()` — can reuse for cycling session colors with `c`/`C` in grid when a session (not project header) is selected.

**Color wiring (project → grid):**
- `internal/tui/helpers.go:174-180` — `gridProjectColors()` builds the color map. Need a parallel `gridSessionColors()`.
- `internal/tui/viewstack.go:164,176` — `refreshGrid()` and `openGrid()` push colors into grid. Must also push session colors.
- `internal/tui/handle_keys.go:162-170` — Grid-mode `c`/`C` handler calls `cycleProjectColor()`. Need to distinguish: if a session is selected, cycle session color instead.

**Persistence:**
- `internal/tui/persist.go:49-89` — `saveState()` marshals `appState.Projects` (which includes sessions) as JSON. A new `Color` field on `Session` will persist automatically.
- `internal/tui/persist.go:125-132` — `migrateProjectColors()` assigns colors to projects missing them. May want a similar migration for sessions (default to empty = "no session color").

**Color cycling operations:**
- `internal/tui/operations.go:35-51` — `cycleProjectColor()`. Need a parallel `cycleSessionColor()`.

**Sidebar (optional, lower priority):**
- `internal/tui/components/sidebar.go:81-152` — `Rebuild()` sets `SidebarItem.ProjectColor`. Could add session color to the sidebar gutter stripe too, but this is a nice-to-have.

### Design Considerations

**How to combine project + session colors in grid cells:**
- **Option A (recommended):** Project color stays as the header background. Session color becomes the cell border (currently gray for unselected, purple for selected). Unselected cells get `sessionColor` border; selected cells keep purple. Simple, clear visual hierarchy.
- **Option B:** Project color as border, session color as header background. Inverts current meaning — riskier for existing users.
- **Option C:** Session color as a small accent stripe (left edge or dot). Minimal but may be too subtle.

**Color assignment strategy:**
- Auto-assign on session creation using a session-scoped `NextFreeColor` within the project (so sessions in the same project get distinct colors).
- Allow cycling with `c`/`C` when a session cell is selected in grid view (reuse existing cycling UX).
- Empty/unset session color = fall back to project color (backward compatible).

### Constraints / Dependencies
- Must be backward compatible — existing `state.json` files with no session color should work (empty string = use project color).
- The color palette (10 colors) is shared. With many sessions in one project, colors will repeat — acceptable given palette size.
- Selected-cell border must remain visually distinct (keep purple accent for selection regardless of session color).
- Complexity is S because the plumbing already exists for project colors — session colors follow the same pattern.

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
