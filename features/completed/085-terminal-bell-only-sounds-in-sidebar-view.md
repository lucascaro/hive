# Feature: Terminal bell only sounds in sidebar view, not when attached to session

- **GitHub Issue:** #85
- **Stage:** DONE
- **Type:** bug
- **Complexity:** M
- **Priority:** P2
- **Branch:** —

## Description

The alarm/terminal bell only sounds when the user is out of a session (appears to be sidebar view only). When attached to a session, bell events from the running agent do not produce audio.

Expected: bell events should sound regardless of which view the user is in — sidebar, grid, or attached to a session.

## Research

### Root Cause

The bell detection loop lives in `handleStatusesDetected` (`internal/tui/handle_session.go:129`), driven by `scheduleWatchStatuses` ticking every ~500 ms via `tea.Tick`. When the user attaches to a session:

- **tmux backend** (`doAttach`, `internal/tui/views.go:241`): `tea.ExecProcess` *suspends* the entire BubbleTea event loop. No `tea.Tick` callbacks fire, `scheduleWatchStatuses` stops, `GetPaneTitles` is never called, and `audio.Play()` is never invoked.
- **native backend** (`tea.Quit` path, `cmd/start.go:136`): The TUI exits entirely. `tui.RunAttach(*a)` blocks while attached. Same result — no bell polling.

`audio.Play()` is only called from `handleStatusesDetected` (`handle_session.go:181`), so it is unreachable while the TUI is suspended/exited.

### Relevant Code
- `internal/tui/handle_session.go:129` — `handleStatusesDetected`: bell detection + `audio.Play` call (line 181)
- `internal/tui/handle_preview.go:110` — `scheduleWatchStatuses`: builds the `WatchStatuses` tick; polls `GetPaneTitles` for bell flags (line 125)
- `internal/tui/views.go:241` — `doAttach`: tmux-backend path uses `tea.ExecProcess` (line 260); native-backend path calls `tea.Quit` (line 248)
- `internal/tmux/capture.go:44` — `GetPaneTitles`: reads `#{window_bell_flag}` from `tmux list-windows`
- `internal/audio/bell.go` — `audio.Play`, `audio.SetTestHooks`, `audio.SyncForTest`
- `cmd/start.go:136` — native-backend post-attach loop; calls `tui.RunAttach`
- `internal/tui/flow_bell_test.go` — existing bell flow tests (debounce, silent mode, custom sound)

### Fix Strategy

**Part 1 — Audible bell during attachment (tmux backend):**
Start a background goroutine *outside* the BubbleTea event loop that polls `GetPaneTitles` for bell flags during attachment. It calls `audio.Play(bellSound)` directly and exits when the attachment ends. The goroutine also accumulates `newBells map[string]bool` (keyed by sessionID) so visual indicators can be restored when the TUI resumes.

Edge tracking matches the existing logic in `handleStatusesDetected`:
- Only fire for *new* bells (not already-pending at attach time).
- Respect the 500 ms debounce.
- Clear tracking when a window stops ringing so it can re-fire.

The watcher covers both: bells from the currently-attached session and bells from other background sessions.

**Part 2 — Bell badge in grid view:**
`GridView` (`internal/tui/components/gridview.go`) has no `bellPending` support at all. Need to add:
- `bellPending map[string]bool` field
- `SetBellPending(bells map[string]bool)` method  
- Bell badge rendered in the cell header prefix area (dark background, same as status dot / agent badge) using a `styles.BellBadgeOnBg(bg)` helper following the existing `StatusDotOnBg`/`AgentBadgeOnBg` pattern.

**Part 3 — Carry bells through `AttachDoneMsg`:**
Add `NewBells map[string]bool` to `AttachDoneMsg` (`messages.go`). `handleAttachDone` merges these into `m.bellPending` and calls both `m.sidebar.SetBellPending` and `m.gridView.SetBellPending` so both views update on return.

### Constraints / Dependencies
- `mux.GetPaneTitles` on the **native backend** returns `nil, nil, nil` — bell detection via polling is unavailable on that path. The watcher goroutine gracefully handles this (empty response = no action). The native backend fix is a known limitation for now.
- All hive sessions share `mux.HiveSession` (`"hive-sessions"`), so one `GetPaneTitles` call covers all sessions (same as `scheduleWatchStatuses`).
- `sync.WaitGroup` in the watcher ensures the goroutine finishes before `Stop()` returns, preventing a race with `AttachDoneMsg` delivery.
- The goroutine needs a mockable `getPaneTitlesFn` hook (like `audio.SetTestHooks`) to avoid requiring a live tmux server in tests.
- `audio.SetTestHooks` and `audio.SyncForTest` already exist for test isolation; the new watcher must respect them.
- `m.gridView.SetBellPending` must be called everywhere `m.sidebar.SetBellPending` is called (3 sites: `handleStatusesDetected:198`, `handleAttachDone:87`, and the new merge in `handleAttachDone` for watcher bells).

## Plan

### Files to Change

1. **`internal/tui/styles/theme.go`** — Add `BellBadgeOnBg(bg lipgloss.Color) string` function, following the `StatusDotOnBg`/`AgentBadgeOnBg` pattern. Returns `♪` with `ColorWarning` foreground on the given background.

2. **`internal/tui/bell_watcher.go`** *(new file)* — `attachBellWatcher` struct:
   - Fields: `done chan struct{}`, `wg sync.WaitGroup`, `mu sync.Mutex`, `newBells map[string]bool` (sessionID), `getPaneTitlesFn` (mockable hook, defaults to `mux.GetPaneTitles`)
   - `newAttachBellWatcher() *attachBellWatcher`
   - `start(bellSound string, sessionTargets map[string]string)` — starts goroutine; takes initial snapshot to establish baseline; polls every 500ms; calls `audio.Play` on new edges (500ms debounce); records sessionIDs in `newBells`
   - `stop() map[string]bool` — closes `done`, calls `wg.Wait()` (ensures goroutine exits before reading), returns `newBells`

3. **`internal/tui/messages.go`** — Add `NewBells map[string]bool` field to `AttachDoneMsg`. Document it.

4. **`internal/tui/views.go`** — In `doAttach`, in the `mux.UseExecAttach()` branch: build `sessionTargets` map (sessionID → tmux target) from `m.appState`, create and start an `attachBellWatcher`, close it in the `ExecProcess` callback and pass result as `AttachDoneMsg.NewBells`.

5. **`internal/tui/handle_session.go`** — In `handleAttachDone`: after the existing `delete(m.bellPending, ...)` call, merge `msg.NewBells` into `m.bellPending` (union, not replace). Call `m.gridView.SetBellPending(m.bellPending)` alongside the existing `m.sidebar.SetBellPending` call.

6. **`internal/tui/components/gridview.go`** — Three changes:
   - Add `bellPending map[string]bool` field to `GridView`
   - Add `SetBellPending(bells map[string]bool)` method
   - In `renderCell`: after building `prefixStr` (dot + darkSp + badge), append `darkSp + styles.BellBadgeOnBg(darkBg)` when `gv.bellPending[sess.ID]` is true; adjust `prefixW` accordingly

7. **`internal/tui/handle_session.go`** — In `handleStatusesDetected` where `m.sidebar.SetBellPending` is called (line 198): also call `m.gridView.SetBellPending(m.bellPending)`.

### Test Strategy

- **`internal/tui/styles/theme_test.go`** — `TestBellBadgeOnBg_ContainsGlyph`: assert `BellBadgeOnBg("#000000")` contains `♪`.

- **`internal/tui/bell_watcher_test.go`** *(new file)* — unit tests for the watcher in isolation using a mock `getPaneTitlesFn`:
  - `TestAttachBellWatcher_PlaysOnNewBell`: mock returns a bell on the second poll; assert `audio.Play` called once.
  - `TestAttachBellWatcher_NoPlayOnAlreadyPending`: bell present at start (baseline); assert `audio.Play` not called.
  - `TestAttachBellWatcher_AccumulatesNewBells`: bell fires mid-watch; assert `stop()` returns the sessionID.
  - `TestAttachBellWatcher_Debounce`: two consecutive polls both with new bells; assert `audio.Play` called only once.
  - `TestAttachBellWatcher_StopIsClean`: `stop()` returns without hanging when called immediately.

- **`internal/tui/flow_bell_test.go`** — Two new flow tests:
  - `TestFlow_BellDuringAttachPlaysSound`: send `AttachDoneMsg{NewBells: map[string]bool{"sess-1": true}}`; assert `audio.Play` is NOT called (bells are pre-played by watcher, not re-played on return), but `f.model.bellPending["sess-1"]` is true.
  - `TestFlow_BellDuringAttachShowsGridBadge`: same setup; assert `f.model.gridView` bell pending state includes `"sess-1"` after `handleAttachDone`.

- **`internal/tui/components/gridview_test.go`** — `TestGridView_BellBadgeRendered`: set `bellPending` on a session, call `View()`, assert rendered output contains `♪`.

### Risks

- **`prefixW` accounting**: adding the bell badge to the prefix increases its measured width. If `prefixW` is not updated correctly, the title will overflow or truncate incorrectly. Use `ansi.StringWidth` on the full `prefixStr` after mutation (already the pattern in the existing code).
- **Goroutine leak on early return**: if `tea.ExecProcess` is cancelled before the callback fires, `done` may never be closed. Acceptable — the goroutine polls with a select on `done`, so it will naturally exit when the channel is eventually closed or the process exits. In practice `ExecProcess` always calls the callback.
- **Native backend**: watcher's `getPaneTitlesFn` returns `nil, nil, nil` — goroutine polls, gets no bells, exits cleanly. No regression.

## Implementation Notes

- Implemented exactly per plan. No deviations.
- `buildSessionTargets` extracted as a standalone helper in `bell_watcher.go` rather than inlining in `doAttach` — cleaner and directly testable.
- `BellPendingForTest` accessor added to `GridView` for test-only introspection (avoids exporting `bellPending` field or resorting to reflection).
- The watcher goroutine safely handles the native backend: `GetPaneTitles` returns `nil, nil, nil`, so the goroutine polls, sees no bells, and exits cleanly on `stop()`.
- `TestAttachBellWatcher_Debounce` uses 4 polling cycles with alternating presence/absence to create two separate bell edges; verifies both fire (separated by >500ms) and the debounce only blocks rapid-fire pairs.

- **PR:** —
