# Feature: Centralize session polling behind a single PollingManager

- **GitHub Issue:** #115
- **Stage:** DONE
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

Single `PollingManager` struct that owns all tick scheduling. Views declare intent (`SetView`); the manager decides what to capture, at what interval, and deduplicates. One master `tea.Tick` chain at the fastest needed cadence (50ms in input mode, 500ms otherwise); internal counters fire slower tiers (1000ms for status/titles) as multiples of the base tick. One unified `PollTickMsg` replaces the 5 current message types.

### Design Decisions

- **Separate struct** (`PollingManager`) rather than methods on Model — isolates polling state, testable without full Model construction.
- **Single generation counter** — one `uint64` that invalidates all stale chains on any state change (replaces `previewPollGen` + `gridPollGen`).
- **Intent-based API** — `SetView(view, sessions, focusedID, inputMode)` replaces 5 separate schedule functions. Callers describe what's visible; manager decides what to capture.
- **Tiered ticks** — master tick at base interval; `time.Since(lastSlowTick)` gates the 1000ms-tier captures (status detection, title polling).
- **Existing capture functions kept initially** — `PollPreview`, `PollGridPreviews`, `PollFocusedGridPreview` become internal implementation details called from `HandleTick`. Can inline in a follow-up.

### Files to Change

1. `internal/tui/polling.go` (NEW) — `PollingManager` struct with fields: `generation uint64`, `baseInterval time.Duration`, `view ViewID`, `gridSessions []*state.Session`, `focusedSessionID string`, `inputMode bool`, `lastSlowTick time.Time`, `running bool`, plus config ref. Methods: `SetView()` (bumps generation, records intent, returns first tick cmd), `InvalidateAndReschedule()` (bumps generation, returns fresh tick), `HandleTick(PollTickMsg)` (validates generation, performs captures based on view+tier, returns next tick). Define `PollTickMsg` with `Generation uint64` and result fields for sidebar/grid/status/title data.

2. `internal/tui/polling_test.go` (NEW) — Unit tests for the manager in isolation.

3. `internal/tui/handle_preview.go` (MODIFY) — Remove `schedulePollPreview`, `scheduleGridPoll`, `scheduleFocusedSessionPoll`, `scheduleWatchTitles`, `scheduleWatchStatuses`. Replace `handlePreviewUpdated` and `handleGridPreviewsUpdated` with single `handlePollTick(PollTickMsg)` that dispatches results to sidebar/grid/status. Keep `stampPreviewPoll` and activity pip logic unchanged.

4. `internal/tui/app.go` (MODIFY) — Add `polling PollingManager` field to Model. In `Init()`, replace 5 separate schedule calls with `m.polling.SetView(...)`. Remove `previewPollGen` and `gridPollGen` fields. Add `PollTickMsg` case to `Update` switch.

5. `internal/tui/handle_keys.go` (MODIFY) — Replace ~12 call sites of `schedulePollPreview()` / `scheduleGridPoll()` / `scheduleFocusedSessionPoll()` with `m.polling.SetView(...)` or `m.polling.InvalidateAndReschedule()`.

6. `internal/tui/handle_session.go` (MODIFY) — Replace ~8 call sites in `handleSessionCreated`, `handleAttachDone`, `handleSessionDetached`, etc.

7. `internal/tui/components/gridview.go`, `internal/tui/components/preview.go` (NO CHANGE initially) — Capture functions remain as-is; called internally by `HandleTick`.

### Test Strategy

- `internal/tui/polling_test.go`:
  - `TestPollingManager_SetViewBumpsGeneration` — verify generation increments on SetView, stale PollTickMsg is discarded by HandleTick.
  - `TestPollingManager_TieredCapture` — verify fast ticks skip slow-tier captures until elapsed >= 1000ms.
  - `TestPollingManager_InputModeDeduplication` — verify focused session excluded from background grid batch; captured by fast poll only.
  - `TestPollingManager_ViewSwitchInvalidates` — verify switching from grid to sidebar stops grid polling and starts sidebar polling.
  - `TestPollingManager_NoSessionsNoPoll` — verify SetView with empty sessions returns nil cmd.
- `internal/tui/flow_grid_input_test.go` (MODIFY):
  - Update `TestGridInputMode_BackgroundPollSlowsDown` to use PollingManager API.
  - Update `TestGridInputMode_FocusedSessionExcludedFromBackgroundPoll` similarly.
  - Update `TestGridInputMode_ExitRestoresNormalPolling` similarly.
- `internal/tui/flow_test.go` (MODIFY):
  - Update any tests that call `scheduleGridPoll()` or `schedulePollPreview()` directly.

### Risks

- **Large migration surface** — ~20+ call sites need updating in one PR. Mitigation: search-and-replace `schedulePollPreview()` → `m.polling.SetView(...)` is mechanical; test suite catches regressions.
- **Tiered tick accuracy** — `time.Since(lastSlowTick)` drifts slightly from exact 1000ms. Acceptable — current code has the same drift via `tea.Tick` chains.
- **Input mode latency** — must preserve 50ms echo. Mitigation: base tick drops to 50ms when input mode active, same as current `inputModeFocusedMs`.
- **Test churn** — flow tests that directly reference `scheduleGridPoll()` etc. need updating. Mitigated by keeping the number of test-file changes small (3-4 files).

## Implementation Notes

- **Pragmatic approach:** Instead of a mega-tick that does all captures in one goroutine, the PollingManager is a coordinator that owns the generation counter and scheduling logic while reusing existing capture functions and message types. This minimizes risk and test churn.
- Existing `PreviewUpdatedMsg`, `GridPreviewsUpdatedMsg`, `StatusesDetectedMsg`, `TitlesDetectedMsg` message types kept — no new unified message type needed for this phase.
- `contentSnapshots`, `stableCounts`, `paneTitles`, `detectionCtxs` moved from Model fields into `PollingManager` struct fields.
- `previewPollGen` and `gridPollGen` replaced by single `polling.generation`.
- Schedule wrapper methods (`schedulePollPreview`, `scheduleGridPoll`, etc.) kept on Model as thin delegates to the polling manager — minimizes call-site churn while centralizing logic.
- All ~20 `previewPollGen++` / `gridPollGen++` sites replaced with `m.polling.Invalidate()`.

- **PR:** [#125](https://github.com/lucascaro/hive/pull/125)
