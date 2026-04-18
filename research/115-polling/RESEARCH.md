# Research: Centralize Session Polling (#115)

## Current Architecture

Five independent `tea.Tick` chains run in parallel with no coordination:

### Chain 1: Sidebar Preview (`schedulePollPreview`)
- **File:** `internal/tui/handle_preview.go:289`
- **Interval:** `PreviewRefreshMs` (default 500ms)
- **Scope:** Single active session (sidebar view)
- **Capture depth:** 500 lines via `mux.CapturePane()`
- **Generation counter:** `previewPollGen` (uint64) — stale messages discarded at handle_preview.go:15-20
- **Pip stamp:** Only stamps if grid is NOT visible (prevents double-stamp)
- **Reschedule sites:** ~8 locations (session switch, create, kill, detach, grid close, every message arrival)

### Chain 2: Grid Background (`scheduleGridPoll`)
- **File:** `internal/tui/handle_preview.go:240`
- **Interval:** 500ms normal, 1000ms in input mode
- **Scope:** All grid sessions; excludes focused session during input mode (`partial=true`)
- **Capture depth:** 100 lines via `mux.BatchCapturePane()`
- **Generation counter:** `gridPollGen` (uint64) — prevents multiplying rate on rapid g/G toggles
- **Pip stamp:** Stamps all sessions in batch
- **Reschedule sites:** Grid open/toggle, mode switch, after attach, every background message arrival

### Chain 3: Grid Focused Fast Poll (`scheduleFocusedSessionPoll`)
- **File:** `internal/tui/handle_preview.go:274`
- **Interval:** 50ms (hard-coded `inputModeFocusedMs`)
- **Scope:** Single focused session during input mode only
- **Capture depth:** 100 lines via `mux.CapturePane()`
- **No generation counter** — relies on input mode flag to stop
- **Pip stamp:** Keeps pip continuously lit (50ms < 150ms flash threshold)

### Chain 4: Status Detection (`scheduleWatchStatuses`)
- **File:** `internal/tui/handle_preview.go:319`
- **Interval:** `PreviewRefreshMs * 2` (default 1000ms)
- **Scope:** All live sessions
- **Capture depth:** 50 lines + pane titles + bell flags
- **No generation counter** — self-rescheduling chain
- **IMPORTANT:** Never updates preview content (50-line vs 500-line conflict, handle_session.go:175-180)

### Chain 5: Title Polling (`scheduleWatchTitles`)
- **File:** `internal/tui/handle_preview.go:305`
- **Interval:** `PreviewRefreshMs * 2` (default 1000ms)
- **Scope:** All live sessions
- **No generation counter**

## Model Fields (app.go:77-123)

| Field | Type | Purpose |
|-------|------|---------|
| `previewPollGen` | uint64 | Sidebar preview invalidation |
| `gridPollGen` | uint64 | Grid background invalidation |
| `lastPreviewChange` | map[string]time.Time | Activity pip flash timing |
| `contentSnapshots` | map[string]string | Status change detection (50-line captures) |
| `stableCounts` | map[string]int | Debounce before status transition |
| `paneTitles` | map[string]string | Grid subtitle rendering |
| `detectionCtxs` | map[string]escape.SessionDetectionCtx | Compiled status regexes |

## Known Issues and Workarounds

1. **Parallel chain accumulation** — Rapid g/G toggles spawn new grid poll goroutines before old ones die. Generation counters prevent them from actually updating state, but the goroutines still run tmux commands until their tick fires and gets discarded.

2. **Double-stamping** — When grid is visible, sidebar preview poll runs but skips pip stamp. Grid batch already captures the active session, so stamping would show double the effective rate.

3. **Content flash on partial batch** — Input mode excludes focused session from background poll. Must use `MergeContents()` not `SetContents()` to avoid blanking the focused cell.

4. **Content depth mismatch** — Status detection (50 lines) and preview (500 lines) capture different amounts. If status detection updated preview content, scroll offset would jump between 50-line and 500-line views.

5. **No coordination between chains** — Sidebar preview and grid poll can both capture the same session in the same tick cycle, wasting tmux subprocess calls.

## Design Direction for PollingManager

A single `PollingManager` component that:
- Owns all tick scheduling (one master tick, or tiered ticks at 50ms/500ms/1000ms)
- Accepts "intent" from views: which sessions are visible, which is focused for input
- Deduplicates captures: each session captured at most once per interval
- Handles generation/cancellation centrally
- Stamps activity pips once per capture, not per chain
- Manages status detection as a sub-task of the same polling loop (shares content snapshots)
