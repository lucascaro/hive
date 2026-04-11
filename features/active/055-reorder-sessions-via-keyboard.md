# Feature: Reorder sessions via keyboard

- **GitHub Issue:** #55
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P4
- **Branch:** feature/55-reorder-sessions-via-keyboard

## Description

Sessions are displayed in creation order with no way to rearrange them. Users may want to group or prioritize sessions visually without creating separate projects or teams.

Allow reordering sessions within their group (standalone list or team) using keyboard shortcuts (e.g. Shift+Up/Down or similar). The new order should persist across restarts.

## Research

Session order is implicit in slice position (`Project.Sessions` and `Team.Sessions` slices in `internal/state/model.go:92-139`). No explicit "order" field exists — position in the slice *is* the order. Both sidebar and grid view render sessions in slice order without sorting.

Persistence is automatic: `saveState()` (`internal/tui/persist.go:49-88`) marshals slices to JSON, preserving order. Any reorder + `commitState()` call persists immediately. No additional persistence work needed.

No backend changes required — reordering is purely a Hive state operation. Tmux/native backends don't need to know about display order.

### Relevant Code
- `internal/state/model.go:92-139` — Project and Session structs; order is implicit in slice index
- `internal/state/store.go:54,96` — `CreateSession` / `AddTeamSession` append to slices; no reorder primitives exist yet
- `internal/tui/components/sidebar.go:78-165` — `Rebuild()` traverses Projects → Teams → Sessions in slice order to build flat item list
- `internal/tui/components/sidebar.go:214-228` — `MoveUp()`/`MoveDown()` move cursor, not sessions
- `internal/tui/components/gridview.go:61-71` — `Show()` receives `[]*state.Session` in slice order
- `internal/tui/components/gridview.go:170-227` — `View()` renders cells in slice order
- `internal/tui/handle_keys.go:74-175` — `handleGridKey()` — grid key handler
- `internal/tui/handle_keys.go:198-511` — `handleGlobalKey()` — global/sidebar key handler
- `internal/tui/keys.go:9-38` — `KeyMap` struct with all key bindings
- `internal/tui/app.go:473-488` — `commitState()` and `persist()` — persistence after mutations

### Constraints / Dependencies
- `Shift+Up`/`Shift+Down` are available — no current bindings use shift+arrow
- #40 ("Reorder sessions within a project") is a broader version of this feature (L complexity, includes drag-and-drop and cross-project moves). #55 is the keyboard-only subset and can ship independently as a stepping stone
- `h`/`j`/`k`/`l` are used for cursor navigation in grid view — reorder keys must use modifiers to avoid conflict

## Plan

Reorder items (sessions, teams, projects) using `Shift+Up`/`Shift+Down`. The move target depends on what's selected in the sidebar:
- **Session** → swap within its containing slice (`Project.Sessions` or `Team.Sessions`)
- **Team** → swap within `Project.Teams`
- **Project** → swap within `AppState.Projects`

Works in both sidebar and grid views (grid only moves sessions). Cursor follows the moved item.

### Files to Change

1. **`internal/config/config.go`** — Add `MoveUp` and `MoveDown` fields to `KeybindingsConfig`
2. **`internal/config/defaults.go`** — Set defaults: `MoveUp: "shift+up"`, `MoveDown: "shift+down"`
3. **`internal/state/store.go`** — Add reducer functions:
   - `MoveSessionUp(state, sessionID) *AppState` / `MoveSessionDown(state, sessionID) *AppState` — find session in its containing slice, swap with adjacent element. No-op at boundary.
   - `MoveTeamUp(state, teamID) *AppState` / `MoveTeamDown(state, teamID) *AppState` — swap team within `Project.Teams`. No-op at boundary.
   - `MoveProjectUp(state, projectID) *AppState` / `MoveProjectDown(state, projectID) *AppState` — swap project within `AppState.Projects`. No-op at boundary.
4. **`internal/state/store_test.go`** — Unit tests for all six reducers (see Test Strategy)
5. **`internal/tui/keys.go`** — Add `MoveUp` and `MoveDown` to `KeyMap` struct; wire them in `NewKeyMap()` from config
6. **`internal/tui/handle_keys.go`** — In `handleGlobalKey()` (sidebar): on `MoveUp`/`MoveDown`, inspect `sidebar.Selected().Kind` to dispatch to the correct reducer (session/team/project), then `commitState()`, rebuild sidebar, sync cursor to the moved item. In `handleGridKey()`: move the selected session, refresh grid session list, `SyncCursor(sessionID)`.
7. **`internal/tui/flow_reorder_test.go`** — Functional tests using `flowRunner` (see Test Strategy)
8. **`docs/keybindings.md`** — Document the new `Shift+Up`/`Shift+Down` bindings
9. **`CHANGELOG.md`** — Add entry under `[Unreleased]` → `Added`

### Test Strategy

**Unit tests (`internal/state/store_test.go`):**
- `MoveSessionUp`/`Down`: move middle standalone session, move first (no-op), move last (no-op), single session (no-op), team session swap
- `MoveTeamUp`/`Down`: move middle team, boundary no-ops, single team
- `MoveProjectUp`/`Down`: move middle project, boundary no-ops, single project

**Functional tests (`internal/tui/flow_reorder_test.go`):**
- `TestFlow_MoveSessionDown_Sidebar`: create project with 3 sessions → select first → Shift+Down → verify order changed and cursor followed
- `TestFlow_MoveSessionUp_Sidebar`: select last session → Shift+Up → verify swap
- `TestFlow_MoveSession_BoundaryNoop`: select first session → Shift+Up → verify no change
- `TestFlow_MoveProjectDown_Sidebar`: create 2 projects → select first project row → Shift+Down → verify project order swapped
- `TestFlow_MoveSession_GridView`: open grid → Shift+Down → verify grid reflects new order and cursor follows
- `TestFlow_MoveTeamDown_Sidebar`: project with 2 teams → select first team → Shift+Down → verify swap

### Risks
- **Terminal compatibility**: Some terminals may not emit `shift+up`/`shift+down` distinctly (e.g. over SSH). Mitigated by making bindings configurable via `KeybindingsConfig`.
- **Grid cursor drift**: After reorder in grid view, cursor index must follow the swapped session. Handled by calling `SyncCursor(sessionID)` after refreshing the grid session list.
- **Sidebar cursor drift**: After moving a project or team, the sidebar is rebuilt from scratch. Must `SyncActiveSession` or manually set cursor to the moved item's new index.

## Implementation Notes

- Grid move keys use `key.Matches()` before the `switch msg.String()` block to avoid hardcoding key strings and respect configurable bindings
- Added `sessionIndex()` helper in store.go to find a session's position in a slice
- Sidebar cursor sync after move uses different strategies per item kind: `SyncActiveSession` for sessions, manual item search for teams and projects
- No deviations from the plan; all six reducer functions, key wiring, and tests implemented as specified

- **PR:** —
