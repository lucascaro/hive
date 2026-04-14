# Feature: In-place session input from grid view

- **GitHub Issue:** #105
- **Stage:** DONE
- **PR:** #106
- **Type:** enhancement
- **Complexity:** M
- **Priority:** —
- **Branch:** feature/grid-input-mode

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

## Implementation Notes

- `Ctrl+Q` exits input mode (not `Esc`); `Esc` is forwarded as `\033` to the session per user request
- `DisableGridInput bool` config field (JSON: `disable_grid_input`) added to `config.Config` for opt-out
- `GridView.InputEnabled` field is set from config on model construction and on each settings save
- `InputMode()` uses a value receiver so it can be called on the non-pointer `gridView` field in `Model`
- The `INPUT · C-Q` badge is overlaid at the right edge of the selected cell header, replacing the last N chars so total header width is preserved
- `keyToBytes()` handles printable runes, Enter→`\r`, Esc→`\033`, arrows→ANSI seqs, and common `Ctrl+` codes
- Keys that have no sensible byte representation (e.g. F1) return `""` and are silently ignored
- Dual-rate polling in input mode: focused session polls at 50 ms (`inputModeFocusedMs`), all others at 250 ms (`inputModeBackgroundMs`). `handleGridPreviewsUpdated` uses `MergeContents` for fast-poll messages to avoid blanking non-focused cells between background sweeps
- `GridView.Hide()` now also clears `inputMode` so re-opened grids always start in nav mode
- `(i) input` added to `GridKeyMap.ShortHelp()` for hint-bar discoverability
- First-use hint: `ViewGridInputHint` overlay shown on first activation; `d` sets `HideGridInputHint=true`, `esc`/`q` also exits input mode

## Plan

### Files to Change

1. **`internal/tmux/window.go`** — Add `SendKeys(target, keys string) error` using `tmux send-keys -t <target> -l -- <text>` (the `-l` flag sends raw bytes, bypassing tmux key-name interpretation)

2. **`internal/mux/interface.go`** — Add `SendKeys(target, keys string) error` to the `Backend` interface; add package-level `mux.SendKeys` forwarding function with nil-guard

3. **`internal/mux/tmux/backend.go`** — Implement `Backend.SendKeys` delegating to `tmux.SendKeys`

4. **`internal/mux/native/pane.go`** — Add `writeInput(keys string) error` that writes bytes directly to `p.ptm` (the PTY master fd); no locking needed — PTY writes are async-safe

5. **`internal/mux/native/manager.go`** — Add `sendKeys(target, keys string) error` calling `paneByTarget(target).writeInput(keys)`

6. **`internal/mux/native/protocol.go`** — Add `Keys string` field to `Request`

7. **`internal/mux/native/daemon.go`** — Add `"send_keys"` case in `handleConn` dispatching to `mgr.sendKeys(req.Target, req.Keys)`

8. **`internal/mux/native/backend_unix.go`** — Implement `Backend.SendKeys` as `b.client.do(Request{Op: "send_keys", Target: target, Keys: keys})`

9. **`internal/mux/native/backend_windows.go`** — Add `SendKeys` stub returning `errors.New("not supported")`

10. **`internal/mux/muxtest/mock.go`** — Add `SendKeys` to mock; add exported `LastSentTarget`, `LastSentKeys` fields for test assertions

11. **`internal/config/config.go`** — Add `DisableGridInput bool` field (default false = feature enabled; set to true to opt out)

12. **`internal/tui/components/gridview.go`**:
    - Add `inputMode bool` field to `GridView`
    - Add `InputMode() bool` accessor (used by `handle_keys.go` and tests)
    - Add `keyToBytes(msg tea.KeyMsg) string` helper: printable runes → `string(msg.Runes)`; enter → `"\r"`; **esc → `"\033"`** (forwarded to session); backspace/delete → `"\x7f"`; tab → `"\t"`; arrows → ANSI seqs (`"\033[A/B/C/D"`); common ctrl+keys (`\x01`–`\x1a`)
    - Modify `Update()`:
      - If `inputMode`: **`Ctrl+Q` → exit input mode** (`gv.inputMode = false`); all other keys (including `Esc`) → return cmd calling `mux.SendKeys(target, keyToBytes(msg))`
      - If `!inputMode`: add `"i"` case — if selected session exists, `gv.inputMode = true`
    - Modify `renderCell()`: when `gv.inputMode && selected`, append `·· INPUT ··` to the header after the title

13. **`internal/tui/handle_keys.go`** — At the top of `handleGridKey()` (after ctrl+c check), add early return: `if m.gridView.InputMode() { cmd, _ := m.gridView.Update(msg); return cmd }` — this bypasses all nav shortcuts when in input mode. Also gate `"i"` activation behind `!m.cfg.DisableGridInput`

14. **`internal/tui/keys.go`** — Add `InputMode key.Binding` to `GridKeyMap`; add a static `(i) input` entry to `ShortHelp()` (hidden if `DisableGridInput` is set — pass config bool through); also add `ctrl+q: exit input` entry shown only when `InputMode()` is true

15. **`CHANGELOG.md`** — Add `[Unreleased]` entry under `Added`

16. **`docs/keybindings.md`** — Document `i` to enter input mode and `Ctrl+Q` to exit in grid view

### Test Strategy

**Unit tests** (`internal/tui/components/gridview_input_test.go`):
- `TestKeyToBytes_PrintableRunes` — `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}` → `"y"`
- `TestKeyToBytes_SpecialKeys` — enter → `"\r"`, backspace → `"\x7f"`, esc → `"\033"`, up → `"\033[A"`, etc.

**Functional flow tests** (`internal/tui/grid_input_flow_test.go`):
- `TestGridInputMode_EnterAndExit` — open grid (`g`), press `i` → `gridView.InputMode()` is true; press `Ctrl+Q` → false; grid stays open
- `TestGridInputMode_EscForwarded` — in input mode, press `Esc` → `mock.LastSentKeys == "\033"` (not exit)
- `TestGridInputMode_KeysForwarded` — in input mode, press `y` → `mock.CallCount("SendKeys") == 1`, `mock.LastSentKeys == "y"`
- `TestGridInputMode_NavSuppressedInInputMode` — in input mode, press `x` → no ConfirmActionMsg; key goes to SendKeys
- `TestGridInputMode_ArrowsForwarded` — in input mode, press `tea.KeyUp` → `mock.LastSentKeys == "\033[A"` (not navigation)
- `TestGridInputMode_DisabledByConfig` — with `cfg.DisableGridInput = true`, press `i` → `gridView.InputMode()` remains false

### Risks

- `Ctrl+Q` is already used by some terminals/shells; conflicts unlikely in grid view context but noted
- `tmux send-keys -l` requires tmux ≥ 1.3 (released 2009; safe to assume)
- Native backend PTY write: `p.ptm.Write()` is already exercised by the attach path; no additional locking needed
