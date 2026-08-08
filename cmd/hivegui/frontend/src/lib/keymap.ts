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
// Structural, not `KeyboardEvent`: the unit tests build plain fakes.
// `code` is optional — only navHistoryKey's layout fallback reads it.
export interface KeyEventLike {
  key: string;
  code?: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
}

export function isShiftEnter(e: KeyEventLike): boolean {
  return (
    e.shiftKey && !e.metaKey && !e.ctrlKey && !e.altKey && e.key === 'Enter'
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
export function isHelpOverlayKey(e: KeyEventLike): boolean {
  if (!(e.metaKey || e.ctrlKey)) return false;
  return e.key === '/' || e.key === '?';
}

// navHistoryKey reports whether a keydown is session back/forward
// navigation. Returns 'back' | 'forward' | null.
//
//   macOS         Ctrl+-        / Ctrl+Shift+-
//   Win / Linux   Ctrl+Alt+-    / Ctrl+Alt+Shift+-
//
// The split mirrors VS Code, and for the same reason: on Windows and
// Linux the app's primary modifier is Ctrl (see lib/platform.js
// cmdOrCtrl), so Ctrl+- and Ctrl+= are ALREADY zoom out / in in
// app/keyboard.js. Requiring Alt there keeps zoom intact; the
// !e.altKey branch on mac keeps ⌥⌃- free for the terminal.
//
// Callers must dispatch this BEFORE the cmdOrCtrl() gate in
// app/keyboard.js — on macOS that gate rejects plain Ctrl outright.
//
// Key matching accepts '-' and '_' (shifted '-' on a US layout, the
// same shape as the '=' / '+' zoom pair) and falls back to the
// physical e.code === 'Minus' for layouts where neither is produced.
//
// Known limitation on Windows/Linux: AltGr reports as ctrlKey+altKey,
// so on a layout where AltGr+'-' composes a character this swallows it.
// Deliberately NOT guarded with e.getModifierState('AltGraph') — that
// flag is set inconsistently for a manually-held Ctrl+Alt across X11
// setups, and breaking the binding outright on Linux is worse than the
// narrow collision. If it is ever reported, the AltGraph check is the
// fix. (VS Code carries the same tradeoff on the same chord.)
export function navHistoryKey(
  e: KeyEventLike,
  isMac: boolean,
): 'back' | 'forward' | null {
  if (e.metaKey || !e.ctrlKey) return null;
  if (isMac ? e.altKey : !e.altKey) return null;
  if (!(e.key === '-' || e.key === '_' || e.code === 'Minus')) return null;
  return e.shiftKey ? 'forward' : 'back';
}
