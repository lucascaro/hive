# Feature: Code refactor: remove bloat

- **GitHub Issue:** #37
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P5
- **Branch:** —

## Description

Review and clean up the codebase to remove unnecessary complexity, dead code, and bloat. Simplify where possible without changing behavior.

## Research

Deep research doc: [`research/code-refactor/RESEARCH.md`](../../research/code-refactor/RESEARCH.md)

### Relevant Code

**Dead code to remove:**
- `internal/tmux/capture.go:86-99` — `GetPaneActivity()` unused export
- `internal/tmux/window.go:56-59` — `SendKeys()` unused export
- `internal/state/store.go:539-545` — `SessionLabel()` only used in tests, not production

**Large files to split/simplify (3,726 lines combined):**
- `internal/tui/handle_keys.go` (824 lines) — `handleGlobalKey()` is 331 lines with 26 case branches
- `internal/tui/components/settings.go` (719 lines) — `buildSettingEntries()` is 184 lines of repetitive closures
- `internal/tui/app.go` (639 lines) — Model struct has 70+ fields mixing concerns
- `internal/tui/operations.go` (547 lines) — 9+ identical commit+rebuild patterns
- `internal/tui/components/gridview.go` (539 lines) — `renderCell()` is 159 lines
- `internal/tui/components/sidebar.go` (458 lines) — rebuild + render entangled

**Key duplication patterns:**
- Grid view sync (5-line block repeated 4x in `handle_keys.go`)
- State commit+rebuild (2-line block repeated 9x in `operations.go`)
- Sidebar navigation (4 identical cases in `handle_keys.go:494-548`)
- Color cycling (2 near-identical functions in `operations.go:35-75`)
- Move up/down (6 similar functions in `state/store.go:312-408`, ~140 lines)
- Picker navigation (shared between `orphanpicker.go` and `recoverypicker.go`)
- Worktree session setup (duplicated in grid view and global keys)

**AI slop patterns:**
- Section separator comments (`// --- Name ---`) — 50+ in production and test code
- Unnecessary `else` after early return — 52 occurrences across 22 files
- Thin getter/setter methods — 15+ methods that just assign/return a field
- Verbose comments restating function names — scattered in `tmux/`, `git/`, etc.
- Boolean toggle anti-pattern in `settings.go:177-184`

### Constraints / Dependencies
- Must not change behavior — pure refactor
- All refactoring must pass existing test suite
- Complexity L — should be broken into multiple PRs (dead code, AI slop, duplication, file splitting)

## Plan

Split into **5 sequential PRs** to keep reviews focused and diffs reversible. Each PR must pass `go test ./...` before merge.

### PR 0: Test coverage safety net (DONE)

Add missing tests for code that will be refactored, so we have a solid safety net before touching anything.

**Sidebar unit tests added** (`internal/tui/components/sidebar_test.go`):
- `TestSidebarMoveUp` — basic navigation + boundary at top
- `TestSidebarJumpPrevProject` — jump between projects + no-op at first
- `TestSidebarJumpNextProject` — jump between projects + no-op at last

**Flow tests added** (`internal/tui/flow_reorder_test.go`):
- `TestFlow_NavProjectDown_JumpsToNextProject` — J key jumps to next project
- `TestFlow_NavProjectUp_JumpsToPrevProject` — K key jumps to prev project
- `TestFlow_NavProjectDown_NoOp_AtLastProject` — boundary: no-op at last
- `TestFlow_NavProjectUp_NoOp_AtFirstProject` — boundary: no-op at first

**Flow tests added** (`internal/tui/flow_session_test.go`):
- `TestFlow_SidebarNewWorktreeSession` — W key from sidebar opens agent picker with worktree mode

**Already well-covered (no new tests needed):**
- `cycleProjectColor` / `cycleSessionColor` — 7 flow tests in `flow_color_test.go`
- `createProject` — full flow test in `flow_project_test.go`
- Grid reorder — flow tests in `flow_reorder_test.go`
- Grid worktree — `TestFlow_GridNewWorktreeSession` in `flow_grid_test.go`
- `MoveSession/Team/ProjectUp/Down` — 18 unit tests in `state/store_test.go`
- Settings boolean toggle — `TestSettingsView_BoolToggle` in `settings_test.go`

### PR 1: Dead code removal + AI slop cleanup

Quick wins — no behavioral change, small diff, high confidence.

1. `internal/tmux/capture.go` — Delete `GetPaneActivity()` (lines 86-99)
2. `internal/tmux/window.go` — Delete `SendKeys()` (lines 56-59)
3. `internal/state/store.go` — Delete `SessionLabel()` (lines 539-545); update `store_test.go` to remove the test for it
4. **All `.go` files** — Remove `// --- Section name ---` separator comments (~50 occurrences in production code, ~30+ in test code)
5. **All `.go` files** — Remove verbose comments that restate the function name (e.g., `CreateSession` documented as "creates a new tmux session"). Keep comments that explain *why* or document non-obvious behavior.
6. `internal/tui/components/settings.go:177-184` — Simplify boolean toggle to `_ = f.set(&sv.cfg, strconv.FormatBool(cur != "true"))`

### PR 2: Duplication consolidation (helpers)

Extract repeated patterns into helpers. Behavioral no-op.

1. `internal/tui/operations.go` — Add `saveAndRebuild()` helper that calls `m.commitState()` + `m.sidebar.Rebuild(&m.appState)`. Replace all 9+ call sites.
2. `internal/tui/components/gridview.go` — Add `SyncState(sessions, projectNames, projectColors, sessionColors)` method that combines `Show()` + `SetProjectNames()` + `SetProjectColors()` + `SetSessionColors()` + `SyncCursor()`. Remove the 5 individual thin setters (`SetProjectNames`, `SetProjectColors`, `SetSessionColors`, `SetPaneTitles`, `SetContents`) — fold into `SyncState` + direct field assignment where called individually.
3. `internal/tui/handle_keys.go` — Replace 4 repetitions of the 5-line grid sync block (lines 86-90, 99-102, 118-120, 137-139) with calls to the new `GridView.SyncState()`.
4. `internal/tui/handle_keys.go` — Extract sidebar navigation helper `navigateSidebar(moveFn func())` to consolidate the 4 identical NavUp/NavDown/NavProjectUp/NavProjectDown cases (lines 494-548).
5. `internal/tui/handle_keys.go` — Extract `initWorktreeSession(projectID)` to deduplicate the worktree setup at lines 188-207 (grid) and 314-341 (global).
6. `internal/tui/operations.go` — Consolidate `cycleProjectColor()` and `cycleSessionColor()` into a parameterized `cycleColor(id string, direction int, getColor func() string, setColor func(string), excludeColors func() []string)` or similar.

### PR 3: State store deduplication

7. `internal/state/store.go` — Replace the 6 `Move*Up/Down` functions (lines 312-408) with a generic helper. The pattern is: find item by ID in a slice, swap with adjacent. Extract `swapAdjacent[T](slice []*T, matchFn func(*T) bool, direction int) bool` and rewrite all 6 functions as thin wrappers calling it.

### PR 4: Unnecessary `else` cleanup

8. **22 files, 52 occurrences** — Convert `if cond { return X } else { ... }` to early-return style: `if cond { return X }; ...`. Focus on the worst offenders first:
   - `internal/tui/handle_keys.go` (8 instances)
   - `internal/tui/app.go` (8 instances)
   - `internal/tui/components/settings.go` (4 instances)
   - `internal/tui/components/gridview.go` (4 instances)
   - `internal/tui/viewstack.go` (4 instances)
   - Remaining 17 files (1-3 instances each)

### Files to Change

| PR | File | What |
|----|------|------|
| 0 | `internal/tui/components/sidebar_test.go` | Add MoveUp, JumpPrev/NextProject tests |
| 0 | `internal/tui/flow_reorder_test.go` | Add NavProjectUp/Down flow tests |
| 0 | `internal/tui/flow_session_test.go` | Add sidebar worktree session flow test |
| 1 | `internal/tmux/capture.go` | Delete `GetPaneActivity` |
| 1 | `internal/tmux/window.go` | Delete `SendKeys` |
| 1 | `internal/state/store.go` | Delete `SessionLabel` |
| 1 | `internal/state/store_test.go` | Delete `SessionLabel` test |
| 1 | ~80 `.go` files | Remove separator comments + redundant doc comments |
| 1 | `internal/tui/components/settings.go` | Simplify boolean toggle |
| 2 | `internal/tui/operations.go` | Add `saveAndRebuild()`, consolidate color cycling |
| 2 | `internal/tui/components/gridview.go` | Add `SyncState()`, remove thin setters |
| 2 | `internal/tui/handle_keys.go` | Use `SyncState()`, extract `navigateSidebar()`, extract `initWorktreeSession()` |
| 3 | `internal/state/store.go` | Generic `swapAdjacent` helper for Move functions |
| 3 | `internal/state/store_test.go` | Update tests if function signatures change |
| 4 | 22 `.go` files | Remove unnecessary `else` blocks |

### Test Strategy

This is a pure refactor — **existing tests + PR 0's new tests are the primary verification**. All PRs must pass `go test ./...` with zero failures. PR 0 adds the safety net; PRs 1-4 introduce no new behavior. Specific checks:

- **PR 1 (dead code):** `go test ./internal/state/... ./internal/tmux/...` — verify `store_test.go` still passes after removing `SessionLabel` test. Verify `go build ./...` succeeds (no compilation errors from removed exports).
- **PR 2 (helpers):** `go test ./internal/tui/...` — all existing flow tests (`flow_reorder_test.go`, `flow_session_test.go`, `flow_grid_test.go`, `app_test.go`) exercise the grid sync and sidebar navigation paths that are being refactored. The `flow_reorder_test.go` tests specifically cover `MoveSessionUp/Down` through the UI, verifying the `SyncState()` consolidation works.
- **PR 3 (state store):** `go test ./internal/state/...` — `store_test.go` has dedicated tests for all 6 Move functions (lines 785-928) that verify swap behavior and boundary conditions.
- **PR 4 (else cleanup):** `go test ./...` — full suite, since changes span 22 files.

### Risks

- **PR 2 is the riskiest** — changing the `GridView` API (consolidating setters into `SyncState`) touches the most call sites. If any caller needs to set colors without a full sync, the consolidation breaks. Mitigate by auditing every call site before merging.
- **PR 3 generics** — `swapAdjacent` needs Go generics or interface-based approach. Sessions are found via nested iteration (projects→teams→sessions), which doesn't fit a simple slice swap. May need to keep `MoveSessionUp/Down` with shared inner logic rather than a fully generic helper.
- **PR 1 separator comments** — mechanical removal across 80 files creates a large diff. Use `sed`/script rather than manual edits to reduce human error. Review diff carefully to ensure no meaningful comments are removed.
- **PR 4 else removal** — some `else` blocks may be intentional for readability (e.g., symmetric if/else). Apply judgment, don't blindly convert all 52.

## Implementation Notes

All 5 planned PRs were combined into a single PR for simplicity since changes are interdependent.

**PR 0 (test safety net):** Committed as-is from prior planning stage.

**PR 1 (dead code + AI slop):**
- Deleted `GetPaneActivity`, `SendKeys`, `SessionLabel` and their tests
- Removed unused imports (`time`, `strconv`, `fmt`, `strings`) from affected files
- Removed ~53 section separator comments across 15 files
- Removed ~41 redundant doc comments across 9 files
- Simplified boolean toggle in settings.go using `strconv.FormatBool`
- Fixed redundant `sidebar.Rebuild` calls after `commitState` (which already rebuilds)

**PR 2 (duplication consolidation):**
- Added `GridView.SyncState()` that combines `Show` + set names/colors + `SyncCursor`
- Added `Model.syncGridState()` convenience wrapper
- Replaced 8+ repetitions of the 5-line grid sync block across handle_keys.go, viewstack.go, app.go, golden_test.go
- Extracted `navigateSidebar(moveFn)` to deduplicate 4 NavUp/Down/ProjectUp/Down cases + 2 mouse scroll cases. Uses `func(*Sidebar)` signature to avoid value-receiver copy issues.
- Extracted `initWorktreeSession(projectID)` to deduplicate grid-W and sidebar-W handlers
- Skipped color cycling consolidation — the two functions have different enough logic that a generic helper would reduce clarity

**PR 3 (state store dedup):**
- Added generic `swapAdjacent[T any]` helper using Go generics
- Consolidated 6 Move*Up/Down functions into 3 directional wrappers (`moveSession`, `moveTeam`, `moveProject`)

**PR 4 (else cleanup):**
- Investigated all 47 `else` blocks in production code — none qualify for removal. All are symmetric if/else, scoped assignments, or else-if chains. The original 52-count estimate was over-counted.

- **PR:** —
