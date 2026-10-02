// Editing a keymap (spec 477, Settings › Shortcuts): turning a key press
// into a chord, deciding whether a chord may be bound, and the keymap
// edits the tab makes. lib/bindings.ts resolves a keymap; this module
// only produces new ones.
//
// Pure module: no DOM, no store.

import { RESERVED_CHORDS, keymapHalf, type Keymap } from './bindings.js';
import { chordsOverlap, parseChord, type KeyEventLike } from './chord.js';
import { chordLabel } from './chord-label.js';

// ---------- capturing a key press ----------

const MODIFIER_KEYS = new Set([
  'Meta',
  'Control',
  'Alt',
  'Shift',
  'OS',
  'AltGraph',
  'CapsLock',
  'Fn',
  'Hyper',
  'Super',
]);

// Keys named by their e.key, as the default bindings write them.
const NAMED_KEYS = new Set([
  'ArrowUp',
  'ArrowDown',
  'ArrowLeft',
  'ArrowRight',
  'Enter',
  'Backspace',
  'Delete',
  'Escape',
  'Tab',
  'Home',
  'End',
  'PageUp',
  'PageDown',
  'Insert',
]);

// Punctuation keys, captured by their physical key: ⌥ and ⇧ rewrite the
// character, but not the code.
const PUNCTUATION_CODES = new Set([
  'Minus',
  'Equal',
  'Backquote',
  'BracketLeft',
  'BracketRight',
  'Backslash',
  'Semicolon',
  'Quote',
  'Comma',
  'Period',
  'Slash',
  'Space',
]);

function keyToken(e: KeyEventLike): string | null {
  if (NAMED_KEYS.has(e.key) || /^F([1-9]|1\d|2[0-4])$/.test(e.key))
    return e.key;
  const letter = /^Key([A-Z])$/.exec(e.code);
  if (letter) {
    // The typed letter when it is one (AZERTY's A is on KeyQ); the
    // physical key when ⌥ or a non-Latin layout typed something else.
    return /^[a-z]$/i.test(e.key) ? e.key.toUpperCase() : `[${e.code}]`;
  }
  const digit = /^Digit(\d)$/.exec(e.code);
  if (digit) return /^\d$/.test(e.key) ? e.key : `[${e.code}]`;
  if (PUNCTUATION_CODES.has(e.code)) return `[${e.code}]`;
  return null;
}

export type Captured =
  | { kind: 'chord'; chord: string }
  | { kind: 'refused'; reason: string };

/**
 * The chord a key press names, as a keymap would store it, or null for a
 * press that names nothing yet (a modifier on its own). On macOS ⌘ is
 * written `Mod` and ⌃ `Ctrl`; elsewhere Ctrl is `Mod`.
 */
export function captureChord(e: KeyEventLike, isMac: boolean): Captured | null {
  if (MODIFIER_KEYS.has(e.key)) return null;
  if (!isMac && e.metaKey) {
    return {
      kind: 'refused',
      reason: 'The Windows key cannot be part of a Hive shortcut.',
    };
  }
  const key = keyToken(e);
  if (!key) {
    return { kind: 'refused', reason: 'Hive cannot bind that key.' };
  }
  const mods: string[] = [];
  if (isMac ? e.metaKey : e.ctrlKey) mods.push('Mod');
  if (isMac && e.ctrlKey) mods.push('Ctrl');
  if (e.altKey) mods.push('Alt');
  if (e.shiftKey) mods.push('Shift');
  return { kind: 'chord', chord: [...mods, key].join('+') };
}

// ---------- which chords may be bound ----------

// Chords the OS or the window manager takes before Hive could see them,
// or that every user expects to keep their usual meaning. Refused.
export const OS_RESERVED: {
  readonly mac: readonly string[];
  readonly other: readonly string[];
} = {
  mac: [
    'Mod+Q',
    'Mod+H',
    'Alt+Mod+H',
    'Mod+M',
    'Ctrl+Mod+F',
    'Mod+X',
    'Mod+C',
    'Mod+V',
    'Mod+A',
    'Alt+Shift+Mod+V',
    'Mod+[Backquote]',
    'Alt+Mod+N',
    'Mod+Tab',
    'Mod+[Space]',
    'Ctrl+C',
  ],
  other: ['Mod+C', 'Alt+F4', 'Alt+Tab'],
};

export type ChordCheck =
  | { kind: 'ok' }
  | { kind: 'warn'; reason: string }
  | { kind: 'refused'; reason: string };

const overlapsAny = (chord: string, list: readonly string[], isMac: boolean) =>
  list.some((c) => chordsOverlap(c, chord, isMac));

/**
 * Whether a chord may be bound: refused when the OS or the terminal owns
 * it, or when it has no modifier (a plain key would stop the user typing
 * it into a session); a warning when it reaches programs in a session
 * that may want it themselves.
 */
export function checkChord(chord: string, isMac: boolean): ChordCheck {
  const label = chordLabel(chord, isMac);
  const half = isMac ? 'mac' : 'other';
  if (overlapsAny(chord, RESERVED_CHORDS[half], isMac)) {
    return {
      kind: 'refused',
      reason: `${label} belongs to the terminal (copy, paste, line editing or a key agents use), so it cannot be rebound.`,
    };
  }
  if (overlapsAny(chord, OS_RESERVED[half], isMac)) {
    return {
      kind: 'refused',
      reason: `${label} is reserved by ${isMac ? 'macOS' : 'the system'}.`,
    };
  }
  const c = parseChord(chord, isMac);
  const fKey = /^f\d+$/.test(c.key ?? '');
  if (c.meta !== true && c.ctrl !== true && c.alt !== true && !fKey) {
    return {
      kind: 'refused',
      reason: `A shortcut needs ${isMac ? '⌘, ⌃ or ⌥' : 'Ctrl or Alt'}: ${label} on its own would stop you typing it in a session.`,
    };
  }
  // ⌘ never reaches a terminal program on macOS. Off macOS Ctrl does, as
  // control characters, unless Shift or Alt comes with it.
  const reachesTerminal = isMac
    ? c.meta !== true && (c.ctrl === true || c.alt === true)
    : (c.ctrl === true && c.shift !== true && c.alt !== true) ||
      (c.alt === true && c.ctrl !== true);
  if (reachesTerminal) {
    return {
      kind: 'warn',
      reason: `Programs in a session can use ${label} too; Hive will take it from them.`,
    };
  }
  return { kind: 'ok' };
}

// ---------- editing ----------

/**
 * The keymap with one command's shortcuts on this platform replaced, or
 * reset to its defaults when `chords` is undefined. The other platform's
 * half, and every other command, are left as they are.
 */
export function withShortcuts(
  keymap: Keymap,
  isMac: boolean,
  id: string,
  chords: readonly string[] | undefined,
): Keymap {
  const half = isMac ? 'mac' : 'other';
  const next: Record<string, readonly string[]> = {
    ...keymapHalf(keymap, isMac),
  };
  if (chords === undefined) delete next[id];
  else next[id] = [...chords];
  return { ...keymap, [half]: next };
}

/** The keymap with every command on this platform at its defaults. */
export function resetHalf(keymap: Keymap, isMac: boolean): Keymap {
  return { ...keymap, [isMac ? 'mac' : 'other']: {} };
}

/** A command that holds a chord, and the shortcuts it has now. */
export interface Holder {
  id: string;
  shortcuts: readonly string[];
}

/**
 * Gives `chord` to `id`, taking it from each holder: a holder keeps its
 * other shortcuts and loses only the ones that overlap the chord.
 */
export function reassign(
  keymap: Keymap,
  isMac: boolean,
  id: string,
  current: readonly string[],
  chord: string,
  holders: readonly Holder[],
): Keymap {
  let next = keymap;
  for (const h of holders) {
    next = withShortcuts(
      next,
      isMac,
      h.id,
      h.shortcuts.filter((s) => !chordsOverlap(s, chord, isMac)),
    );
  }
  return withShortcuts(next, isMac, id, [...current, chord]);
}

// ---------- reading ----------

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

/**
 * A keymap from untrusted JSON: keymap.json as Go hands it over, or an
 * imported file. Anything malformed is dropped on its own, not the whole
 * keymap: a half that is not an object, an entry that is not a list (a
 * hand-typed `null`), a chord that is not a string. `malformed` names each
 * entry that lost something, as `half: id`.
 */
export function keymapFromJSON(raw: unknown): {
  keymap: Keymap;
  malformed: string[];
} {
  const malformed: string[] = [];
  const out: { mac?: Keymap['mac']; other?: Keymap['other'] } = {};
  if (!isRecord(raw)) return { keymap: out, malformed };
  for (const half of ['mac', 'other'] as const) {
    const src = raw[half];
    if (src === undefined) continue;
    if (!isRecord(src)) {
      malformed.push(half);
      continue;
    }
    const entries: [string, string[]][] = [];
    for (const [id, chords] of Object.entries(src)) {
      if (!Array.isArray(chords)) {
        malformed.push(`${half}: ${id}`);
        continue;
      }
      const strings = chords.filter((c): c is string => typeof c === 'string');
      if (strings.length !== chords.length) malformed.push(`${half}: ${id}`);
      // [null] is not [] ("no shortcut"): nothing usable means defaults.
      if (chords.length > 0 && strings.length === 0) continue;
      entries.push([id, strings]);
    }
    // fromEntries defines own properties, so a "__proto__" id stays data.
    out[half] = Object.fromEntries(entries);
  }
  return { keymap: out, malformed };
}

// ---------- comparing ----------

function sortedHalf(
  half: Readonly<Record<string, readonly string[]>> | undefined,
): Record<string, readonly string[]> | undefined {
  if (!half) return undefined;
  const ids = Object.keys(half).sort();
  if (!ids.length) return undefined;
  return Object.fromEntries(ids.map((id) => [id, [...half[id]]]));
}

/**
 * A keymap in one canonical form — version dropped, empty halves dropped,
 * command ids sorted — so two keymaps that mean the same thing compare
 * equal as JSON whatever order or empty fields they were written with.
 */
export function canonicalKeymap(k: Keymap): Keymap {
  const out: { mac?: Keymap['mac']; other?: Keymap['other'] } = {};
  const mac = sortedHalf(k.mac);
  const other = sortedHalf(k.other);
  if (mac) out.mac = mac;
  if (other) out.other = other;
  return out;
}

export function sameKeymap(a: Keymap, b: Keymap): boolean {
  return (
    JSON.stringify(canonicalKeymap(a)) === JSON.stringify(canonicalKeymap(b))
  );
}
