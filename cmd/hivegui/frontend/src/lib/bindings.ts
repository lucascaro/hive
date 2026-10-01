// ---------- the app's chords, and the user's overrides of them ----------
//
// The shipped default chords for every app command (spec 478), and the
// one resolver that lays the user's keymap (spec 477) over them. Dispatch
// (app/key-scopes.ts), every shortcut label (lib/shortcuts.ts), plugin
// collisions and the native macOS menu all read effectiveFor(), so a
// rebind reaches every surface at once and no surface can show a key
// that does nothing. app/bindings.ts binds these to the live store.
//
// Pure module: no DOM, no store.
//
// A keymap override REPLACES a command's defaults on one OS half; [] is
// "no shortcut". A command the user did not touch keeps its defaults,
// unless one of them overlaps a chord the user chose for something else:
// then that command ships unbound, rather than one key firing two
// commands or a new default silently taking the user's key.

import {
  chordsFor,
  chordsOverlap,
  parseChord,
  toMenuAccelerator,
  type Keys,
} from './chord.js';
import { chordLabel } from './chord-label.js';

/** keymap.json (spec 477). One half per platform family: a macOS
 * rebind never changes the Windows/Linux keys, and the reverse. Each
 * half maps a command id to the chords that replace its defaults; []
 * means "no shortcut". */
export interface Keymap {
  version?: 1;
  mac?: Readonly<Record<string, readonly string[]>>;
  other?: Readonly<Record<string, readonly string[]>>;
}

export const EMPTY_KEYMAP: Keymap = Object.freeze({});

export interface Binding {
  keys: Keys;
  /** Command id, or null to reserve the chord: it ends dispatch but is
   * left to the terminal, so nothing further down (a plugin) can take it. */
  command: string | null;
  /** false: a held key does not repeat the command. */
  repeat?: false;
}

// ⌘/ and ⌘? both mean the shortcuts panel: '?' is Shift+/ on a US layout.
// The '?' form only ever fires off macOS — there the Help menu's ⌘/
// accelerator takes both before the webview (menu_darwin.go).
export const HELP_CHORD: Keys = ['Mod+Shift?+/', 'Mod+Shift?+?'];

// The app's global chords. Exact matches, except where a key has always
// ignored Shift ('+' is Shift+= on a US layout; digits need Shift on
// AZERTY). Each entry is ONE shortcut: several chords in an entry are
// spellings of the same key on different layouts, and its label is the
// first. Order only matters between chords that overlap, and none do.
export const DEFAULT_APP_BINDINGS: readonly Binding[] = [
  // Ctrl+` opens an OS terminal at the active session's worktree. Ctrl on
  // every platform, mirroring VS Code: macOS reserves ⌘` for window
  // cycling.
  { keys: 'Ctrl+[Backquote]', command: 'open-os-terminal' },
  // Session back / forward. Ctrl on macOS but Ctrl+Alt elsewhere, where
  // plain Ctrl+- is already zoom out. '_' is shifted '-', and [Minus]
  // covers layouts that produce neither. Known limitation off macOS:
  // AltGr reports as Ctrl+Alt, so a layout where AltGr+'-' composes a
  // character loses it — VS Code carries the same tradeoff.
  {
    keys: {
      mac: ['Ctrl+-', 'Ctrl+_', 'Ctrl+[Minus]'],
      other: ['Ctrl+Alt+-', 'Ctrl+Alt+_', 'Ctrl+Alt+[Minus]'],
    },
    command: 'nav-back',
  },
  {
    keys: {
      mac: ['Ctrl+Shift+-', 'Ctrl+Shift+_', 'Ctrl+Shift+[Minus]'],
      other: ['Ctrl+Alt+Shift+-', 'Ctrl+Alt+Shift+_', 'Ctrl+Alt+Shift+[Minus]'],
    },
    command: 'nav-forward',
  },
  // Agent activity (spec 416). Not plain Ctrl+J off macOS: that is byte
  // 0x0a, the newline Claude Code documents for every terminal.
  {
    keys: {
      mac: ['Mod+J', 'Mod+[KeyJ]'],
      other: ['Ctrl+Shift+J', 'Ctrl+Shift+[KeyJ]'],
    },
    command: 'toggle-activity',
  },
  {
    keys: {
      mac: ['Mod+Shift+J', 'Mod+Shift+[KeyJ]'],
      other: ['Ctrl+Alt+Shift+J', 'Ctrl+Alt+Shift+[KeyJ]'],
    },
    command: 'activity-grid',
  },
  // Find in session (spec 431). Not plain Ctrl+F off macOS: that is 0x06,
  // readline's forward-char. On macOS the native ⌘F accelerator takes the
  // key before the webview and the menu event runs the same command; the
  // chord is here so the menu's accelerator is derived from it.
  {
    keys: { mac: 'Mod+F', other: ['Ctrl+Shift+F', 'Ctrl+Shift+[KeyF]'] },
    command: 'find-in-session',
  },

  { keys: ['Mod+Shift?+=', 'Mod+Shift?++'], command: 'zoom-in' },
  { keys: ['Mod+Shift?+-', 'Mod+Shift?+_'], command: 'zoom-out' },
  { keys: 'Mod+Shift?+0', command: 'zoom-reset' },
  { keys: 'Mod+Shift+K', command: 'command-palette' },
  // ⌘⏎ zooms into the tile you navigated to, from a grid only (the
  // command declines in single view). ONE-WAY on purpose: Claude and
  // Codex bind Cmd+Enter themselves (spec #217), so in single view the
  // key must reach the terminal, and ⇧⌘⏎ stays unclaimed in every view.
  { keys: 'Mod+Shift+Enter', command: null },
  { keys: 'Mod+Enter', command: 'focus-active-session' },
  { keys: HELP_CHORD, command: 'keyboard-shortcuts' },
  // ⌘, — the standard Settings chord. On macOS the File menu carries the
  // same accelerator; Windows/Linux have no native menu, so this is the
  // only path there.
  { keys: 'Mod+Shift?+,', command: 'settings' },
  { keys: 'Mod+P', command: 'duplicate-session' },
  { keys: 'Mod+Shift+P', command: 'duplicate-session-choose-tool' },
  { keys: 'Mod+T', command: 'new-session' },
  { keys: 'Mod+Shift+T', command: 'new-session-worktree' },
  { keys: 'Mod+Shift+Backspace', command: 'delete-project' },
  { keys: 'Mod+E', command: 'worktrees' },
  { keys: 'Mod+I', command: 'quick-idea' },
  { keys: 'Mod+Shift+I', command: 'idea-inbox' },
  { keys: 'Mod+S', command: 'toggle-sidebar' },
  { keys: 'Mod+G', command: 'toggle-project-grid' },
  { keys: 'Mod+Shift+G', command: 'toggle-all-grid' },
  // ⌘N — new project. (⌥⌘N is reserved by macOS Spotlight.)
  { keys: 'Mod+N', command: 'new-project' },
  { keys: 'Mod+Shift+N', command: 'new-window' },
  { keys: 'Mod+B', command: 'next-attention' },
  { keys: 'Mod+Shift+B', command: 'jump-back' },
  { keys: 'Mod+W', command: 'close-session' },
  { keys: 'Mod+Shift+W', command: 'close-window' },
  // ⌘Z undoes the close you just made. ⇧⌘Z reads as redo, which this has
  // no counterpart for, so it is reserved rather than handed to a plugin.
  { keys: 'Mod+Z', command: 'reopen-closed-session' },
  { keys: 'Mod+Shift+Z', command: null },
  ...Array.from({ length: 9 }, (_, i) => ({
    keys: `Mod+Shift?+${i + 1}`,
    command: `switch-${i + 1}`,
  })),
  // Horizontal arrows are only ours in a grid: in focused mode ⌘←/⌘→ are
  // start/end-of-line in the terminal, and the command declines.
  { keys: 'Mod+Shift?+ArrowLeft', command: 'grid-left' },
  { keys: 'Mod+Shift?+ArrowRight', command: 'grid-right' },
  { keys: 'Mod+ArrowUp', command: 'prev-session' },
  { keys: 'Mod+ArrowDown', command: 'next-session' },
  // ⇧⌘↑/↓ reorder the active session in every view — the same command
  // the Session menu's Move items run (spec 477 unified the two).
  { keys: 'Mod+Shift+ArrowUp', command: 'move-backward' },
  { keys: 'Mod+Shift+ArrowDown', command: 'move-forward' },
  { keys: 'Mod+Shift?+[', command: 'prev-project' },
  { keys: 'Mod+Shift?+]', command: 'next-project' },
];

// Chords the terminal owns through hand-written predicates rather than
// bindings (app/session-term.ts, lib/keymap.ts), plus the null
// reservations above. Nothing may bind them: a plugin is refused one, and
// the Shortcuts settings tab refuses them too.
export const RESERVED_CHORDS: {
  readonly mac: readonly string[];
  readonly other: readonly string[];
} = {
  mac: [
    'Mod+Shift+Enter',
    'Mod+Shift+Z',
    'Shift+Enter',
    'Mod+ArrowLeft',
    'Mod+ArrowRight',
    'Mod+Backspace',
    'Mod+Delete',
    'Alt+Delete',
  ],
  other: [
    'Mod+Shift+Enter',
    'Mod+Shift+Z',
    'Shift+Enter',
    'Ctrl+Shift+C',
    'Ctrl+Shift+V',
    'Ctrl+Shift+A',
  ],
};

type Overrides = Readonly<Record<string, readonly string[]>>;

export interface Effective {
  bindings: readonly Binding[];
  /** Commands left with no shortcut because a default overlapped a chord
   * the user gave another command (spec 477, criterion 8). */
  displaced: readonly string[];
  /** Override chords that do not parse, as `id: chord`; ignored. */
  invalid: readonly string[];
}

/** The overrides that apply on this platform. */
export function keymapHalf(keymap: Keymap, isMac: boolean): Overrides {
  return (isMac ? keymap.mac : keymap.other) ?? {};
}

/**
 * Lays one OS half of a keymap over a default binding table. Overrides go
 * first, so even an overlap this misses resolves to the user's choice.
 * `skip` filters which override ids belong to this table (plugin ids are
 * resolved by the plugin host).
 */
export function effectiveBindings(
  defaults: readonly Binding[],
  overrides: Overrides,
  isMac: boolean,
  skip: (id: string) => boolean = (id) => id.startsWith('plugin:'),
): Effective {
  const invalid: string[] = [];
  const out: Binding[] = [];
  const userChords: string[] = [];
  const repeatOf = (id: string) =>
    defaults.find((b) => b.command === id)?.repeat;
  for (const [id, chords] of Object.entries(overrides)) {
    if (skip(id) || !Array.isArray(chords)) continue;
    for (const chord of chords) {
      try {
        parseChord(chord, isMac);
      } catch {
        invalid.push(`${id}: ${chord}`);
        continue;
      }
      const repeat = repeatOf(id);
      out.push(
        repeat === false
          ? { keys: chord, command: id, repeat }
          : { keys: chord, command: id },
      );
      userChords.push(chord);
    }
  }
  const displaced = new Set<string>();
  for (const b of defaults) {
    if (b.command === null || b.command in overrides) continue;
    const hit = chordsFor(b.keys, isMac).some((c) =>
      userChords.some((u) => chordsOverlap(c, u, isMac)),
    );
    if (hit) displaced.add(b.command);
  }
  for (const b of defaults) {
    if (
      b.command !== null &&
      (b.command in overrides || displaced.has(b.command))
    )
      continue;
    out.push(b);
  }
  return { bindings: out, displaced: [...displaced], invalid };
}

// ---------- resolving a keymap ----------

const cache = new WeakMap<Keymap, { mac?: Effective; other?: Effective }>();

/** The app's bindings under a keymap, cached per keymap object (a new
 * keymap is always a new object). Warns once about chords it ignores. */
export function effectiveFor(keymap: Keymap, isMac: boolean): Effective {
  let entry = cache.get(keymap);
  if (!entry) {
    entry = {};
    cache.set(keymap, entry);
  }
  const half = isMac ? 'mac' : 'other';
  let eff = entry[half];
  if (!eff) {
    eff = effectiveBindings(
      DEFAULT_APP_BINDINGS,
      keymapHalf(keymap, isMac),
      isMac,
    );
    for (const bad of eff.invalid) console.warn(`keymap: ignoring ${bad}`);
    entry[half] = eff;
  }
  return eff;
}

/** Every chord that runs a command, layout spellings included. */
export function chordsOfIn(
  keymap: Keymap,
  id: string,
  isMac: boolean,
): string[] {
  return effectiveFor(keymap, isMac).bindings.flatMap((b) =>
    b.command === id ? chordsFor(b.keys, isMac) : [],
  );
}

/** A command's shortcuts, one chord each (the first spelling). */
export function shortcutsIn(
  keymap: Keymap,
  id: string,
  isMac: boolean,
): string[] {
  return effectiveFor(keymap, isMac).bindings.flatMap((b) => {
    if (b.command !== id) return [];
    const first = chordsFor(b.keys, isMac)[0];
    return first ? [first] : [];
  });
}

/** Whether a command still has its shipped shortcuts under a keymap. */
export function isDefaultIn(
  keymap: Keymap,
  id: string,
  isMac: boolean,
): boolean {
  return (
    !(id in keymapHalf(keymap, isMac)) &&
    !effectiveFor(keymap, isMac).displaced.includes(id)
  );
}

/** What to show for a command's shortcut: '⇧⌘K', 'Ctrl+T / Ctrl+Y', or
 * '' when it has none. */
export function labelIn(keymap: Keymap, id: string, isMac: boolean): string {
  return shortcutsIn(keymap, id, isMac)
    .map((c) => chordLabel(c, isMac))
    .join(' / ');
}

// ---------- the native macOS menu ----------

function accelFor(bindings: readonly Binding[], id: string): string {
  for (const b of bindings) {
    if (b.command !== id) continue;
    const first = chordsFor(b.keys, true)[0];
    const a = first ? toMenuAccelerator(first) : '';
    if (a) return a;
  }
  return '';
}

/** Each native-menu command's accelerator under the shipped defaults. */
export function defaultMenuAccelerators(
  ids: readonly string[],
): Record<string, string> {
  return Object.fromEntries(
    ids.map((id) => [id, accelFor(DEFAULT_APP_BINDINGS, id)]),
  );
}

/** The menu items whose accelerator the keymap changes, by command id
 * ('' = no accelerator). Empty when the keymap changes none. */
export function menuAcceleratorOverrides(
  ids: readonly string[],
  keymap: Keymap,
): Record<string, string> {
  const eff = effectiveFor(keymap, true).bindings;
  const out: Record<string, string> = {};
  for (const id of ids) {
    const want = accelFor(eff, id);
    if (want !== accelFor(DEFAULT_APP_BINDINGS, id)) out[id] = want;
  }
  return out;
}
