# Feature: Fix mouse support

- **GitHub Issue:** #13
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Branch:** —

## Description

Enable clicking on previews to attach to that session. Mouse interactions should work in the main view for session selection.

## Research

### Key Finding: Mouse Breaks After First Attach/Detach Cycle

The TUI has comprehensive mouse handling code, but **mouse stops working after attaching to a session and detaching back**. This is a bubbletea `RestoreTerminal()` bug.

**Root cause chain:**
1. User attaches to a session → `doAttach()` → `tea.ExecProcess()` (app.go:2761)
2. bubbletea calls `ReleaseTerminal()` → `restoreTerminalState()` → **`p.disableMouse()`** (bubbletea tty.go:45)
3. User detaches → `ExecProcess` callback fires → bubbletea calls `RestoreTerminal()` (bubbletea tea.go:885)
4. `RestoreTerminal()` re-enables alt screen, bracketed paste, focus reporting — but **never re-enables mouse**
5. Mouse is now dead for the rest of the session

**Fix:** In the `AttachDoneMsg` handler (app.go:334), return `tea.EnableMouseCellMotion` to re-enable mouse after each attach/detach cycle.

**Native backend is unaffected:** It uses `tea.Quit` + restart loop (`cmd/start.go:99-104`), creating a fresh `tea.NewProgram` with `WithMouseCellMotion()` each time.

### Mouse Handling (Already Implemented)

| Interaction | Location | Effect |
|-------------|----------|--------|
| Left-click | Sidebar session | Select session + load preview |
| Left-click | Preview pane | Attach to active session |
| Left-click | Grid cell | Select + attach session |
| Wheel scroll | Sidebar | Navigate sessions |
| Wheel scroll | Preview | Scroll output ±3 lines |
| Left-click | Project/Team row | Toggle collapse/expand |

### Relevant Code
- `cmd/start.go:101-104` — `tea.NewProgram` with `tea.WithMouseCellMotion()` enables mouse on startup
- `internal/tui/app.go:334-342` — `AttachDoneMsg` handler — **missing mouse re-enable** (the fix goes here)
- `internal/tui/app.go:2761` — `tea.ExecProcess` call in `doAttach()` — triggers the ReleaseTerminal/RestoreTerminal cycle
- `internal/tui/app.go:607-608` — `tea.MouseMsg` case routes to `handleMouse()`
- `internal/tui/app.go:1311-1410` — `handleMouse()` — main mouse event router
- `internal/tui/app.go:1412-1447` — `handleSidebarClick()` — sidebar click processing
- `internal/tui/components/gridview.go:328-358` — `CellAt()` — grid click detection
- `internal/tui/components/sidebar.go:184-196` — `ItemAtRow()` — sidebar row mapping
- `internal/tui/layout.go:12-42` — `computeLayout()` — sidebar/preview width split

### Constraints / Dependencies
- **bubbletea bug:** `RestoreTerminal()` (tea.go:885-917) does not re-enable mouse, though it restores alt screen, bracketed paste, and focus reporting. This is a known gap — mouse state (`withMouseCellMotion`) is checked at startup (tea.go:664) but not in `RestoreTerminal`.
- **Separate from #12:** Issue #12 enables mouse *inside* tmux sessions (for scrolling agent output). This issue is about the TUI's own mouse handling breaking after attach.
- **Only affects tmux backend:** The native backend restarts the entire `tea.Program`, getting a fresh mouse init each time.

## Plan

Re-enable mouse cell motion after `tea.ExecProcess` returns by batching `tea.EnableMouseCellMotion` into the `AttachDoneMsg` handler's return command.

### Files to Change
1. `internal/tui/app.go` — In the `AttachDoneMsg` handler (line ~334), change the return to batch `tea.EnableMouseCellMotion` alongside `m.schedulePollPreview()`. Before: `return m, m.schedulePollPreview()`. After: `return m, tea.Batch(tea.EnableMouseCellMotion, m.schedulePollPreview())`.
2. `CHANGELOG.md` — Add entry under `[Unreleased] > Fixed`: "Fix mouse clicks not working after detaching from a session"

### Test Strategy
- Add a test in `internal/tui/app_test.go` that sends `AttachDoneMsg{}` to the model and asserts the returned `tea.Cmd` produces an `enableMouseCellMotionMsg` (verify via `tea.BatchMsg` inspection, matching existing test patterns in `flow_test.go`)
- Manual: start hive, attach to a session, detach, verify sidebar clicks and preview clicks still work

### Risks
- `tea.EnableMouseCellMotion` is a `func() tea.Msg` (a `tea.Cmd`), not a `tea.Msg`. Must be passed as a cmd to `tea.Batch`, not called directly. Low risk — same pattern used elsewhere in bubbletea apps.

## Implementation Notes

- No deviations from plan — single-line fix in `AttachDoneMsg` handler, wrapping return cmd with `tea.Batch(tea.EnableMouseCellMotion, m.schedulePollPreview())`
- Test uses `fmt.Sprintf("%T")` to check for unexported `tea.enableMouseCellMotionMsg` in the batch, since bubbletea doesn't export the msg type
- All checks pass: `go build`, `go vet`, `go test ./...`

- **PR:** #24
