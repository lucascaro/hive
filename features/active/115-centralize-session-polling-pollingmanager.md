# Feature: Centralize session polling behind a single PollingManager

- **GitHub Issue:** #115
- **Stage:** RESEARCH
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

<Filled during RESEARCH stage.>

### Relevant Code
- `internal/tui/handle_preview.go` — current `schedulePollPreview` / `scheduleGridPoll` / `scheduleFocusedSessionPoll` and `gridPollGen` / `previewPollGen` plumbing
- `internal/tui/components/preview.go` — `PollPreview` tea.Cmd
- `internal/tui/components/gridview.go` — `PollGridPreviews` / `PollFocusedGridPreview`
- `internal/tui/components/preview_activity.go` — pip-stamp consumer
- `internal/tui/handle_keys.go`, `internal/tui/handle_session.go`, `internal/tui/app.go` — ~37 call sites that schedule polls

### Constraints / Dependencies
- Bubble Tea's tick model: each `tea.Tick` is a one-shot; "intervals" are reschedule chains. Manager must drive its own internal scheduler.
- Active-session content must stay fresh while grid is on top (so closing grid shows current content).
- Input-mode focused-cell echo must remain at ~50 ms latency.

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
