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
    : // A synthetic keydown (autofill) can arrive with no key at all.
      typeof e.key === 'string' && e.key.toLowerCase() === c.key;
}

// ---------- comparing chords (spec 477) ----------

// e.code → the e.key it produces on a US layout.
const CODE_KEYS: Record<string, string> = {
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
  Space: ' ',
};

// A shifted character → its unshifted key on a US layout.
const SHIFTED: Record<string, string> = {
  _: '-',
  '+': '=',
  '~': '`',
  '{': '[',
  '}': ']',
  '|': '\\',
  ':': ';',
  '"': "'",
  '<': ',',
  '>': '.',
  '?': '/',
  '!': '1',
  '@': '2',
  '#': '3',
  $: '4',
  '%': '5',
  '^': '6',
  '&': '7',
  '*': '8',
  '(': '9',
  ')': '0',
};

/** The unshifted key a chord names, and whether naming it implies Shift. */
function baseKey(c: Chord): { key: string; shifted: boolean } {
  if (c.code !== undefined) {
    const m = /^(?:Key|Digit)(\w)$/.exec(c.code);
    const key = m
      ? m[1].toLowerCase()
      : (CODE_KEYS[c.code] ?? c.code.toLowerCase());
    return { key, shifted: false };
  }
  const k = c.key ?? '';
  return k in SHIFTED
    ? { key: SHIFTED[k], shifted: true }
    : { key: k, shifted: false };
}

const compatible = (a: Want, b: Want) => a === 'any' || b === 'any' || a === b;

/**
 * Whether some key press could match both chords. Errs towards yes: a
 * shifted character (`?`, `_`) is compared as its unshifted key with
 * Shift unknown, and `[Code]` keys through a US layout. Two chords that
 * overlap must never both be live, or one key would run two commands.
 */
export function chordsOverlap(a: string, b: string, isMac: boolean): boolean {
  const ca = parseChord(a, isMac);
  const cb = parseChord(b, isMac);
  const ka = baseKey(ca);
  const kb = baseKey(cb);
  if (ka.key !== kb.key) return false;
  // A shifted character with Shift unnamed may still need Shift on the
  // user's layout, so Shift is unknown; one that names Shift keeps it.
  const shiftA: Want = ka.shifted && ca.shift === false ? 'any' : ca.shift;
  const shiftB: Want = kb.shifted && cb.shift === false ? 'any' : cb.shift;
  return (
    compatible(ca.meta, cb.meta) &&
    compatible(ca.ctrl, cb.ctrl) &&
    compatible(ca.alt, cb.alt) &&
    compatible(shiftA, shiftB)
  );
}

const MENU_KEYS: Record<string, string> = {
  arrowup: 'up',
  arrowdown: 'down',
  arrowleft: 'left',
  arrowright: 'right',
  backspace: 'backspace',
  delete: 'delete',
  enter: 'return',
  escape: 'escape',
  tab: 'tab',
  ' ': 'space',
  '+': 'plus',
};

/**
 * A macOS chord as a Wails menu accelerator (`cmdorctrl+shift+t`), or ''
 * when it cannot be one: only ⌘ chords go in the native menu, everything
 * else stays on the keydown path. Don't-care modifiers are dropped, since
 * an accelerator is exact.
 */
export function toMenuAccelerator(s: string): string {
  const c = parseChord(s, true);
  if (c.meta !== true) return '';
  let key: string;
  if (c.code !== undefined) {
    const m = /^(?:Key|Digit)(\w)$/.exec(c.code);
    const k = m ? m[1].toLowerCase() : CODE_KEYS[c.code];
    if (!k) return '';
    key = k;
  } else {
    key = MENU_KEYS[c.key ?? ''] ?? c.key ?? '';
    if (key.length !== 1 && !Object.values(MENU_KEYS).includes(key)) {
      if (!/^f\d{1,2}$/.test(key)) return '';
    }
  }
  const mods = ['cmdorctrl'];
  if (c.ctrl === true) mods.push('ctrl');
  if (c.alt === true) mods.push('optionoralt');
  if (c.shift === true) mods.push('shift');
  return [...mods, key].join('+');
}
