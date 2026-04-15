# Feature: fix(grid): sidebar view flashes during attach/detach in grid mode

- **GitHub Issue:** #111
- **Stage:** DONE
- **Type:** bug
- **Complexity:** S
- **Priority:** P1
- **Branch:** —

## Description

Regression: when in grid mode and pressing `a`/Enter to attach (or on detach), the sidebar view briefly flashes before the attach transition completes.

PR #110 was intended to fix this, but the fix is incomplete. `internal/tui/handle_preview.go:handleGridSessionSelected` pops the grid view unconditionally before calling `doAttach()`. With `HideAttachHint=true` (the default), no view is pushed over the top, so the next `View()` render falls back to the sidebar (`TopView() == ViewMain`) for at least one frame before BubbleTea executes the `tea.Quit`/`tea.Exec` returned by `doAttach()`.

Current tests missed it because:
- `TestFlow_GridKeyAttachNoFlash` only asserts grid state immediately after the key event, not after the returned `Cmd` resolves.
- `TestFlow_GridAttachDetachRestoresGrid` exercises only the native backend, not the `tea.Exec` tmux path used by default.
- No test calls `View()` between `doAttach()` returning and the `Cmd` executing — the flashed frame is invisible to the suite.

### Acceptance criteria

- In grid mode, pressing `a`/Enter to attach shows no sidebar frame.
- On detach, restoring back to grid mode shows no sidebar frame.
- Regression tests: flow tests that (a) render `View()` after the attach `Cmd` resolves and before `tea.Quit`/`tea.Exec` runs, and (b) cover both native and tmux backend paths.

## Research

### Root Cause

`handleGridSessionSelected` at `internal/tui/handle_preview.go:53-92` unconditionally pops `ViewGrid` before deciding the next step:

```go
if m.HasView(ViewGrid) {
    m.PopView()                    // grid removed → TopView becomes ViewMain
}
// ... build attach details ...
if !m.cfg.HideAttachHint {
    m.pendingAttach = attach
    m.PushView(ViewAttachHint)     // push happens after pop, same Update — no frame between
    return m, nil
}
cmd := m.doAttach(*attach)         // HideAttachHint=true: nothing pushed over ViewMain
return m, cmd                      // cmd is tea.Exec (tmux) or tea.Quit (native)
```

After `Update` returns, BubbleTea renders `View()` once before running the returned `Cmd`. `app.go:410 View()` dispatches on `TopView()`:

```go
case ViewGrid:   return m.gridView.View(...)
// ... (no case for ViewMain — falls through to sidebar+preview+status layout at line 460)
```

With the grid popped and nothing pushed, `TopView() == ViewMain` → sidebar is rendered for one frame before `tea.Exec` releases the terminal. Same flash fires on `enter` in the hint overlay at `handle_keys.go:819-827`: `PopView()` (hint) → `doAttach()` → render frame with `TopView == ViewMain` → `tea.Exec`.

On **detach** (tmux backend), `bubbletea/exec.go` calls `p.RestoreTerminal()` and enables the renderer *before* sending `AttachDoneMsg` via `go p.Send(...)`. The renderer calls `View()` while state is still "grid popped" → sidebar renders briefly before `handleAttachDone` pushes the grid back via `restoreGrid()` at `app.go:254-265`.

### Proposed Fix

Keep `ViewGrid` on the stack throughout the attach/detach lifecycle. The grid is re-rendered for every frame — no "ViewMain" gap.

1. **`handle_preview.go:57-59`** — Remove the `PopView()` call. Attach hint already renders via `overlayView` (dark background + centered hint; does not show underlying grid content, but no sidebar flash either), and it gets popped correctly in `handle_keys.go:819-845`.

2. **`app.go:restoreGrid()`** — Make `PushView(ViewGrid)` idempotent. Currently pushes unconditionally; with the fix, grid is already on the stack on detach, so the push would warn about a duplicate and break the stack invariant.

3. **Sidebar-origin attach stays sidebar.** `handleSessionAttach` in `handle_session.go:71-76` is not in the grid path; it just calls `doAttach`. `RestoreGridMode = None` there, so `restoreGrid()` early-returns. Sidebar flash is a non-issue because user is coming from sidebar.

### Flow Walk-Through with Fix

- **From grid, HideAttachHint=true**: Enter → `GridSessionSelectedMsg` → `doAttach` (no pop). Render: `TopView=ViewGrid` ✓. `tea.Exec` attaches. Detach → renderer re-enabled → render `TopView=ViewGrid` ✓. `AttachDoneMsg` → `restoreGrid` no-op (already on stack).
- **From grid, HideAttachHint=false**: Enter → `PushView(ViewAttachHint)` over grid. Render: hint ✓. Enter on hint → `PopView` (back to grid) → `doAttach`. Render: `TopView=ViewGrid` ✓. Exec/detach as above. Esc on hint → pop → back to grid. No attach.
- **From sidebar**: grid never pushed; `RestoreGridMode=None`; renders stay sidebar throughout; no regression.

### Existing Test Gaps

- `flow_grid_test.go:382-401 TestFlow_GridKeyAttachNoFlash` — `SendSpecialKey(KeyEnter)` only runs `Update(KeyMsg)` → `gridview.Update` returns the Cmd. The assertion runs **before** the Cmd produces `GridSessionSelectedMsg` and **before** `handleGridSessionSelected` executes. It's checking state pre-handler, so it can't catch the pop-then-render flash.
- `flow_grid_test.go:407-439 TestFlow_GridMouseAttachNoFlash` — sends `GridSessionSelectedMsg` directly then asserts `gridView.Active == false` (line 436-438). That asserts the OPPOSITE of what we want — it treats the pop as correct.
- `flow_grid_test.go:14-74 TestFlow_GridAttachDetachRestoresGrid` — exercises only the native `tea.Quit` path (`mock.SetUseExecAttach(false)`); doesn't reach the `tea.Exec` path.
- None of the above call `f.Model().View()` between `GridSessionSelectedMsg` processing and the returned Cmd — so the sidebar-rendered frame is invisible to the suite.

### Relevant Code

- `internal/tui/handle_preview.go:53-92` — `handleGridSessionSelected` (bug site).
- `internal/tui/app.go:254-265` — `restoreGrid` (needs idempotency).
- `internal/tui/app.go:410-460` — `View()` TopView dispatch (sidebar fallback).
- `internal/tui/views.go:17-32, 178-207` — `overlayView` and `doAttach`.
- `internal/tui/handle_session.go:71-106` — `handleSessionAttach` and `handleAttachDone`.
- `internal/tui/handle_keys.go:819-845` — attach hint confirm/cancel handlers.
- `internal/tui/viewstack.go:31-72` — `PushView` / `PopView` / `HasView` / `TopView`.
- `internal/tui/components/gridview.go:337-339` — grid `View()` early-return when `!Active`.
- `internal/tui/flow_test.go:32-97` — `Send` / `SendKey` / `ExecCmdChain` runner semantics (Send calls Update once; does NOT auto-execute returned Cmd).
- `internal/mux/muxtest/mock.go` — `MockBackend.SetUseExecAttach(true)` flips `doAttach` to the `tea.Exec` path.

### Constraints / Dependencies

- Must not reintroduce the renderer race that PR #110 fixed: no `Hide()` in the gridview key handler, no writes to terminal that race with the renderer.
- `restoreGrid()` is also called during startup (`cmd/start.go` reload path) with `appState.RestoreGridMode` pre-seeded; must still push grid when stack has only `ViewMain`.
- `PushView` emits a `debugLog WARNING` on duplicate stack entries (viewstack.go:33-38). Adding the `HasView` guard is required to keep the stack clean on detach-restore.

## Plan

### Files to Change

1. **`internal/tui/handle_preview.go`** (lines 53-92 `handleGridSessionSelected`)
   - **Delete** the unconditional pop at lines 57-59:
     ```go
     if m.HasView(ViewGrid) {
         m.PopView()
     }
     ```
   - Keep the rest of the function unchanged. The attach-hint branch (`!HideAttachHint`) will push `ViewAttachHint` on top of `ViewGrid`; the direct-attach branch will leave `ViewGrid` on top while `tea.Exec`/`tea.Quit` fires. Update the function doc comment accordingly (remove reference to "pop the grid from the view stack here").

2. **`internal/tui/handle_keys.go`** (lines 819-845 — the attach-hint `enter`/`d`/`esc` handlers)
   - `enter` and `d` currently `PopView()` (removes hint) then call `doAttach`. With the fix, `TopView()` after the pop is `ViewGrid` (if user came from grid) or `ViewMain` (sidebar origin) — both behaviors are now correct:
     - Grid origin → grid renders the pre-exec frame ✓
     - Sidebar origin → sidebar renders (expected for sidebar attach) ✓
   - `esc` pops the hint and clears `pendingAttach`; with the fix, the view stack naturally returns to whatever was under the hint — grid if the user came from grid, sidebar if they came from sidebar. **Behavior change for the grid-origin path only**: previously `esc` always flattened to sidebar (because the grid was popped before the hint was pushed); now it returns to the prior view. Sidebar-origin behavior is unchanged. Update `TestFlow_GridAttachHintCancelReturnsToSidebar` (`flow_grid_test.go:441-469`) — rename to `TestFlow_GridAttachHintCancelReturnsToGrid` and flip the final assertion to `AssertGridActive(true)` + a grid-content `ViewContains`.
   - No code changes in `handle_keys.go` itself — only the test expectation.

3. **`internal/tui/app.go`** (lines 254-265 `restoreGrid`)
   - Make idempotent. Change the trailing `m.PushView(ViewGrid)` to:
     ```go
     if !m.HasView(ViewGrid) {
         m.PushView(ViewGrid)
     }
     ```
   - Needed because on the tmux `tea.Exec` detach path the grid is already on the stack when `AttachDoneMsg` arrives.

4. **`internal/tui/flow_grid_test.go`** — update existing tests and add regression tests (see Test Strategy).

5. **`CHANGELOG.md`** — append to `[Unreleased] → Fixed`:
   - `Sidebar view no longer flashes briefly when attaching or detaching from grid mode (regression from #109 / PR #110).`

### Test Strategy

Per `AGENTS.md`: every behaviour change needs unit + functional tests; bug fixes must include the test that would have caught the regression. All tests live in `internal/tui/flow_grid_test.go` (and `flow_attach_test.go` for sidebar-origin cases).

#### Flow matrix to cover

Render frames that previously flashed (now must stay on the origin view):

| # | Origin | Hint? | Trigger | Backend | Pre-exec frame | Post-detach frame |
|---|--------|-------|---------|---------|----------------|-------------------|
| 1 | Grid (project) | off | key Enter | native | grid | grid (via New()) |
| 2 | Grid (project) | off | key Enter | tmux  | grid | grid (pre-AttachDone) |
| 3 | Grid (project) | off | mouse click | native | grid | — |
| 4 | Grid (project) | off | mouse click | tmux | grid | — |
| 5 | Grid (all) | off | key Enter | tmux | grid | grid |
| 6 | Grid (project) | on | Enter on hint | tmux | grid | grid |
| 7 | Grid (project) | on | "d" on hint | tmux | grid | — |
| 8 | Grid (project) | on | Esc on hint | — | return to grid | — |
| 9 | Sidebar | on | Esc on hint | — | return to sidebar (regression guard) | — |
| 10 | Sidebar | off | key "a" | tmux | sidebar (regression guard) | sidebar |

Plus one pure unit test: `restoreGrid()` idempotency.

#### Tests to update

- **`TestFlow_GridAttachDetachRestoresGrid`** (`flow_grid_test.go:17`, covers row #1)
  - After `f.ExecCmdChain(cmd)`: change `f.AssertGridActive(false)` to `true`, add `f.ViewContains("session-1")` and `if f.Model().TopView() != ViewGrid { t.Error(...) }` before the re-entry block to assert the pre-exec render frame is grid.

- **`TestFlow_GridAllProjectsRestores`** (`flow_grid_test.go:77`, covers row #5 native half)
  - Same edits as above for the all-projects variant.

- **`TestFlow_GridKeyAttachNoFlash`** (`flow_grid_test.go:382`, strengthen row #1)
  - Replace the post-KeyMsg assertion with a full chain: `cmd := f.SendSpecialKey(tea.KeyEnter); f.ExecCmdChain(cmd)`, then assert `TopView() == ViewGrid`, `f.AssertGridActive(true)`, and `f.ViewContains("session-1")`.

- **`TestFlow_GridMouseAttachNoFlash`** (`flow_grid_test.go:407`, covers row #3)
  - Flip the final `gridView.Active == false` assertion to `true`. Call `f.Model().View()` and assert grid content present.

- **`TestFlow_GridAttachHintCancelReturnsToSidebar`** (`flow_grid_test.go:441`, now row #8)
  - Rename to `TestFlow_GridAttachHintCancel_ReturnsToGrid`. Final assertions: `AssertGridActive(true)` + `ViewContains("session-1")`.

#### New tests

1. **`TestFlow_GridKey_TmuxExec_NoFlash`** (row #2) — `testFlowModel` + `mock.SetUseExecAttach(true)` + `m.attachOut = &bytes.Buffer{}`. Open project grid with `g`, send `tea.KeyEnter`, run `ExecCmdChain`. Assert `TopView()==ViewGrid`, `ViewContains("session-1")`, and that `View()` does NOT contain a sidebar-only token (e.g. the sidebar root project name when rendered in sidebar style). The canonical tmux-backend regression test.

2. **`TestFlow_GridMouse_TmuxExec_NoFlash`** (row #4) — Same setup as #1 but send `components.GridSessionSelectedMsg{...}` directly (mimics mouse-click Cmd). Assert grid-still-active + grid content rendered.

3. **`TestFlow_GridAll_TmuxExec_NoFlash`** (row #5 tmux half) — Open all-projects grid with `G`, repeat #1. Assert `ViewContains("session-1")` and `ViewContains("session-2")`.

4. **`TestFlow_GridDetach_TmuxExec_RendersGridPreAttachDone`** (row #2 post-detach) — Open grid, send `GridSessionSelectedMsg` + run chain, then call `f.Model().View()` and assert grid content BEFORE sending `AttachDoneMsg`. Then send `AttachDoneMsg{RestoreGridMode: GridRestoreProject}` and assert grid still active and `len(viewStack) == 2` (Main + Grid, no duplicate).

5. **`TestFlow_GridHint_Confirm_TmuxExec_NoFlash`** (row #6) — `testFlowModelWithHint` + `SetUseExecAttach(true)`. Open grid → Enter (hint shows) → Enter on hint. After the confirm chain, assert `TopView()==ViewGrid` and grid content rendered.

6. **`TestFlow_GridHint_D_TmuxExec_NoFlash`** (row #7) — Same as #5 but press `d` on hint instead of Enter. Assert grid still rendered pre-exec AND `cfg.HideAttachHint == true` persisted.

7. **`TestFlow_SidebarAttachHintCancel_ReturnsToSidebar`** (row #9) — `testFlowModelWithHint`. Press `a` from sidebar (no grid open), hint shows, press Esc. Assert `TopView() == ViewMain`, grid not active, `pendingAttach == nil`. Regression guard: proves the plan didn't accidentally break the sidebar-origin stack semantics.

8. **`TestFlow_Sidebar_TmuxExec_NoGridRender`** (row #10) — `testFlowModel` + `SetUseExecAttach(true)`. Press `a` from sidebar. Assert `TopView() == ViewMain` throughout, sidebar content rendered in `View()`, grid NEVER rendered (negative assertion on a grid-only token, e.g. the grid border chars or an all-projects header). Regression guard.

9. **`TestFlow_RestoreGrid_Idempotent`** — unit test on `restoreGrid()`. Build a Model with `ViewGrid` already on the stack (use `testFlowModel` + `openGrid`), capture `len(m.viewStack)`, set `appState.RestoreGridMode = state.GridRestoreAll`, call `m.restoreGrid()`. Assert:
   - `len(m.viewStack)` unchanged (no duplicate push).
   - `m.HasView(ViewGrid)` still true.
   - `m.gridView.Mode == state.GridRestoreAll` (SyncState still ran — mode switched from project to all).
   - `m.appState.RestoreGridMode == state.GridRestoreNone` (consumed).

10. **`TestFlow_RestoreGrid_FromEmptyStack`** — startup path. Build Model without grid on stack, set `RestoreGridMode = state.GridRestoreProject`, call `m.restoreGrid()`. Assert `HasView(ViewGrid)` true, `len(viewStack) == 2`. Confirms startup restore still pushes.

#### Rendering-assertion helpers

Tests that check "grid rendered, sidebar not rendered" need stable tokens:
- **Grid-only**: grid cell borders (`│` in specific positions) or the grid status hints that differ from sidebar. Check `internal/tui/components/gridview.go` `View()` output and pick a stable substring (e.g. a hint like `"(i) input"` that only the grid renders).
- **Sidebar-only**: the sidebar project header format, or the preview pane divider, or a status-bar hint that's only in sidebar mode.

Concrete tokens to use will be finalized during IMPLEMENT by running `go test -run TestFlow_GridKeyAttachNoFlash -v` with a `fmt.Println(view)` probe, then picking from the actual output.

#### Why these tests catch the regression

- Every new/updated test calls `f.Model().View()` in the frame between the Cmd chain resolving and the terminal handoff — the exact frame the existing suite skipped.
- The `mock.SetUseExecAttach(true)` path (tmux backend) is exercised for 6 of 10 rows, closing the coverage gap called out in Research.
- Both attach triggers (keyboard + mouse) are covered.
- Both grid modes (project + all-projects) are covered.
- All three hint branches (Enter / "d" / Esc) are covered.
- Regression guards (rows #9 #10) ensure sidebar-origin flows are not collaterally broken.

### Risks

- **Behavior change on `esc` from hint (grid-origin only)**: previously always returned to sidebar because grid had been popped; now returns to whatever was under the hint on the stack (grid for grid-origin, sidebar for sidebar-origin). Sidebar-origin behavior is unchanged. The test `TestFlow_GridAttachHintCancelReturnsToSidebar` encoded the previous — buggy — behavior for the grid path; renaming and flipping it is intentional. Call out in the PR description.
- **Duplicate `ViewGrid` on the stack** is prevented by the `HasView` guard in `restoreGrid()`. If any other path (not `restoreGrid`) pushes grid after detach, we'd see the `debugLog WARNING` and potentially a broken pop on `G`/`g` toggle. Mitigation: `TestFlow_RestoreGrid_Idempotent` asserts no stack growth; a grep for other `PushView(ViewGrid)` callsites beyond `openGrid` and `restoreGrid` should come back empty (already verified in Research).
- **Startup restore path** (`cmd/start.go` reload after native detach) constructs a fresh Model with `appState.RestoreGridMode` seeded; `restoreGrid()` runs from an empty stack so `HasView(ViewGrid)` is false and `PushView` still fires. Verified by the existing `TestFlow_GridAttachDetachRestoresGrid` re-entry block.
- **Renderer race (PR #110 regression risk)**: the fix does not touch `gridview.go` `Hide()` or any terminal writes that race with the renderer — only view-stack bookkeeping. Low risk.

## Implementation Notes

- Code changes were exactly as planned: (1) delete the `PopView(ViewGrid)` block in `handleGridSessionSelected` and replace with a doc-comment explaining why the grid stays on the stack; (2) wrap `restoreGrid()`'s `PushView(ViewGrid)` in a `!HasView(ViewGrid)` guard.
- Test writing discovered that `Model.TopView()` / `HasView()` are pointer receivers, so tests must assign `f.Model()` into a local variable before calling them (`mm := f.Model(); mm.TopView()`). Several existing tests already followed this pattern — followed suit for the new ones.
- Stable tokens picked from actual `View()` output: `"↑↓←→ navigate"` (grid-only nav hint) and `"[1] test-project-1"` (sidebar numbered project list). These are used in `ViewContains` / `ViewNotContains` assertions to differentiate grid vs sidebar rendering.
- In the tmux-exec tests, `m.attachOut = &bytes.Buffer{}` captures the synchronous `\033[2J\033[H` pre-write from `doAttach` and prevents test-run stdout pollution.
- All 10 planned new tests added; 6 existing tests updated. Full suite (`go test ./...`) green.
- `TestFlow_GridAttachWithHint` (line 344) and `TestFlow_GridAttachSetsActiveSessionID` (line 593) were not in the original plan list but broke under the behavior change (expected grid hidden post-attach) — updated to assert the new grid-stays-active contract.

- **PR:** #113
