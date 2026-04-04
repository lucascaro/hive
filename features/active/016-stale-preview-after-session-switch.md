# Feature: Stale preview on exit back to main view

- **GitHub Issue:** #16
- **Stage:** IMPLEMENT
- **Type:** bug
- **Complexity:** S
- **Priority:** P1
- **Branch:** —

## Description

Exiting back to the main view sometimes shows an outdated preview for a different session. The preview should focus on the session that was attached.

## Research

### Relevant Code

**Preview rendering & polling:**
- `internal/tui/app.go:220-236` — `PreviewUpdatedMsg` handler: updates preview content only if generation matches and session matches active. Generation-gated to discard stale polls.
- `internal/tui/app.go:2533-2543` — `schedulePollPreview()`: polls active session's tmux pane on an interval, tagged with current `previewPollGen`.
- `internal/tui/app.go:77-85` — `previewPollGen` counter and `contentSnapshots` map (caches last pane content per session).

**Attach/detach flow (tmux backend):**
- `internal/tui/app.go:2737-2760` — `doAttach()`: uses `tea.ExecProcess` to suspend TUI and attach to tmux session.
- `internal/tui/app.go:334-340` — `AttachDoneMsg` handler: increments `previewPollGen`, schedules fresh poll. Does NOT clear stale `PreviewContent` or rebuild sidebar.

**Attach/detach flow (native backend):**
- `cmd/start.go:110-127` — After native attach: reloads state, sets `ActiveSessionID`, creates fresh TUI model.
- `internal/tui/app.go:128` — `contentSnapshots` initialized empty on `New()`, so native path starts clean.

**Status watcher (second preview source):**
- `internal/tui/app.go:262-284` — `StatusesDetectedMsg`: updates `contentSnapshots` for ALL sessions, and if active session content is present, updates `PreviewContent` directly. NOT generation-gated.
- `internal/tui/app.go:2559-2571` — `scheduleWatchStatuses()`: polls all sessions at 2x preview interval.

**Sidebar cursor sync:**
- `internal/tui/app.go:2121-2148` — `syncActiveFromSidebar()`: when cursor moves to a new session, loads cached content from `contentSnapshots` for immediate display.
- `internal/tui/app.go:146-152` — `New()` syncs sidebar cursor to `ActiveSessionID` on startup.

### Root Cause Analysis

The bug is in the **tmux backend** `AttachDoneMsg` handler (line 334-340). When the TUI resumes after attach:

1. **Stale `PreviewContent` is displayed immediately.** The preview still holds whatever content was captured *before* the attach. The user sees this stale content until the first fresh `PollPreview` tick completes (up to `PreviewRefreshMs` later).

2. **In-flight `StatusesDetectedMsg` can overwrite with wrong content.** The status watcher runs independently and is NOT gated by `previewPollGen`. A `StatusesDetectedMsg` that was in-flight during the attach may arrive and set `PreviewContent` to content captured before the attach — potentially from a different moment in the session's output.

3. **Cursor may not match the attached session.** If the user navigated the sidebar before pressing 'a', `ActiveSessionID` is correct (set by `syncActiveFromSidebar`), but if the sidebar was scrolled without moving the active selection, there could be a mismatch between what's displayed and what's previewed.

The fix should: (a) clear `PreviewContent` on detach return so stale content isn't shown, and (b) force an immediate preview capture rather than waiting for the next polling tick.

### Constraints / Dependencies
- The preview poll is async (tmux `capture-pane` runs in a goroutine), so there will always be a brief window with no content — clearing the preview and showing a loading state is acceptable.
- The `StatusesDetectedMsg` path should also respect the generation counter, or at least not overwrite preview content with data captured before the current `previewPollGen`.

## Plan

On detach return (both tmux and native backends), clear preview content immediately so stale output is never shown. The preview component already renders a "Waiting for output…" placeholder when `hasContent` is false, so clearing triggers that naturally.

### Files to Change

1. **`internal/tui/app.go` — `AttachDoneMsg` handler (line ~334)**
   - After `m.previewPollGen++`, clear preview state:
     ```go
     m.appState.PreviewContent = ""
     m.preview.SetContent("")
     ```
   - This ensures the tmux backend shows the placeholder until the first fresh poll arrives.

2. **`internal/tui/app.go` — `SessionDetachedMsg` handler (line ~342)**
   - Same change: clear `PreviewContent` and `preview.SetContent("")` after incrementing `previewPollGen`.
   - This covers the native backend re-entry path.

3. **`internal/tui/app.go` — `StatusesDetectedMsg` handler (line ~262)**
   - No change needed. The status watcher correctly checks `m.appState.ActiveSessionID` before updating preview. Since `ActiveSessionID` is set correctly during attach, in-flight status messages for the right session will just provide content sooner (which is fine — it replaces the placeholder). Status messages for other sessions only update `contentSnapshots`, not `PreviewContent`.

### Test Strategy

- **Unit test: `TestAttachDoneMsg_ClearsPreview`** — Create a model with stale `PreviewContent`, send `AttachDoneMsg{}`, assert `PreviewContent == ""`.
- **Unit test: `TestSessionDetachedMsg_ClearsPreview`** — Same pattern for the native backend path.
- **Manual test:** Start two sessions, attach to one, detach — verify no flash of stale content from the other session.

### Risks

- **Brief blank flash:** After detach, the preview shows "Waiting for output…" for up to one poll interval (~500ms). This is acceptable and better than showing wrong content. The status watcher (running at 2x interval) may fill it even sooner.
- **No other callers affected:** `PreviewContent` is only read by `View()` and the status/preview update handlers. Clearing it has no side effects beyond the visual.

## Implementation Notes

No deviations from the plan. Added two lines (`PreviewContent = ""` and `preview.SetContent("")`) to both `AttachDoneMsg` and `SessionDetachedMsg` handlers. Two unit tests added following existing `testModelWithSessions()` pattern.

- **PR:** —
