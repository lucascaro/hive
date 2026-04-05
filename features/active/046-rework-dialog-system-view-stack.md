# Feature: Rework dialog system to use a view stack

- **GitHub Issue:** #46
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P3
- **Branch:** —

## Description

Dialogs currently have custom logic to decide which view to return to after they close. This leads to bugs — for example, renaming a session while in grid view incorrectly returns to the main view instead of back to the grid.

Rework the dialog system to use a view stack:

- When a dialog opens, it is **pushed** on top of the current view
- When a dialog closes, it is **popped** and the user returns to whatever view was underneath
- No per-dialog custom "return to" logic — the stack handles it automatically

This would fix the rename-in-grid-view bug and prevent similar navigation issues in the future.

## Research

### Current Architecture

The app has **no centralized view/mode tracking**. Instead, it uses a priority-ordered cascade of boolean flags and `Active` fields to determine what's shown. The `View()` method checks each overlay in priority order and renders the first active one.

**View state is distributed across:**
- `AppState` flags: `ShowHelp`, `ShowTmuxHelp`, `ShowConfirm`, `EditingTitle`, `FilterActive` — in `internal/state/model.go:143-182`
- Component `Active` fields: `gridView.Active`, `settings.Active`, `agentPicker.Active`, `teamBuilder.Active`, `dirPicker.Active`, `recoveryPicker.Active`, `orphanPicker.Active` — in `internal/tui/app.go:39-84`
- `inputMode` string: `"project-name"`, `"project-dir-confirm"`, `"custom-command"`, `"worktree-branch"`, `"new-session"` — in `internal/tui/app.go:56`

**Key handlers use the same priority cascade** (`buildKeyHandlers()` at `handle_keys.go:35-166`): the first handler whose `Focused()` returns true gets exclusive key input.

**View rendering** (`app.go:296-355`) checks overlays in the same order: settings > grid > help > attach hint > confirm > pickers > inputs > rename > main layout.

### The Bug: Grid → Rename → Returns to Main

When pressing `r` in grid view (`handle_keys.go:193-199`):
1. `m.gridView.Hide()` — grid is closed immediately
2. `m.startRename()` — rename dialog opens
3. When rename completes (`handle_input.go:175-199`), it just sets `EditingTitle = false`
4. Since `gridView.Active` is already false, the main sidebar/preview view is shown

The same pattern affects `x` (kill) from grid (`handle_keys.go:182-192`) — grid is hidden before the confirm dialog opens. After confirmation, the user lands on the main view instead of returning to the grid.

### Relevant Code

- `internal/tui/app.go:296-355` — `View()` method: priority-ordered view cascade, the core rendering logic that would need to respect a view stack
- `internal/tui/app.go:39-84` — `Model` struct: holds all component references and overlay state
- `internal/tui/app.go:156-167` — `restoreGrid()`: existing pattern for restoring grid view after detach (uses `RestoreGridMode` flag)
- `internal/tui/handle_keys.go:35-166` — `buildKeyHandlers()`: priority-ordered key handler dispatch
- `internal/tui/handle_keys.go:168-245` — `handleGridKey()`: grid key handling, contains the rename/kill bug
- `internal/tui/handle_input.go:15-24` — `startRename()`: opens rename dialog
- `internal/tui/handle_input.go:175-199` — `handleTitleEdit()`: closes rename dialog, no grid restoration
- `internal/tui/handle_system.go:90-101` — `handleCancelled()`: blanket cancel that clears all overlay flags
- `internal/state/model.go:143-182` — `AppState`: all overlay boolean flags
- `internal/state/model.go:64-79` — `Pane` and `GridRestoreMode` constants
- `internal/tui/components/gridview.go:46-71` — `GridView` struct with `Show()`/`Hide()`
- `internal/tui/views.go:33-48` — `renameDialogView()` rendering

### Existing "Restore" Pattern

The app already has a limited restore mechanism for the grid-after-detach case:
- `AppState.RestoreGridMode` stores which grid mode was active when attaching
- `restoreGrid()` (`app.go:156-167`) re-opens the grid on detach
- This is a single-level "remember one previous state" pattern, not a stack

### Constraints / Dependencies

- **Rendering is priority-based, not stack-based**: The `View()` method uses if/else-if. A view stack would need to coexist with or replace this cascade.
- **Key handling mirrors View()**: `buildKeyHandlers()` returns handlers in the same priority order. Both systems need to stay in sync.
- **`handleCancelled()` is a blanket reset**: It clears all overlay flags at once. A stack-based system would just pop the top.
- **Multiple overlay types**: Some are full-screen (grid, settings), some are overlays on the main view (rename, confirm, pickers). The stack needs to handle both.
- **Grid state is expensive**: Grid view tracks sessions, preview data, cursor position. Hiding and re-showing loses cursor state unless preserved.
- **`inputMode` is a mini-state-machine**: The project creation flow uses `inputMode` transitions (`"project-name"` → dir picker → `"project-dir-confirm"`). This is already a form of navigation sequence that could be modeled as stack pushes.
- **No dependency on external features**: This is a self-contained UI refactor.

## Plan

### Approach: View Stack

Introduce a `viewStack []ViewID` on Model as the **single source of truth** for which view is displayed. Push to open, pop to close. `View()` and key handling inspect the stack top — no more priority cascade of boolean flags.

**ViewID** is a string enum identifying each view layer:
- Base: `ViewMain` (always at stack bottom, never popped)
- Full-screen: `ViewSettings`, `ViewGrid`
- Overlays: `ViewHelp`, `ViewTmuxHelp`, `ViewAttachHint`, `ViewConfirm`, `ViewRecovery`, `ViewOrphan`, `ViewAgentPicker`, `ViewTeamBuilder`, `ViewRename`, `ViewProjectName`, `ViewDirPicker`, `ViewDirConfirm`, `ViewCustomCmd`, `ViewWorktreeBranch`

**Stack operations** on Model:
- `PushView(id)` — push a view on top
- `PopView() ViewID` — remove and return top (never pops below ViewMain)
- `TopView() ViewID` — peek at the top
- `HasView(id) bool` — is this view anywhere in the stack (e.g., "is grid underneath?")

**Why this is better than reordering:**
- Single source of truth instead of ~15 scattered flags
- Open/close is symmetric push/pop — no per-dialog "return to" logic
- Nesting works naturally (grid → rename → confirm = 3 stack entries)
- Future dialogs just need a ViewID constant, no cascade insertion
- `handleCancelled()` becomes `PopView()` instead of blanket flag reset
- Impossible to forget restoring the previous view

### Transition strategy

The stack replaces the dispatch logic (View() and key routing). Component structs still live as fields on Model and keep their internal state (grid cursor, picker items, etc.). Component `Active` fields and AppState overlay flags (`ShowHelp`, `ShowConfirm`, `EditingTitle`, etc.) are **kept in sync** with the stack during this PR — they're set on push, cleared on pop. This avoids breaking any component code that reads its own Active field internally. A future cleanup PR can remove the redundant flags.

### Files to Change

1. **`internal/tui/viewstack.go` (NEW)** — Define `ViewID` type, all constants, and stack methods on Model: `PushView`, `PopView`, `TopView`, `HasView`, `ReplaceTop` (for inputMode transitions like project-name → dir-picker). Include a `refreshGrid()` helper that refreshes the grid session list when it's in the stack (extracted from the pattern at `handle_session.go:19-24`).

2. **`internal/tui/app.go`** — Add `viewStack []ViewID` field to Model struct (~line 39). In `New()` (~line 90), initialize stack with `[]ViewID{ViewMain}`. In `restoreGrid()` (~line 156), push `ViewGrid` onto stack instead of just calling `gridView.Show()`. **Rewrite `View()` method** (~lines 296-355): replace the if/else-if cascade with a `switch m.TopView()` that dispatches to the correct renderer. Each case calls the same rendering function as today (e.g., `ViewRename` → `m.overlayView(m.renameDialogView())`).

3. **`internal/tui/handle_keys.go`** — **Rewrite `buildKeyHandlers()`** (~lines 35-166): replace the priority-ordered `[]KeyHandler` slice with a single `switch m.TopView()` dispatch. Each case calls the corresponding handler function. This eliminates the `componentHandler` wrapper and `focused()` callbacks entirely. **Modify `handleGridKey()`** (~lines 182-199): for `"r"` and `"x"`, replace `m.gridView.Hide()` with `m.PushView(ViewRename)` / `m.PushView(ViewConfirm)` (the grid stays in the stack underneath). For `"t"` and `"W"` (~lines 200-227), add `m.PushView(ViewAgentPicker)`. **Modify `handleGlobalKey()`**: for keys that open overlays (`"?"` → help, `"S"` → settings, `"g"`/`"G"` → grid, `"r"` → rename, `"/"` → filter, etc.), replace flag-setting with `PushView()`.

4. **`internal/tui/handle_input.go`** — In `startRename()` (~line 15): replace `m.appState.EditingTitle = true` with `m.PushView(ViewRename)` (which also sets EditingTitle for compat). In `handleTitleEdit()` (~line 175): replace `m.appState.EditingTitle = false` with `m.PopView()` (which clears EditingTitle). If `m.HasView(ViewGrid)`, call `refreshGrid()`. In `handleNameInput()` (~line 60): replace `m.inputMode = ""` / `m.dirPicker.Show()` transitions with `m.ReplaceTop(ViewDirPicker)`. In `handleDirConfirm()`, `handleCustomCommandInput()`, `handleWorktreeBranchInput()`: replace `m.inputMode = ""` with `m.PopView()`. For chained transitions (custom-command → worktree-branch), use `m.ReplaceTop(ViewWorktreeBranch)`.

5. **`internal/tui/handle_system.go`** — Rewrite `handleCancelled()` (~line 90): replace blanket flag reset with `m.PopView()`. The pop clears the appropriate flag for the popped view. Keep component cleanup (titleEditor.Stop(), agentPicker.Hide(), nameInput.Blur()) triggered by checking what was popped. Rewrite `handleConfirmed()` (~line 84): after the confirmed action, `m.PopView()` the confirm view. If grid is in the stack, call `refreshGrid()`.

6. **`internal/tui/handle_session.go`** — In `handleSessionKilled()` (~line 30): add sidebar rebuild and `refreshGrid()` call (currently missing). In `handleSessionTitleChanged()` (~line 36): add `refreshGrid()` call after `commitState()`. In `handleSessionCreated()` (~line 12): replace `m.gridView.Active` check with `m.HasView(ViewGrid)`.

7. **`internal/tui/handle_project.go`** — In `handleDirPicked()` (~line 25): replace `m.dirPicker.Active = false` / `m.inputMode = "project-dir-confirm"` with stack operations (`PopView` or `ReplaceTop`). In `handleDirPickerCancel()` (~line 47): replace manual flag manipulation with `m.ReplaceTop(ViewProjectName)`.

8. **`internal/tui/viewstack_test.go` (NEW)** — Unit tests for the view stack.

### Test Strategy

- **Unit tests** (`internal/tui/viewstack_test.go`):
  - `PushView`/`PopView` basics: push several views, pop returns them in LIFO order, can't pop below ViewMain
  - `TopView` returns the top view, `HasView` checks the full stack
  - `ReplaceTop` swaps the top without affecting views below
  - Scenario: push ViewGrid, push ViewRename, pop → top is ViewGrid
  - Scenario: push ViewGrid, push ViewConfirm, pop → top is ViewGrid
  - Scenario: push ViewGrid, push ViewAgentPicker, pop → top is ViewGrid
- **Integration-style tests** (same file or separate):
  - Simulate: grid active → press `r` → verify stack is `[Main, Grid, Rename]`
  - Simulate: rename done (enter/esc) → verify stack is `[Main, Grid]`
  - Simulate: grid active → press `x` → verify stack is `[Main, Grid, Confirm]`
  - Simulate: confirm done → verify stack is `[Main, Grid]` and session killed
- **Manual testing scenarios:**
  - Grid → rename (`r`) → confirm → verify grid shown with updated title
  - Grid → rename (`r`) ��� cancel (`esc`) → verify grid shown
  - Grid → kill (`x`) → confirm → verify grid shown without killed session
  - Grid → new session (`t`) → pick agent → verify session created, grid refreshed
  - All main-view overlay flows unchanged (rename, confirm, help, settings, project creation wizard)
  - Project creation wizard: new project (`n`) → name → dir picker → confirm → verify project created

### Risks

- **Sync drift**: Component `Active` fields and AppState flags are kept in sync with the stack during this PR. If a code path sets a flag without going through push/pop, the stack and flags will diverge. Mitigation: audit all flag-setting code during implementation; add a debug assertion that validates stack-flag consistency.
- **Multi-step flows**: The project creation wizard (name → dir picker → dir confirm) and worktree flow (agent picker → custom command → branch name) chain several views. These need `ReplaceTop()` for lateral transitions and `PushView()` for nested ones. Getting this wrong would break the wizard. Mitigation: test each step of each wizard flow.
- **handleCancelled() scope**: The current blanket reset clears everything. With the stack, `PopView()` only removes the top layer. If multiple overlays were somehow pushed (shouldn't happen normally), a single cancel only pops one. This is actually correct behavior, but differs from the current "nuclear reset". Mitigation: verify no flow depends on the blanket reset clearing more than the active overlay.
- **Grid refresh after state changes**: Grid is now underneath overlays in the stack. After state-changing operations (kill, rename, create), the grid's session list may be stale. Mitigation: `refreshGrid()` helper called whenever state changes and grid is in the stack.

## Implementation Notes

- **Branch:** `feature/46-view-stack`
- Implemented the full view stack approach as planned.
- Legacy flags (`ShowHelp`, `ShowConfirm`, `EditingTitle`, `gridView.Active`, etc.) are kept in sync via `syncLegacyFlags()` during push/pop. This avoids breaking component code that reads its own Active field internally.
- `inputMode = "new-session"` was kept as a semantic flag (not a ViewID) because it tracks purpose, not a visible view. The visible view in that flow is `ViewAgentPicker`.
- The `focus.go` file (KeyHandler interface, componentHandler, dispatchKey) is no longer used in production code but kept for its standalone tests. Could be removed in a future cleanup.
- `handleGridKey` now detects when the grid component internally closes itself (esc/q/enter) by checking `!m.gridView.Active` after calling `gridView.Update()`, and pops the stack accordingly.
- Startup overlays (recoveryPicker, orphanPicker) that initialize with `Active = true` are pushed onto the stack in `New()`.
- Grid refresh helper (`refreshGrid()`) added and called from session handlers to keep grid data fresh when it's underneath a dialog.

- **PR:** —
