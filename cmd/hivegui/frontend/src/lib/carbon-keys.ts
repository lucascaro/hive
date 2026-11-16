// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// A keymap chord → the (virtual key code, Carbon modifier mask) pair the
// menu bar hands to RegisterEventHotKey for the system-wide dictation
// hotkey (spec 553). The chord grammar stays here, in the frontend that
// owns it; Go only ever sees the two numbers.
//
// Key codes are macOS kVK_* values, which name physical key positions on
// an ANSI keyboard. A chord spelled by character ('a', '-') is mapped
// through its US-layout position, the same assumption lib/chord.ts makes
// when it compares chords.
//
// Pure module: no DOM, no store.

import { parseChord } from './chord.js';

/** Carbon modifier bits (Events.h); mirrored in internal/wire. */
export const CARBON_CMD = 0x0100;
export const CARBON_SHIFT = 0x0200;
export const CARBON_OPTION = 0x0800;
export const CARBON_CONTROL = 0x1000;

export interface CarbonHotkey {
  keyCode: number;
  modifiers: number;
}

// kVK_ANSI_* by KeyboardEvent.code.
const CODE_TO_KVK: Record<string, number> = {
  KeyA: 0x00,
  KeyS: 0x01,
  KeyD: 0x02,
  KeyF: 0x03,
  KeyH: 0x04,
  KeyG: 0x05,
  KeyZ: 0x06,
  KeyX: 0x07,
  KeyC: 0x08,
  KeyV: 0x09,
  KeyB: 0x0b,
  KeyQ: 0x0c,
  KeyW: 0x0d,
  KeyE: 0x0e,
  KeyR: 0x0f,
  KeyY: 0x10,
  KeyT: 0x11,
  Digit1: 0x12,
  Digit2: 0x13,
  Digit3: 0x14,
  Digit4: 0x15,
  Digit6: 0x16,
  Digit5: 0x17,
  Equal: 0x18,
  Digit9: 0x19,
  Digit7: 0x1a,
  Minus: 0x1b,
  Digit8: 0x1c,
  Digit0: 0x1d,
  BracketRight: 0x1e,
  KeyO: 0x1f,
  KeyU: 0x20,
  BracketLeft: 0x21,
  KeyI: 0x22,
  KeyP: 0x23,
  Enter: 0x24,
  KeyL: 0x25,
  KeyJ: 0x26,
  Quote: 0x27,
  KeyK: 0x28,
  Semicolon: 0x29,
  Backslash: 0x2a,
  Comma: 0x2b,
  Slash: 0x2c,
  KeyN: 0x2d,
  KeyM: 0x2e,
  Period: 0x2f,
  Tab: 0x30,
  Space: 0x31,
  Backquote: 0x32,
  Backspace: 0x33,
  Escape: 0x35,
  F5: 0x60,
  F6: 0x61,
  F7: 0x62,
  F3: 0x63,
  F8: 0x64,
  F9: 0x65,
  F11: 0x67,
  F10: 0x6d,
  F12: 0x6f,
  F4: 0x76,
  F2: 0x78,
  F1: 0x7a,
  ArrowLeft: 0x7b,
  ArrowRight: 0x7c,
  ArrowDown: 0x7d,
  ArrowUp: 0x7e,
};

// A lower-cased e.key → the KeyboardEvent.code that types it on a US
// layout, for chords spelled by character.
const KEY_TO_CODE: Record<string, string> = {
  ' ': 'Space',
  space: 'Space',
  enter: 'Enter',
  tab: 'Tab',
  escape: 'Escape',
  backspace: 'Backspace',
  arrowleft: 'ArrowLeft',
  arrowright: 'ArrowRight',
  arrowup: 'ArrowUp',
  arrowdown: 'ArrowDown',
  '-': 'Minus',
  '=': 'Equal',
  '`': 'Backquote',
  '[': 'BracketLeft',
  ']': 'BracketRight',
  '\\': 'Backslash',
  ';': 'Semicolon',
  "'": 'Quote',
  ',': 'Comma',
  '.': 'Period',
  '/': 'Slash',
};

function codeOf(key: string): string | undefined {
  if (/^[a-z]$/.test(key)) return `Key${key.toUpperCase()}`;
  if (/^[0-9]$/.test(key)) return `Digit${key}`;
  if (/^f([1-9]|1[0-2])$/.test(key)) return key.toUpperCase();
  return KEY_TO_CODE[key];
}

/**
 * The Carbon pair for a macOS keymap chord, or null when it cannot be a
 * system-wide hotkey: an unparseable chord, a key with no fixed code, an
 * optional (`?`) or `Any` modifier — Carbon matches modifiers exactly —
 * or no modifier at all, which would swallow a plain key in every app.
 */
export function carbonHotkey(chord: string): CarbonHotkey | null {
  let c: ReturnType<typeof parseChord>;
  try {
    c = parseChord(chord, true);
  } catch {
    return null;
  }
  const wants = [c.meta, c.ctrl, c.alt, c.shift];
  if (wants.some((w) => w === 'any')) return null;
  const code = c.code ?? (c.key !== undefined ? codeOf(c.key) : undefined);
  const keyCode = code === undefined ? undefined : CODE_TO_KVK[code];
  if (keyCode === undefined) return null;
  let modifiers = 0;
  if (c.meta === true) modifiers |= CARBON_CMD;
  if (c.ctrl === true) modifiers |= CARBON_CONTROL;
  if (c.alt === true) modifiers |= CARBON_OPTION;
  if (c.shift === true) modifiers |= CARBON_SHIFT;
  if (modifiers === 0) return null;
  return { keyCode, modifiers };
}

/** The wire's GlobalHotkey (GLOBAL_HOTKEY / SET_GLOBAL_HOTKEY). */
export interface WireHotkey {
  key_code: number;
  modifiers: number;
  enabled: boolean;
}

export const DISABLED_HOTKEY: WireHotkey = Object.freeze({
  key_code: 0,
  modifiers: 0,
  enabled: false,
});

/** The capture-HUD hotkey a macOS keymap asks for: the command's first
 * chord, or disabled when it has none or none Carbon can register. */
export function desiredHotkey(chords: readonly string[]): WireHotkey {
  for (const chord of chords) {
    const h = carbonHotkey(chord);
    if (h)
      return { key_code: h.keyCode, modifiers: h.modifiers, enabled: true };
  }
  return DISABLED_HOTKEY;
}

/** Whether the daemon's copy differs from what the keymap asks for. A
 * disabled pair compares equal whatever its stale code and mask say. */
export function sameHotkey(a: WireHotkey, b: WireHotkey): boolean {
  if (!a.enabled || !b.enabled) return a.enabled === b.enabled;
  return a.key_code === b.key_code && a.modifiers === b.modifiers;
}
