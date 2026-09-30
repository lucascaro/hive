// Chord strings: the data form of a key binding (spec 478).
//
//   'Mod+Shift+K'        ⌘⇧K on macOS, Ctrl+Shift+K elsewhere
//   'Ctrl+[Backquote]'   Ctrl on every platform, matched on e.code
//   'Mod+Shift?+='       Shift is "don't care" — '=' and '+' share a key
//   'Any+Escape'         every modifier the chord does not name is "don't care"
//
// A modifier the chord does not name must be OFF, unless the chord starts
// with `Any+`. That is what makes a binding exact by default: ⌥⌘T is not
// ⌘T. `Mod` is ⌘ on macOS and Ctrl elsewhere, and like cmdOrCtrl() it
// rejects the other one (⌘⌃T is neither).
//
// The key is an e.key value, compared case-insensitively so caps lock does
// not change the answer, or `[Code]` for e.code — the physical key, for
// chords whose e.key a layout or ⌥ rewrites.
//
// Pure module: no DOM, unit-testable.

export interface KeyEventLike {
  key: string;
  code: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
}

type Want = boolean | 'any';

export interface Chord {
  meta: Want;
  ctrl: Want;
  alt: Want;
  shift: Want;
  /** Lower-cased e.key, or undefined when the chord matches on code. */
  key?: string;
  code?: string;
}

type ChordList = string | readonly string[];

/** One chord, several, or a per-platform pair of either. */
export type Keys = ChordList | { mac?: ChordList; other?: ChordList };

const MODIFIERS = new Set(['Mod', 'Ctrl', 'Alt', 'Shift']);

export function parseChord(s: string, isMac: boolean): Chord {
  // The key is everything after the last '+', except that '+' is itself
  // a key: 'Mod+Shift?++' binds the plus sign.
  const cut = s.endsWith('++') ? s.length - 2 : s.lastIndexOf('+');
  const keyTok = cut < 0 ? s : s.slice(cut + 1);
  const modToks = cut < 0 ? [] : s.slice(0, cut).split('+');
  if (!keyTok) throw new Error(`chord "${s}": no key`);

  const any = modToks[0] === 'Any';
  const rest: Want = any ? 'any' : false;
  const c: Chord = { meta: rest, ctrl: rest, alt: rest, shift: rest };
  for (const tok of any ? modToks.slice(1) : modToks) {
    const optional = tok.endsWith('?');
    const name = optional ? tok.slice(0, -1) : tok;
    if (!MODIFIERS.has(name))
      throw new Error(`chord "${s}": bad token "${tok}"`);
    const want: Want = optional ? 'any' : true;
    if (name === 'Mod') {
      if (isMac) c.meta = want;
      else c.ctrl = want;
    } else if (name === 'Ctrl') c.ctrl = want;
    else if (name === 'Alt') c.alt = want;
    else c.shift = want;
  }
  const code = /^\[(\w+)\]$/.exec(keyTok);
  if (code) c.code = code[1];
  else c.key = keyTok.toLowerCase();
  return c;
}

/** The chords that apply on this platform. */
export function chordsFor(keys: Keys, isMac: boolean): string[] {
  const list: ChordList | undefined =
    typeof keys === 'string' || Array.isArray(keys)
      ? (keys as ChordList)
      : isMac
        ? (keys as { mac?: ChordList }).mac
        : (keys as { other?: ChordList }).other;
  if (list === undefined) return [];
  return typeof list === 'string' ? [list] : [...list];
}

const agrees = (want: Want, got: boolean) => want === 'any' || want === got;

export function chordMatches(c: Chord, e: KeyEventLike): boolean {
  if (
    !agrees(c.meta, e.metaKey) ||
    !agrees(c.ctrl, e.ctrlKey) ||
    !agrees(c.alt, e.altKey) ||
    !agrees(c.shift, e.shiftKey)
  ) {
    return false;
  }
  return c.code !== undefined
    ? e.code === c.code
    : e.key.toLowerCase() === c.key;
}
