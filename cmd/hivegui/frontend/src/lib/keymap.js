// Pure key-decision helpers for the terminal's custom key handler.
//
// Extracted from main.js so the decision logic can be unit-tested with
// fake event objects (see test/unit/keymap.test.js), mirroring the
// platform.js idiom. main.js keeps only the imperative wiring.

import { isMac } from './platform.js';

// Byte written to the PTY to insert a newline in the agent's input
// without submitting. This is Ctrl+J (LF, 0x0a) — the one newline
// shortcut that both Claude Code and Codex accept in every terminal
// with no per-terminal configuration. Option+Enter (\x1b\r) and
// Shift+Enter (CSI-u \x1b[13;2u) only work when the terminal is
// specially configured and are documented as regression-prone, so we
// deliberately do not emulate them.
export const NEWLINE_SEQ = '\x0a';

// isCmdEnter reports whether a keydown event is a bare macOS Cmd+Enter
// (no other modifier). On macOS the terminal sends a plain \r for Enter
// and drops the Cmd modifier, so the agent CLI cannot tell Cmd+Enter
// from Enter and submits. We intercept it here and send NEWLINE_SEQ
// instead. Gated to mac + Cmd only: plain Enter still submits, and
// Shift/Option/Ctrl+Enter and every non-mac platform are left untouched.
export function isCmdEnter(e, mac = isMac) {
  return (
    mac &&
    e.metaKey &&
    !e.ctrlKey &&
    !e.altKey &&
    !e.shiftKey &&
    e.key === 'Enter'
  );
}
