# Feature: Per-session color for grid cells

- **GitHub Issue:** #54
- **Stage:** IMPLEMENT
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

Use Option A from research: project color stays as the header background, session color becomes the cell border for unselected cells. Selected cells keep the purple accent border. Auto-assign session colors on creation. Keybinds: `c`/`C` continues to cycle project color forward/backward (unchanged). `v`/`V` cycles session color forward/backward in grid view (new keybind, mnemonic: "visual" color). Both support forward and backward cycling so users can return to a previous color.

### Files to Change

1. **`internal/state/model.go`** — Add `Color string \`json:"color,omitempty"\`` field to `Session` struct (after `AgentCmd` field, line ~132). Empty string = inherit project color for border.

2. **`internal/state/store.go`** — Add `SetSessionColor(state *AppState, sessionID, color string) *AppState` function (mirrors `SetProjectColor` at line 184). Walks all projects' sessions and team sessions to find and set the color.

3. **`internal/tui/styles/theme.go`** — Add `NextFreeSessionColor(projectColor string, usedColors []string) string` that picks the first palette color not in `usedColors` and not equal to `projectColor`. This ensures session colors differ from each other and from the project color.

4. **`internal/tui/operations.go`** — Add `cycleSessionColor(sessionID string, direction int)` method on `Model`. Collects colors used by sibling sessions in the same project, calls `styles.CycleColor()`, calls `state.SetSessionColor()`, then `commitState()`.

5. **`internal/tui/helpers.go`** — Add `gridSessionColors() map[string]string` that builds a `sessionID→hex color` map from all sessions across all projects (include team sessions). Return only sessions that have a non-empty `Color`.

6. **`internal/tui/components/gridview.go`**:
   - Add `sessionColors map[string]string` field to `GridView` struct (line ~57).
   - Add `SetSessionColors(colors map[string]string)` method (mirrors `SetProjectColors`).
   - In `renderCell()` (line 230): look up `gv.sessionColors[sess.ID]`. If non-empty, use it as `borderColor` for unselected cells (instead of `styles.ColorBorder`). Selected cells keep `styles.ColorAccent`.

7. **`internal/tui/viewstack.go`** — In `refreshGrid()` (line ~164) and `openGrid()` (line ~176), add `m.gridView.SetSessionColors(m.gridSessionColors())` after the existing `SetProjectColors` call.

8. **`internal/tui/handle_keys.go`** — Add a new `"v", "V"` case in `handleGridKey` (after the existing `"c", "C"` case at line 162):
   - `v` → `m.cycleSessionColor(sess.ID, +1)` (forward)
   - `V` → `m.cycleSessionColor(sess.ID, -1)` (backward)
   - After cycling, call `m.gridView.SetSessionColors(m.gridSessionColors())`.
   - Also add `SetSessionColors` after reorder moves (lines 86, 98) and in all other places that call `SetProjectColors`.

9. **`internal/tui/operations.go`** — In `createSession()` (line 169) and `spawnWorktreeSession()` (line 113) and `addTeamSession()` (line 230): after `CreateSession`/`AddTeamSession`, auto-assign a session color by collecting sibling session colors and calling `styles.NextFreeSessionColor(proj.Color, usedColors)`, then setting `sess.Color`.

### Test Strategy

**Unit tests:**
- **`internal/tui/styles/theme_test.go`** — `TestNextFreeSessionColor`: verify it returns a palette color that is not the project color and not in the used list. `TestNextFreeSessionColor_AllUsed`: verify it still returns a color when all palette slots are taken.
- **`internal/state/store_test.go`** — `TestSetSessionColor_Standalone`: verify setting color on a standalone session. `TestSetSessionColor_TeamSession`: verify setting color on a session inside a team.
- **`internal/tui/components/gridview_test.go`** — `TestGridView_SetSessionColors`: verify session colors are stored and retrievable. `TestGridView_RenderCell_SessionColorBorder`: render an unselected cell with a session color and verify the border uses that color, not the default gray. `TestGridView_RenderCell_SelectedOverridesSessionColor`: render a selected cell with a session color and verify the border uses the accent color, not the session color.

**Functional (flow) tests** (using the `flowRunner` pattern in `internal/tui/flow_color_test.go`):
- **`TestFlow_SessionColorCycle_GridView`**: open grid, press `v`, verify the selected session's `Color` field changed from its initial value.
- **`TestFlow_SessionColorCyclePrev_GridView`**: open grid, press `v` then `V`, verify cycling backward returns to the initial color.
- **`TestFlow_SessionColorCycle_SkipsSiblingColors`**: create a project with 3 sessions with pre-assigned colors, cycle one — verify it skips colors already used by siblings.
- **`TestFlow_SessionColor_AutoAssignedOnCreate`**: create a new session via the grid `t` key flow, verify the new session has a non-empty `Color` field that differs from existing sibling session colors.
- **`TestFlow_ProjectColorCycle_UnchangedInGrid`**: open grid, press `c`, verify project color still cycles (existing behavior not broken).

### Risks
- With 10 palette colors and many sessions, colors will repeat — acceptable.
- `v`/`V` is a new keybind — users need to discover it. Will be shown in the grid footer hints.

## Implementation Notes

Implemented exactly as planned (Option A). No deviations.

- Session `Color` field added to the `Session` struct with `json:"color,omitempty"` for backward compatibility.
- `NextFreeSessionColor` skips both the project color and sibling session colors.
- `v`/`V` keybinds cycle session color forward/backward in grid view; `c`/`C` continues to cycle project color (unchanged).
- Auto-assign happens in `createSession`, `createSessionWithWorktree`, and `addTeamSession`.
- Session color is used as the border color for unselected grid cells; selected cells keep the purple accent border.
- Golden files updated for the new hint line in the grid footer.

- **PR:** #70
