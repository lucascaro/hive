// A chord string (lib/chord.ts) as the text a user reads: '⇧⌘K' on macOS,
// 'Ctrl+Shift+K' elsewhere. Every shortcut hint in the app is built here
// from the binding data, so a hint cannot name a key the binding does not
// use (spec 477).
//
// Pure module: no DOM, unit-testable.

import { parseChord } from './chord.js';

const NAMED: Record<string, { mac: string; other: string }> = {
  enter: { mac: '↩', other: 'Enter' },
  backspace: { mac: '⌫', other: 'Backspace' },
  delete: { mac: '⌦', other: 'Del' },
  escape: { mac: 'Esc', other: 'Esc' },
  arrowup: { mac: '↑', other: 'Up' },
  arrowdown: { mac: '↓', other: 'Down' },
  arrowleft: { mac: '←', other: 'Left' },
  arrowright: { mac: '→', other: 'Right' },
  ' ': { mac: 'Space', other: 'Space' },
  tab: { mac: '⇥', other: 'Tab' },
};

const CODE_LABELS: Record<string, string> = {
  Minus: '-',
  Equal: '=',
  Backquote: '`',
  BracketLeft: '[',
  BracketRight: ']',
  Backslash: '\\',
  Semicolon: ';',
  Quote: "'",
  Comma: ',',
  Period: '.',
  Slash: '/',
};

/** The key part of a label: 'K', '↩' / 'Enter', '`'. */
export function keyLabel(key: string, isMac: boolean): string {
  const named = NAMED[key.toLowerCase()];
  if (named) return isMac ? named.mac : named.other;
  return key.length === 1 ? key.toUpperCase() : key;
}

/**
 * The label for one chord. Modifiers marked `?` are left out: the chord
 * fires without them, and the label names the plain form.
 */
export function chordLabel(s: string, isMac: boolean): string {
  const c = parseChord(s, isMac);
  let key: string;
  if (c.code !== undefined) {
    const m = /^(?:Key|Digit)(\w)$/.exec(c.code);
    key = m ? m[1].toUpperCase() : (CODE_LABELS[c.code] ?? c.code);
  } else {
    key = keyLabel(c.key ?? '', isMac);
  }
  const ctrl = c.ctrl === true;
  const alt = c.alt === true;
  const shift = c.shift === true;
  const meta = c.meta === true;
  if (isMac) {
    // Apple's order: ⌃⌥⇧⌘.
    return `${ctrl ? '⌃' : ''}${alt ? '⌥' : ''}${shift ? '⇧' : ''}${meta ? '⌘' : ''}${key}`;
  }
  const mods = [ctrl && 'Ctrl', alt && 'Alt', shift && 'Shift', meta && 'Meta'];
  return [...mods.filter(Boolean), key].join('+');
}

/** 'Worktrees (⌘E)', or just 'Worktrees' for a command with no key. */
export function withKey(label: string, key: string): string {
  return key ? `${label} (${key})` : label;
}
