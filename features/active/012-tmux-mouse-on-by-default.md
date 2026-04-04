# Feature: tmux mouse on by default

- **GitHub Issue:** #12
- **Stage:** PLAN
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P5
- **Branch:** —

## Description

Enable mouse support in tmux by default so users can easily scroll through output.

## Research

### Relevant Code

- `internal/tmux/session.go:10-19` — `CreateSession()` creates detached tmux sessions via `tmux new-session -d`. Does NOT set any session options (including mouse). This is the natural place to enable mouse after session creation.
- `internal/tmux/client.go:11,23` — `Exec()` and `ExecSilent()` — low-level helpers to run arbitrary tmux commands. `ExecSilent` is used for fire-and-forget commands like `set-option`.
- `internal/mux/tmux/backend.go:24-26` — `Backend.CreateSession()` wraps `tmux.CreateSession()`. Could set mouse option here, or in the lower-level tmux package.
- `internal/tui/app.go:2782-2838` — `buildAttachScript()` — the existing pattern for setting/restoring tmux session options during attach. Uses `tmux set-option -t <session>` for status bar options and restores originals on detach. Mouse should NOT follow this pattern (it should persist, not be attach-only).
- `internal/tui/app.go:103` — `tea.WithMouseCellMotion()` — enables mouse in the Bubble Tea TUI itself (not in tmux sessions).
- `internal/config/config.go` — User config struct. No tmux-specific options exist currently.

### Constraints / Dependencies

- **Scope is tmux sessions only** — the native PTY backend doesn't use tmux, so mouse there is handled by the terminal emulator + Bubble Tea directly.
- **Session-level vs server-level** — `tmux set-option -t <session> mouse on` is scoped to a single session. `tmux set-option -g mouse on` would affect ALL tmux sessions globally (including non-hive ones). Session-level is the correct choice to avoid side effects.
- **No user opt-out currently** — there's no config toggle for this. Since mouse mode can interfere with terminal copy-paste (users must hold Shift to select text), a config option to disable it may be desirable. However, for an S-complexity feature, enabling by default with no toggle is acceptable as a first pass.
- **Timing** — the option must be set AFTER `CreateSession` returns (the session must exist). It can be a follow-up `ExecSilent("set-option", "-t", session, "mouse", "on")` call.

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
