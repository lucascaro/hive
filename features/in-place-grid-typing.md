# Feature: In-place session input from grid view

- **GitHub Issue:** —
- **Stage:** RESEARCH
- **Type:** enhancement
- **Complexity:** M
- **Priority:** —
- **Branch:** —

## Description

Users currently must attach to a session (full-screen takeover) to type into it. This feature adds an "input mode" to the grid view so users can send keystrokes to any session without leaving the grid — useful for quick commands, confirmations (y/N), or nudging an agent, without losing the multi-session overview.

## Research

### Summary

The grid displays read-only live previews via `mux.CapturePane()` (polled every 500ms). All keypresses in grid mode are consumed for navigation/commands. There is no `SendKeys` API on the `Backend` interface today. Typing requires a full `tmux attach` terminal takeover.

### Proposed Design

Introduce a vim-style **input mode** toggle on the focused grid cell:

| Key | Nav mode | Input mode |
|-----|----------|------------|
| `i` | enter input mode on selected cell | — |
| `Esc` | exit grid | exit input mode → back to nav |
| arrows | move cursor | forward to tmux session |
| printable chars | commands (`g`, `x`, `r`, …) | forward to session via SendKeys |
| `Enter` | attach full-screen | send Enter to session |
| `Ctrl+Q` (or `Esc`) | — | exit input mode |

Visual indicator: `-- INPUT --` badge (or cursor glyph) in the cell header when input mode is active.

Preview polling continues unchanged — cell content updates within ~500ms of each keystroke.

### Relevant Code

- `internal/tui/components/gridview.go:46-62` — `GridView` struct; add `inputMode bool` and `inputTarget` here
- `internal/tui/components/gridview.go:167-256` — `Update()` key dispatch; add input-mode forwarding branch
- `internal/tui/components/gridview.go:344-510` — `renderCell()`; add input-mode badge to cell header
- `internal/tui/handle_keys.go:75-226` — `handleGridKey()`; gate nav-key shortcuts behind `!inputMode`
- `internal/mux/interface.go:25-93` — `Backend` interface; add `SendKeys(target, text string) error`
- `internal/mux/tmux/` — tmux implementation: `tmux send-keys -t <target> <text> ""`
- `internal/mux/native/` — native PTY implementation: write bytes to the PTY stdin

### Constraints / Dependencies

- **Arrow keys conflict**: in input mode arrows must be forwarded to the session (cursor movement) not grid nav. `Esc` is the only exit.
- **Input latency**: no local echo; preview refresh (~500ms) is the only feedback. Acceptable for quick commands; may feel laggy for long input. A local echo buffer is a possible follow-up.
- **Scrolling**: cell shows only the last N lines. In-cell scroll is a separate feature; in-place typing is still useful without it.
- **Key conflicts**: single-char bindings (`r`, `x`, `g`, etc.) must be excluded from input mode. Using explicit `i` to enter (rather than "any printable key") avoids accidental mode entry.
- **Native backend**: needs `SendKeys` wired to PTY write; tmux backend uses `tmux send-keys`.

## Plan

_To be filled during PLAN stage._

### Files to Change

1. `internal/mux/interface.go` — add `SendKeys(target, text string) error` to `Backend`
2. `internal/mux/tmux/` — implement `SendKeys` via `tmux send-keys -t <target> <text> ""`
3. `internal/mux/native/` — implement `SendKeys` via PTY write
4. `internal/tui/components/gridview.go` — `inputMode` flag, key forwarding in `Update()`, badge in `renderCell()`
5. `internal/tui/handle_keys.go` — gate grid shortcuts behind `!inputMode`

### Test Strategy

- Unit test `SendKeys` tmux command construction
- Integration: verify grid stays visible and cell content updates after key forward
- Verify `Esc` exits input mode without exiting grid

### Risks

- Arrow key semantics change in input mode may surprise users — needs clear visual cue
- Native backend PTY write path may need locking if daemon is concurrent
