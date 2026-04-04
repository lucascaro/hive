# Feature: Terminal flashes previous content when attaching/detaching sessions

- **GitHub Issue:** #30
- **Stage:** IMPLEMENT
- **Type:** bug
- **Complexity:** M
- **Priority:** P2
- **Branch:** —

## Description

When attaching to or detaching from a session, the terminal briefly flashes the content that was displayed before hive was started. This creates a jarring visual glitch during transitions.

### Steps to Reproduce

1. Have some content visible in the terminal
2. Start hive
3. Attach to a session (or detach from one back to grid view)
4. Observe a brief flash of the pre-hive terminal content

### Expected Behavior

Transitions between grid view and full-screen sessions should be seamless without flashing the underlying terminal content.

## Research

### Root Cause

The app uses `tea.WithAltScreen()` (cmd/start.go:102) which puts the TUI in the
terminal's alternate screen buffer. When `tea.ExecProcess` runs the tmux attach
script, Bubble Tea calls `RestoreTerminal()` which **exits the alt screen**
(`\x1b[?1049l`). This briefly reveals the **main screen buffer** — whatever was
visible before hive started — before `tmux attach-session` takes over the terminal.

The same flash happens in reverse on detach: tmux exits, Bubble Tea's `setupTerm()`
re-enters alt screen, but there's a timing gap where the main buffer is visible.

### Attach/Detach Flow (tmux backend)

1. User presses `a` → `doAttach()` → `tea.ExecProcess(cmd, callback)` (app.go:2766-2774)
2. Bubble Tea `RestoreTerminal()` → **exits alt screen** → flash of main buffer
3. Shell script runs `tmux attach-session` → tmux takes over terminal
4. User detaches → tmux exits → shell script restores tmux settings
5. `tea.ExecProcess` callback fires `AttachDoneMsg`
6. Bubble Tea `setupTerm()` → **re-enters alt screen** → flash of main buffer again
7. TUI resumes rendering (app.go:340-353)

### Relevant Code
- `cmd/start.go:101-104` — `tea.WithAltScreen()` initialization
- `internal/tui/app.go:2752-2775` — `doAttach()` dispatches `tea.ExecProcess`
- `internal/tui/app.go:2804-2849` — `buildAttachScript()` shell wrapper
- `internal/tui/app.go:340-353` — `AttachDoneMsg` handler (post-detach recovery)
- `internal/tui/app_test.go:500-501` — comment noting `RestoreTerminal()` doesn't fully restore state

### Constraints / Dependencies
- `tea.ExecProcess` always calls `RestoreTerminal()` — this is Bubble Tea internals, not easily overridden
- Previous `tea.ClearScreen` calls were deliberately removed (commit 1ec33a7) because they caused their own flicker
- The native backend has a different path (`RunAttach` + quit/restart loop) which may also be affected

### Possible Approaches

1. **Clear screen in the attach script** — Add `clear` or `printf '\e[2J\e[H'` as the first command in `buildAttachScript()` before `tmux attach-session`. This would replace the stale main buffer content with a blank screen, making the flash less jarring (black instead of old content). Similarly add a clear after tmux exits.

2. **Use tmux's own alt screen** — The attach script could enter an alternate screen buffer itself (`printf '\e[?1049h'`) before attaching, and exit it after detach. This would hide the main buffer during the transition.

3. **Patch Bubble Tea** — Override or wrap the ExecProcess behavior to avoid exiting alt screen. More invasive and couples to BT internals.

## Plan

Wrap the attach script in its own alt screen buffer so the main buffer (pre-hive
content) is never visible during transitions.

### Files to Change
1. `internal/tui/app.go` — In `buildAttachScript()`: add `printf '\e[?1049h\e[2J'` as first line (enter alt screen + clear), `trap "printf '\\e[?1049l'" EXIT` for cleanup, and `printf '\e[?1049l'` as last line (exit alt screen)
2. `internal/tui/app_test.go` — Update `TestBuildAttachScript` to verify alt screen sequences are present

### Test Strategy
- Existing tests pass (with updates for new sequences)
- Visual: start hive with visible terminal content, attach/detach, confirm no flash

### Risks
- Terminal compatibility: `\e[?1049h/l` is widely supported; old terminals without alt screen see a no-op
- Trap cleanup ensures alt screen is exited even if tmux attach fails

## Implementation Notes

Added two lines at the start of the generated shell script in `buildAttachScript()`:
- `printf '\033[?1049h\033[2J'` — enters alt screen and clears it
- `trap "printf '\\033[?1049l'" EXIT` — ensures alt screen is exited on any exit path

No deviations from the plan. The `\e` escape was written as `\033` for POSIX sh
compatibility.

- **PR:** —
