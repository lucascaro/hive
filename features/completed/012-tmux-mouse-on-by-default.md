# Feature: tmux mouse on by default

- **GitHub Issue:** #12
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P5
- **Branch:** feature/12-tmux-mouse-on

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

Enable `mouse on` for every tmux session Hive creates, scoped to the session (not global). Add a low-level helper in the tmux package and call it from the tmux backend after session creation.

### Files to Change
1. `internal/tmux/session.go` — Add `SetOption(session, key, value string) error` function that runs `tmux set-option -t <session> <key> <value>`. Generic helper so it can be reused for future session options.
2. `internal/mux/tmux/backend.go` — In `Backend.CreateSession()`, after `tmux.CreateSession()` succeeds, call `tmux.SetOption(session, "mouse", "on")`. If SetOption fails, log/ignore — don't fail session creation over a cosmetic option.
3. `CHANGELOG.md` — Add entry under `[Unreleased]` → `Changed`: "Enable mouse support by default in tmux sessions"

### Test Strategy
- Add a unit test in `internal/tmux/session_test.go` for `SetOption` that verifies the correct tmux arguments are constructed (mock or skip if tmux not available)
- Manual verification: start Hive, create a session, confirm `tmux show-options -t <session> mouse` returns `on`
- Verify scrolling works in an attached session with the mouse

### Risks
- **Low:** `SetOption` failure after session creation — mitigated by not failing the whole creation flow
- **Low:** Mouse mode interferes with terminal text selection (users must hold Shift) — acceptable for v1, can add a config toggle later if requested

## Implementation Notes

- Implemented exactly as planned — no deviations needed.
- `SetOption` is a generic helper (takes key/value) so it can be reused for future tmux session options.
- Mouse enable failure is silently ignored (`_ = tmux.SetOption(...)`) to keep session creation robust.

- **PR:** #23
