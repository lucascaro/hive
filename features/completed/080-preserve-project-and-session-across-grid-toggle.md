# Feature: Preserve selected project and session when toggling between grid views (g/G)

- **GitHub Issue:** #80
- **Stage:** DONE
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Branch:** —

## Description

When in the all-projects grid view (`G`) and pressing `g` to switch to the single-project grid, the currently selected project is not preserved — the view doesn't land on the project of the currently selected session. The selection should round-trip: toggling `g` ↔ `G` should always keep the same project and session selected, so the user stays oriented when switching between overview and per-project views.

## Research

The root cause is localized: when pressing `g` in all-projects grid (`GridRestoreAll`) to switch to single-project grid (`GridRestoreProject`), the handler preserves the selected session ID but never updates `AppState.ActiveProjectID` to match that session's project. `gridSessions(GridRestoreProject)` then filters by the stale `ActiveProjectID`, producing sessions from the wrong project. `SyncCursor(prevID)` then fails to find the session in the filtered list and the cursor lands on a default.

### Relevant Code

- `internal/tui/handle_keys.go:102-113` — `case "g"`: when in `GridRestoreAll`, reads `m.gridView.Selected()` for `prevID` and calls `SyncState(gridSessions(GridRestoreProject), ...)`. **Missing: `m.appState.ActiveProjectID = s.ProjectID` before calling `gridSessions`.**
- `internal/tui/handle_keys.go:114-125` — `case "G"`: symmetric path from project grid → all-projects grid. Accidentally works today because `GridRestoreAll` returns all sessions regardless of `ActiveProjectID`, but for correctness `ActiveProjectID` should still be synced (so subsequent grid-exit lands on the right project).
- `internal/tui/helpers.go:131-148` — `gridSessions(mode)`: filters by `ActiveProjectID` when `mode == GridRestoreProject`. This is the code that "sees" the stale project ID.
- `internal/tui/components/gridview.go:46-59` — `GridView` struct; no `ProjectID` field — mode + session list are the source of truth. No changes needed here.
- `internal/tui/components/gridview.go:115-121` — `SyncState(sessions, mode, …, cursorSessionID)` and `SyncCursor(sessionID)` at lines 124-134. Already correct — the upstream caller is what's broken.
- `internal/state/model.go:144-183` — `AppState.ActiveSessionID` and `ActiveProjectID`. `Session.ProjectID` links the two.
- `internal/tui/helpers.go:71-90` — `focusSession(sessionID)` helper already syncs both `ActiveSessionID` and `ActiveProjectID` together (used on grid exit via `popGridState`). Candidate for reuse inside the `g`/`G` handlers so the fix mirrors existing infrastructure from feature #35.

### Existing Test Coverage

- `internal/tui/flow_grid_test.go` → `TestFlow_GridToggleBetweenModes` (lines 230-247) only covers project → all direction and does not assert session preservation.
- `TestFlow_GridExitSyncsActiveProjectID` (lines 500-524) asserts sync on *exit*, not on mid-grid toggle.
- **Gap:** no test covers "open all-projects grid, navigate to session X from project Y, press `g`, assert project-grid shows Y's sessions with X still selected". The new test is the regression net for this fix.

### Constraints / Dependencies

- Fix must preserve current behavior when `m.gridView.Selected()` returns `nil` (edge case — empty grid). In that case, don't mutate `ActiveProjectID`.
- Fix should symmetrically update `ActiveProjectID` on both `g` and `G` so grid state stays consistent even when the G direction appears to work today.
- Don't break `TestFlow_GridExitSyncsActiveProjectID` — exit-time syncing still runs via `popGridState` regardless of what we do on toggle.

### Related Prior Work

- Feature #35 (`features/completed/035-selected-session-persist-across-views.md`, PR #42) established the pattern of syncing `ActiveSessionID` + `ActiveProjectID` together through `focusSession()`. It handled grid-entry and grid-exit but did not address intra-grid mode switching — this fix completes that coverage.

## Plan

See full plan at `/Users/lucascaro/.claude/plans/sequential-churning-dragonfly.md`.

### Approach

Inline two-line fix in both `case "g"` and `case "G"` branches of `internal/tui/handle_keys.go:102-125`. Before calling `gridSessions(...)`, sync `AppState.ActiveSessionID`, `ActiveProjectID`, and `ActiveTeamID` from the currently-selected session so the project filter uses the right project. Skip the mutation when `Selected()` returns `nil` (empty-grid edge case).

Rejected alternative: reusing `focusSession(s.ID)` — does extra UI work (sidebar/preview sync) that's wasted inside grid mode.

### Files to Change

1. **`internal/tui/handle_keys.go:102-125`** — both `g` and `G` branches: when `s := m.gridView.Selected()` is non-nil, set `m.appState.ActiveSessionID = s.ID`, `m.appState.ActiveProjectID = s.ProjectID`, `m.appState.ActiveTeamID = s.TeamID` before `m.gridSessions(...)`.
2. **`internal/tui/flow_grid_test.go`** — add two new flow tests and augment one existing test (see Test Strategy).
3. **`internal/tui/helpers_test.go`** (or create if absent) — unit test for `gridSessions` project-filter behavior.
4. **`CHANGELOG.md`** — `[Unreleased]` → `### Fixed`: "Grid mode toggle preserves selection (#80)".

### Test Strategy

**Flow tests** (`internal/tui/flow_grid_test.go`):

- **`TestFlow_GridAllToProject_PreservesSession` (NEW)** — regression test. Open all-projects grid with `G`, navigate to session-2 (project-2, not the default active project), press `g`. Assert `activeTab == GridRestoreProject`, `ActiveProjectID == "project-2"`, `gridView.Selected().ID == "session-2"`, view contains session-2 and not session-1.
- **`TestFlow_GridProjectToAll_PreservesSession` (NEW)** — symmetric case. Open project grid with `g`, record `initialSel`, press `G`. Assert `gridView.Selected().ID == initialSel.ID` and `ActiveProjectID == initialSel.ProjectID`.
- **`TestFlow_GridToggleBetweenModes` (EXTEND, lines 231-247)** — append round-trip assertion after the existing snapshot: record selection, press `g`, assert selection preserved. Do not disturb the existing `01-all-projects-after-toggle.golden`.

**Unit test** (`internal/tui/helpers_test.go`):

- **`TestUnit_GridSessionsFiltersByActiveProject` (NEW)** — set `m.appState.ActiveProjectID = "project-1"`, assert `gridSessions(GridRestoreProject)` returns only project-1 sessions; switch to `"project-2"`, assert the filter follows. Pins down `gridSessions` behavior independently of the handler.

Run: `go test ./internal/tui/... -run 'Grid.*Preserves|GridToggleBetweenModes|GridSessionsFilters' -v` then full `go test ./...`.

### Risks

1. **Empty grid** — `Selected()` returns `nil`. Guarded by the existing `if s := ... ; s != nil` check.
2. **Standalone sessions** (`TeamID == ""`) — copying `s.TeamID` unconditionally matches what `focusSession` does; clearing `ActiveTeamID` for standalones is intentional.
3. **`gridSessions` fallback-to-all** (helpers.go:142-144) — currently masks some of the bug. Fix reduces how often it triggers; no test regression expected.
4. **Golden file drift** — `01-all-projects-after-toggle.golden` captured before our new assertions; view state at snapshot time unchanged.
5. **Fixture IDs** — plan assumes `project-1`/`session-1` etc. from `testAppStateWithTwoProjects`; will verify and adjust literals if different.

## Implementation Notes

- Fix matched Plan (A) exactly — three-line sync of `ActiveSessionID`/`ActiveProjectID`/`ActiveTeamID` inside each of the `g` and `G` mode-toggle branches at `internal/tui/handle_keys.go:102-125`, guarded by the existing `Selected() != nil` check.
- Fixture IDs matched the plan's assumptions (`proj-1`/`proj-2`, `sess-1`/`sess-2`, titles `session-1`/`session-2`, navigation key `l`). No literal adjustments needed.
- Flow tests had to split `f.Model()` into a local variable before calling `gridView.Selected()` — `Model()` returns a value and `Selected()` has a pointer receiver, so `f.Model().gridView.Selected()` doesn't compile. Matched the existing pattern at `flow_grid_test.go:560`.
- `01-all-projects-after-toggle.golden` snapshot survived unchanged; the new round-trip assertions run *after* the snapshot.
- Added `internal/tui/helpers_test.go` as a new file (was absent) for the `gridSessions` unit test.

- **PR:** —
