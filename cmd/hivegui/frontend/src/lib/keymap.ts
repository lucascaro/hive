// Pure key-decision helpers for the terminal's custom key handler.
//
// Extracted from main.js so the decision logic can be unit-tested with
// fake event objects (see test/unit/keymap.test.ts), mirroring the
// platform.ts idiom. main.ts keeps only the imperative wiring.

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
// is the cross-platform "newline in a chat input" convention, and it
// carries no Cmd/Ctrl modifier, so the capture-phase window shortcut
// handler (which gates on Cmd/Ctrl) never sees it and the key reaches
// the terminal. Plain Enter still submits. Cmd/Ctrl+Enter was the
// grid-project toggle until #249 unbound it; it is now inert.
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

// Bytes for "jump to start / end of the current line": Ctrl+A and
// Ctrl+E, the readline (emacs-mode) bindings that bash, zsh, fish and
// the agent CLIs all honour with no per-terminal configuration. This is
// what iTerm2's "Natural Text Editing" preset sends for the same chord.
// Escape sequences (Home/End, \x1bOH / \x1bOF) were the alternative;
// they are honoured by fewer inner programs, and a shell in vi-mode is
// the only place they'd win.
export const LINE_START_SEQ = '\x01';
export const LINE_END_SEQ = '\x05';

// macLineEditSeq maps ⌘← / ⌘→ to those bytes, or null when the event
// isn't ours.
//
// Why this has to exist: xterm.js deliberately emits NOTHING for a
// meta-modified arrow key — its handler reads `case 37: if (e.metaKey)
// break` — and the browser doesn't translate the chord either. macOS
// terminals implement ⌘←/→ as an emulator-level key mapping, not as
// something the PTY provides. So merely letting the key through to the
// terminal (which is all the app can do) produces silence; the sequence
// has to be written explicitly, the same way ⌘⌫ → Ctrl+U already is.
//
// Mac-only: on Windows/Linux the equivalent chord is Ctrl+←/→, which
// means word-wise movement and which xterm already encodes correctly as
// \x1b[1;5D / \x1b[1;5C. Remapping it there would break word jumps.
//
// Shift is allowed through as a plain move: ⇧⌘← is select-to-line-start
// in a text field, but a PTY has no selection for us to extend, so the
// cursor move is the closest honest thing. Ctrl and Alt are not — ⌥←/→
// is word-wise movement and ⌃←/→ belongs to tmux and friends.
export function macLineEditSeq(e: KeyEventLike, isMac: boolean): string | null {
  if (!isMac || !e.metaKey || e.ctrlKey || e.altKey) return null;
  if (e.key === 'ArrowLeft') return LINE_START_SEQ;
  if (e.key === 'ArrowRight') return LINE_END_SEQ;
  return null;
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
// zoom pair in app/keyboard.ts.
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
// Linux the app's primary modifier is Ctrl (see lib/platform.ts
// cmdOrCtrl), so Ctrl+- and Ctrl+= are ALREADY zoom out / in in
// app/keyboard.ts. Requiring Alt there keeps zoom intact; the
// !e.altKey branch on mac keeps ⌥⌃- free for the terminal.
//
// Callers must dispatch this BEFORE the cmdOrCtrl() gate in
// app/keyboard.ts — on macOS that gate rejects plain Ctrl outright.
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
