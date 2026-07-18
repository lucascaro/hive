// Pure key-decision helpers for the terminal's custom key handler.
//
// Extracted from main.js so the decision logic can be unit-tested with
// fake event objects (see test/unit/keymap.test.js), mirroring the
// platform.js idiom. main.js keeps only the imperative wiring.

// Byte written to the PTY to insert a newline in the agent's input
// without submitting. This is Ctrl+J (LF, 0x0a) — the one newline
// shortcut that both Claude Code and Codex accept in every terminal
// with no per-terminal configuration. Option+Enter (\x1b\r) and
// the CSI-u Shift+Enter encoding (\x1b[13;2u) only work when the
// terminal is specially configured and are documented as
// regression-prone, so we deliberately send the literal Ctrl+J byte.
export const NEWLINE_SEQ = '\x0a';

// isShiftEnter reports whether a keydown event is a bare Shift+Enter
// (no other modifier). xterm sends a plain \r for Shift+Enter — the
// Shift is dropped — so Claude/Codex can't tell it from Enter and
// submit. We intercept it here and send NEWLINE_SEQ instead. Shift+Enter
// is the cross-platform "newline in a chat input" convention and, unlike
// Cmd/Ctrl+Enter, carries no Cmd/Ctrl modifier, so it is not consumed by
// the capture-phase window shortcut handler (which gates on Cmd/Ctrl) and
// actually reaches the terminal. Plain Enter still submits.
export function isShiftEnter(e) {
  return (
    e.shiftKey &&
    !e.metaKey &&
    !e.ctrlKey &&
    !e.altKey &&
    e.key === 'Enter'
  );
}

// isHelpOverlayKey reports whether a keydown opens (or closes) the
// keyboard-shortcuts panel. Both ⌘/ and ⌘? are accepted: "?" is Shift+/
// on a US layout, so e.key is already "?" when shift is held and no
// separate shiftKey check is needed — the same shape as the '=' / '+'
// zoom pair in app/keyboard.js.
//
// The '?' branch only ever fires on Windows/Linux. On macOS the Help
// menu item's ⌘/ accelerator already matches both chords (AppKit matches
// key equivalents on the unshifted character), so the menu consumes them
// before the webview sees a keydown — see menu_darwin.go. Non-mac has no
// native menu at all, which is where this predicate earns its keep.
//
// The Cmd/Ctrl modifier is required: a bare "?" is an ordinary character
// that must reach the terminal, never the overlay. Callers that already
// gate on cmdOrCtrl() still get the right answer, since this re-checks.
export function isHelpOverlayKey(e) {
  if (!(e.metaKey || e.ctrlKey)) return false;
  return e.key === '/' || e.key === '?';
}
