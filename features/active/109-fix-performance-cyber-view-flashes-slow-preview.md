# Feature: Fix performance: cyber view flashes and slow preview refresh on slow machines

- **GitHub Issue:** #109
- **Stage:** IMPLEMENT
- **Type:** bug
- **Complexity:** M
- **Priority:** P1
- **Branch:** —

## Description

On slower machines, users experience brief flashes of the sidebar view when attaching or detaching sessions in grid view. Additionally, preview panel refreshes are noticeably slow on lower-powered hardware. These issues likely point to inefficient rendering, excessive re-renders, or unoptimized refresh intervals that need investigation and optimization to ensure smooth performance across all hardware configurations.

## Research

Two distinct root causes — one rendering state-machine bug and one polling inefficiency.

### Issue 1: Sidebar view flash during attach/detach

**Root cause: one-frame blank render between grid hiding and attach transition**

In `gridview.Update()` (`internal/tui/components/gridview.go:259-265`), when the user presses Enter/a to select a session, `gv.Hide()` is called immediately, setting `gridView.Active = false`. This happens before `GridSessionSelectedMsg` is processed by the root model.

Bubble Tea calls `View()` AFTER each `Update()` completes, before processing the Cmd that carries the message. So for exactly one frame:
- `TopView() == ViewGrid` (view stack unchanged)
- `gridView.Active = false`
- `gridView.View()` returns `""` (`gridview.go:339-341`)
- `app.View()` `case ViewGrid:` returns that empty string
- **Terminal renders a blank frame** → sidebar/terminal background flashes

This one-frame blank is imperceptible on fast machines, very visible on slow ones.

**Secondary cause: `doAttach` escape sequence**

`doAttach()` (`internal/tui/views.go:196`) writes:
`\033[?1049l\033[2J\033[H\033[?1049h`

This exits the alt-screen buffer, briefly exposing the primary buffer (which may contain previous terminal content), then re-enters. On a slow machine this flash is visible before tmux takes over.

### Issue 2: Slow preview refresh

**Multiple concurrent polling goroutines run redundant tmux capture-pane subprocess calls.**

Grid view polling in steady state (6 sessions, 500ms default):
- `PollGridPreviews` — 500ms, ALL sessions × 200 lines → 12 captures/sec
- `WatchStatuses` — 1000ms, ALL sessions × 50 lines → 6 captures/sec
- `WatchTitles` — 1000ms, ALL sessions × 200 lines (raw), **early-exits on first title found** → wastes ~(N-1)/2 captures per tick

**Critical inefficiency in `WatchTitles`** (`internal/escape/watcher.go:29-31`):
```go
if title := ExtractTitle(raw); title != "" {
    return TitleDetectedMsg{SessionID: sessionID, Title: title}
}
```
Returns on the first session that has a title, leaving remaining sessions unchecked and never emitting their titles. With N sessions, roughly (N-1)/2 `CapturePaneRaw` calls are wasted. Each is a tmux subprocess.

**Oversized grid captures**: `PollGridPreviews` requests 200 lines per session (`gridview.go:40`). Grid cells can display ~20-40 lines. The extra 160 lines are parsed and thrown away, adding unnecessary bytes per tmux call.

**Input mode double-polling** (`handle_preview.go:42-50`): When grid input mode is active, `PollFocusedGridPreview` (50ms, 1 session) AND `PollGridPreviews` (250ms, all sessions) both run. Both capture the focused session redundantly.

### Relevant Code

- `internal/tui/components/gridview.go:259-265` — Enter/a case calling `gv.Hide()` prematurely
- `internal/tui/components/gridview.go:338-341` — `View()` returns `""` when `!Active`
- `internal/tui/views.go:196` — escape sequence that exits/re-enters alt-screen before attach
- `internal/tui/handle_preview.go:53-86` — `handleGridSessionSelected` pushes ViewAttachHint
- `internal/tui/handle_keys.go:818-828` — `handleAttachHint` pops hint, calls `doAttach`
- `internal/escape/watcher.go:22-35` — `WatchTitles` with early-exit bug
- `internal/tui/components/gridview.go:32-47` — `PollGridPreviews` capturing 200 lines
- `internal/tui/handle_preview.go:104-135` — `scheduleGridPoll` / `scheduleFocusedSessionPoll`

### Constraints / Dependencies
- `WatchTitles` return type needs to change from `TitleDetectedMsg` to a batch type — handler in `app.go` Update() needs updating
- The `gv.Hide()` removal from Enter/a case must not break the `esc` case (which should still hide)
- `doAttach` escape sequence may have been added to fix a terminal state issue — test carefully after removal

## Plan

Two root-cause fixes + two efficiency improvements.

### Files to Change

1. `internal/tui/components/gridview.go`
   - Remove `gv.Hide()` from the `enter/a` case (line 261) — prevents premature `Active=false` that causes `handleGridKey` to pop ViewGrid one frame early
   - Change `PollGridPreviews` capture from 200 → 100 lines (line 40) — grid cells display ≤50 lines; halves tmux data per poll

2. `internal/tui/handle_keys.go`
   - Remove inline `m.PopView()` from mouse click grid attach path (line 680) — same root cause as keyboard path; premature stack pop before message processing

3. `internal/tui/handle_preview.go`
   - In `handleGridSessionSelected`: add `if m.HasView(ViewGrid) { m.PopView() }` before pushing hint or calling doAttach — this is the correct place to pop the grid (both keyboard and mouse paths converge here)

4. `internal/tui/views.go`
   - Change `\033[?1049l\033[2J\033[H\033[?1049h` to `\033[2J\033[H` (line 196) — remove alt-screen exit/re-enter that flashes the primary terminal buffer

5. `internal/escape/watcher.go`
   - Fix `WatchTitles` to loop through ALL sessions and collect all found titles instead of returning early on first match — eliminates ~(N-1)/2 wasted `CapturePaneRaw` calls per tick

6. `internal/tui/messages.go`
   - Add `TitlesDetectedMsg{Titles map[string]string}` type with compile-time check

7. `internal/tui/app.go`
   - Update handler: replace `handleTitleDetected(TitleDetectedMsg)` with `handleTitlesDetected(TitlesDetectedMsg)` that applies title updates to all matched sessions

### Test Strategy

**New flow tests** (`internal/tui/flow_grid_test.go`):
- `TestFlow_GridKeyAttachNoFlash` — after keyboard Enter on grid cell, verify stack goes directly to `[ViewMain, ViewAttachHint]` without intermediate `[ViewMain]`
- `TestFlow_GridMouseAttachNoFlash` — same for mouse click path

**Updated unit test** (`internal/escape/watcher_test.go`):
- `TestWatchTitles_BatchCollectsAllTitles` — mock 3 sessions with titles; verify all 3 are returned in `TitlesDetectedMsg`, not just the first

**Existing tests must pass:**
- `TestFlow_GridAttachDoneRestoresGrid` / `...AllMode`
- All escape watcher tests

### Risks

- If any path other than keyboard/mouse sends `GridSessionSelectedMsg` with ViewGrid NOT in the stack, the `m.HasView(ViewGrid)` guard protects against an unintended extra pop
- `doAttach` escape change: some terminals may need the alt-screen exit before tmux attach; test manually and revert if tmux layout breaks
- `TitlesDetectedMsg` now updates all sessions' titles (not just active) — behaviorally more correct but is a change; verify user-set titles still aren't overwritten

## Implementation Notes

- Removed `gv.Hide()` from the `enter/a` case in `gridview.Update()` — the grid stays Active=true until `handleGridSessionSelected` processes the message, eliminating the one-frame blank render.
- Removed premature `m.PopView()` from the mouse-click path in `handle_keys.go` — same root cause as keyboard path.
- Added `if m.HasView(ViewGrid) { m.PopView() }` at the top of `handleGridSessionSelected` — this is where the grid is correctly removed, after message processing.
- Simplified `doAttach` escape sequence from `\033[?1049l\033[2J\033[H\033[?1049h` to `\033[2J\033[H` — removes the alt-screen exit/re-enter that briefly flashed the primary terminal buffer.
- Replaced `WatchTitles` early-exit with batch collection: all sessions' titles are gathered in one loop and returned as a single `TitlesDetectedMsg`, eliminating ~(N-1)/2 wasted `CapturePaneRaw` calls per tick.
- Reduced `PollGridPreviews` line count from 200 → 100 — grid cells display ≤50 lines; halves tmux data transfer per poll.
- Updated 4 existing flow tests to reflect new behavior (grid stays active until `ExecCmdChain` processes the message).
- Added 4 new tests: `TestFlow_GridKeyAttachNoFlash`, `TestFlow_GridMouseAttachNoFlash`, `TestFlow_GridAttachHintCancelReturnsToSidebar`, `TestWatchTitles_BatchCollectsAllTitles`, `TestWatchTitles_NilWhenNoTitles`.

- **PR:** —
