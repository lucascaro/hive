# Feature: Persist user preferences (e.g. start in grid mode)

- **GitHub Issue:** #78
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Branch:** —

## Description

The grid-mode toggle resets each session. Add a lightweight user-preferences store so choices like "start in grid mode" persist across reloads.

Consider making it general — a `preferences` object keyed by setting name — rather than a one-off flag, so future toggles (theme, density, default view, etc.) can use the same mechanism.

**Acceptance:**
- Toggling grid mode persists and is restored on next load
- Storage mechanism is reusable for other preferences

## Research

The codebase already has all the infrastructure needed. This is a pure extension of existing patterns.

### Relevant Code

- `internal/config/config.go` — `Config` struct; add `StartInGridMode bool` field here.
- `internal/config/defaults.go` — `DefaultConfig()`; set `StartInGridMode: false` (explicit default).
- `internal/config/migrate.go` — `currentSchemaVersion = 3`; bump to 4 and add a migration block (no data migration needed since bool zero-value is correct, but the version bump keeps the pattern consistent).
- `internal/tui/components/settings.go:736` — `buildSettingTabs()` declarative settings builder; add a `fieldBool` entry to the General tab for "Start in Grid Mode".
- `internal/tui/app.go:207` — `restoreGrid()` opens the grid view from persisted `RestoreGridMode` state; after this call in `New()` (line 192), add logic to also open grid when `cfg.StartInGridMode` is true and the grid wasn't already restored from state.

### How the startup grid logic works

`restoreGrid()` checks `AppState.RestoreGridMode` (set when detaching from a grid session) and clears it after opening. The new preference is orthogonal: if `RestoreGridMode` was already set, honor that (it's a more specific signal); otherwise, if `StartInGridMode` is true, open grid for the current project.

```go
// after m.restoreGrid()
if m.appState.RestoreGridMode == state.GridRestoreNone && m.cfg.StartInGridMode {
    // open grid exactly as 'g' key does
}
```

The `g` key handler (handle_keys.go ~line 264) calls `m.openGrid(state.GridRestoreProject)` — we use the same call.

### Constraints / Dependencies

- No new packages needed.
- No state.json changes needed (preference lives in config.json, not state.json).
- The `StartInGridMode` config field defaults to `false` so existing users are unaffected without migration data changes.

## Plan

### Files to Change

1. `internal/config/config.go` — Add `StartupView string \`json:"startup_view,omitempty"\`` field to `Config` struct after `HideWhatsNew`. Accepted values: `"sidebar"` (default), `"grid"` (current-project grid), `"grid-all"` (all-projects grid).

2. `internal/config/defaults.go` — Add `StartupView: "sidebar"` in `DefaultConfig()`.

3. `internal/config/migrate.go` — Bump `currentSchemaVersion` from 3 to 4. Add migration block: if `cfg.StartupView == ""`, set it to `"sidebar"` so existing configs get the explicit default.

4. `internal/tui/components/settings.go` — In `buildSettingTabs()`, add a `fieldSelect` entry to the General tab:
   ```go
   {
       label:       "Startup View",
       description: "View shown on startup. 'sidebar' opens the normal session list; 'grid' opens the current-project grid; 'grid-all' opens the all-projects grid.",
       kind:        fieldSelect,
       options:     []string{"sidebar", "grid", "grid-all"},
       get:         func(c config.Config) string { return c.StartupView },
       set: func(c *config.Config, v string) error {
           c.StartupView = v
           return nil
       },
   },
   ```

5. `internal/tui/app.go` — In `New()`, after `m.restoreGrid()` (line 192), add:
   ```go
   if m.appState.RestoreGridMode == state.GridRestoreNone {
       switch m.cfg.StartupView {
       case "grid":
           m.openGrid(state.GridRestoreProject)
       case "grid-all":
           m.openGrid(state.GridRestoreAll)
       }
   }
   ```
   `restoreGrid()` clears `RestoreGridMode` after consuming it, so `RestoreGridMode == None` here means no attach-restore already opened the grid. `"sidebar"` and `""` are no-ops (default behavior).

### Test Strategy

- `internal/config/migrate_test.go`:
  - `TestMigrate_V3ToV4_FillsStartupView` — config with `SchemaVersion: 3` and empty `StartupView` gets `"sidebar"` after migration.
  - `TestMigrate_V3ToV4_PreservesExistingStartupView` — config with `StartupView: "grid"` keeps it after migration.

- `internal/tui/flow_grid_test.go`:
  - `TestFlow_StartupView_Grid_OpensProjectGrid` — `cfg.StartupView = "grid"`; assert `AssertGridActive(true)` and `AssertGridMode(GridRestoreProject)` after construction.
  - `TestFlow_StartupView_GridAll_OpensAllGrid` — `cfg.StartupView = "grid-all"`; assert `AssertGridActive(true)` and `AssertGridMode(GridRestoreAll)`.
  - `TestFlow_StartupView_Sidebar_NoGrid` — `cfg.StartupView = "sidebar"`; assert `AssertGridActive(false)` (regression guard).
  - `TestFlow_StartupView_DoesNotConflictWithRestoreGridMode` — `cfg.StartupView = "grid"` AND `appState.RestoreGridMode = GridRestoreAll`; `restoreGrid()` fires first, grid is active in All mode; startup preference does not push a second grid.

- `internal/tui/flow_settings_test.go`:
  - `TestFlow_Settings_StartupViewFieldPresent` — open settings, verify "Startup View" field appears in General tab with options sidebar/grid/grid-all.

### Risks

- **None significant.** Empty string and `"sidebar"` are both no-ops — existing behavior preserved.
- `openGrid()` in `New()` cannot return `scheduleGridPoll()` as a cmd (constructors don't return cmds). This matches `restoreGrid()`'s existing pattern; the preview ticker handles polling once the model runs.

## Implementation Notes

- `StartupView` is a string enum (`"sidebar"`, `"grid"`, `"grid-all"`) rather than the bool originally considered; maps cleanly onto `state.GridRestoreMode`.
- Had to snapshot `hadRestoreGrid` before calling `restoreGrid()` because that function clears `RestoreGridMode` — the original plan assumed checking after the call would work, but it always read `None` post-clear.
- Migration block fills empty `StartupView` with `"sidebar"` for existing users; schema bumped to 4.
- No `scheduleGridPoll()` needed in `New()` — matches `restoreGrid()`'s existing pattern (preview ticker handles it).

- **PR:** —
