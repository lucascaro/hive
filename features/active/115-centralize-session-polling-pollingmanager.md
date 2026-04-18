# Feature: Centralize session polling behind a single PollingManager

- **GitHub Issue:** #115
- **Stage:** PLAN
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P1
- **Branch:** —

## Description

Session-preview polling is currently spread across multiple ad-hoc tea.Cmd chains
(`schedulePollPreview` for the active session, `scheduleGridPoll` for grid background,
`scheduleFocusedSessionPoll` for grid input mode), each with its own generation counter,
reschedule logic, and view-coupling rules. This has caused several rate-inconsistency
bugs: parallel chains spawned by g/G mode toggles, focused-cell double-stamping in
input mode, and active-session double-stamping while grid is on top.

Refactor polling into a single `PollingManager` that owns the entire schedule. Views
declare *which sessions are currently visible* (and any per-session hints like
"focused for input echo"); the manager decides what to capture, at what interval, and
deduplicates so each session is polled at most once per tick. Input mode becomes a
flag passed to the manager rather than a second parallel chain. The activity-pip
stamp becomes a single source of truth driven by the manager.

Goals:
- One scheduling authority — no more parallel `tea.Tick` chains per view.
- Views push intent ("these sessions are visible, this one is focused-for-input"),
  not commands.
- Consistent per-session polling cadence, observable via the activity panel.
- Generation/cancellation handled centrally.

## Research

Detailed findings in `research/115-polling/RESEARCH.md`.

### Current Architecture: 5 Independent Polling Chains

| Chain | Function | Interval | Scope | Generation counter? |
|-------|----------|----------|-------|-------------------|
| Sidebar preview | `schedulePollPreview()` | 500ms | Single active session | Yes (`previewPollGen`) |
| Grid background | `scheduleGridPoll()` | 500ms (1000ms in input mode) | All grid sessions (excl. focused in input mode) | Yes (`gridPollGen`) |
| Grid focused | `scheduleFocusedSessionPoll()` | 50ms | Single focused session in input mode | No |
| Status detection | `scheduleWatchStatuses()` | 1000ms | All live sessions | No |
| Title polling | `scheduleWatchTitles()` | 1000ms | All live sessions | No |

### Relevant Code
- `internal/tui/handle_preview.go` — all 5 scheduling functions, generation counter invalidation, pip stamping logic, reschedule handlers
- `internal/tui/handle_preview.go:289` — `schedulePollPreview()`: single-session sidebar poll
- `internal/tui/handle_preview.go:240` — `scheduleGridPoll()`: batch grid poll with input-mode partial exclusion
- `internal/tui/handle_preview.go:274` — `scheduleFocusedSessionPoll()`: 50ms fast poll for input mode echo
- `internal/tui/handle_preview.go:319` — `scheduleWatchStatuses()`: status detection at 2× interval
- `internal/tui/handle_preview.go:305` — `scheduleWatchTitles()`: title polling at 2× interval
- `internal/tui/components/preview.go` — `PollPreview` tea.Cmd (500-line capture)
- `internal/tui/components/gridview.go:56-102` — `PollGridPreviews` / `PollFocusedGridPreview` (100-line capture)
- `internal/tui/app.go:77-123` — Model fields: `previewPollGen`, `gridPollGen`, `lastPreviewChange`, `contentSnapshots`, `stableCounts`, `paneTitles`, `detectionCtxs`
- `internal/tui/app.go:336-345` — `Init()` starts sidebar preview + status chains; grid poll conditional on `HasView(ViewGrid)`

### Reschedule Call Sites (~20+)
- Session switched/created/killed → reschedule sidebar preview (handle_session.go)
- Grid open/mode toggle → reschedule grid poll + bump `gridPollGen` (handle_keys.go)
- Input mode enter/exit → start/stop focused poll (handle_keys.go)
- Grid close → reschedule sidebar preview (handle_keys.go:268)
- Every message arrival → self-reschedule (handle_preview.go)
- Detach → reschedule sidebar preview (handle_session.go)

### Known Workarounds in Current Code
1. **Double-stamping prevention** — sidebar `PreviewUpdatedMsg` skips pip stamp when grid is visible (handle_preview.go:22-25)
2. **Partial batch in input mode** — background grid poll excludes focused session; uses `MergeContents()` to prevent flash (handle_preview.go:56-62)
3. **Stale chain invalidation** — generation counters prevent rapid g/G toggles from multiplying effective rate (handle_preview.go:41-43)
4. **Content length conflict** — `WatchStatuses` captures 50 lines vs `PollPreview` captures 500; WatchStatuses is banned from updating preview content (handle_session.go:175-180)

### Constraints / Dependencies
- Bubble Tea's tick model: each `tea.Tick` is a one-shot; "intervals" are reschedule chains. Manager must drive its own internal scheduler.
- Active-session content must stay fresh while grid is on top (so closing grid shows current content).
- Input-mode focused-cell echo must remain at ~50 ms latency.
- Status detection and title polling capture 50 lines; preview captures 500 lines. Must remain separate capture depths.
- 5 chains with different intervals (50ms / 500ms / 1000ms) — manager needs per-chain cadence, not one global tick.
- Activity pip must stamp exactly once per session per poll cycle regardless of how many chains capture it.

## Plan

<Filled during PLAN stage.>

### Files to Change
1. `path/to/file.go` — <what and why>

### Test Strategy
- <how to verify>

### Risks
- <what could go wrong>

## Implementation Notes

<Filled during IMPLEMENT stage.>

- **PR:** —
