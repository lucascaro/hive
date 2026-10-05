// The shortcut lists the help overlay (⌘/) and the command palette show.
// Every key here is DERIVED from the binding data in lib/bindings.ts under
// the user's keymap (spec 477), so a surface cannot show a key the app
// does not bind, and a rebind shows up everywhere at once.
//
// What stays hand-written: the group a command sits in and its overlay
// wording, the rows that are not commands (mouse gestures, terminal
// editing, overlay keys), and the compact text of a few merged rows
// (⌘1–⌘9, the arrows). A merged row keeps its compact text only while
// every command in it has its default keys; otherwise it splits into one
// live row per command.
//
// The drift surface for a GUI binding change is now:
//   1. the binding — lib/bindings.ts (chord data), naming a command in
//      app/commands.ts (with its palette title)
//   2. the native macOS menu's DEFAULT accelerator — menu_darwin.go,
//      pinned to the binding data by
//      cmd/hivegui/testdata/menu-default-accelerators.json
//   3. the user-facing shortcut table in README.md
//
// UI plugins add chords at runtime (spec 471, app/plugin-host.ts). They
// are not in this file: the host appends them to the palette and to a
// "Plugins" group in the overlay, and refuses any that overlaps a core
// chord.
//
// Pure module: no DOM, unit-testable.

import {
  EMPTY_KEYMAP,
  effectiveFor,
  isDefaultIn,
  labelIn,
  type Keymap,
} from './bindings.js';

export interface Shortcut {
  keys: string;
  label: string;
}

export interface ShortcutGroup {
  title: string;
  items: Shortcut[];
}

interface ModOpts {
  shift?: boolean;
}

// key: a printable key or a symbolic name from KEYS below.
// Typed as a plain Record so an arbitrary printable key (`'T'`, `'['`)
// indexes cleanly and falls through to the `k ? … : key` default.
const KEYS: Record<string, { mac: string; other: string }> = {
  enter: { mac: '↩', other: 'Enter' },
  backspace: { mac: '⌫', other: 'Backspace' },
  delete: { mac: '⌦', other: 'Del' },
  up: { mac: '↑', other: 'Up' },
  down: { mac: '↓', other: 'Down' },
  left: { mac: '←', other: 'Left' },
  right: { mac: '→', other: 'Right' },
};

function keyLabel(key: string, isMac: boolean): string {
  const k = KEYS[key];
  return k ? (isMac ? k.mac : k.other) : key;
}

// cmd ("⌘" / "Ctrl+"), optionally with shift ("⇧⌘" / "Ctrl+Shift+").
//
// Exported because components render bindings inline too — a button's
// title, a modal's key hint (AGENTS.md › Key Discoverability). A
// hardcoded "⌘E" in a component tells a Windows or Linux user to press
// a key their keyboard does not have.
export function mod(
  isMac: boolean,
  key: string,
  { shift = false }: ModOpts = {},
): string {
  const k = keyLabel(key, isMac);
  if (isMac) return (shift ? '⇧⌘' : '⌘') + k;
  return (shift ? 'Ctrl+Shift+' : 'Ctrl+') + k;
}

// Ctrl on every platform (Ctrl+`, Ctrl+Shift+C/V/A — deliberately not
// ⌘ on mac, see main.tsx comments).
function ctrl(
  isMac: boolean,
  key: string,
  { shift = false }: ModOpts = {},
): string {
  const k = keyLabel(key, isMac);
  if (isMac) return (shift ? '⌃⇧' : '⌃') + k;
  return (shift ? 'Ctrl+Shift+' : 'Ctrl+') + k;
}

// Arrow-key sequences: mac glyphs read fine run together (↑↓←→);
// word labels need separators so non-mac renders "Up/Down/Left/Right"
// instead of the unreadable "UpDownLeftRight".
function arrowSeq(isMac: boolean, ...keys: string[]): string {
  return keys.map((k) => keyLabel(k, isMac)).join(isMac ? '' : '/');
}

interface Opts {
  isMac: boolean;
  /** The user's keymap; the shipped defaults when omitted. */
  keymap?: Keymap;
}

type Row =
  | Shortcut
  | { command: string; label: string }
  | {
      /** Shown as one row with `keys` while every member is at its
       * default; otherwise one row per member. */
      merged: readonly { command: string; label: string }[];
      keys: string;
      label: string;
    };

function rows(list: Row[], keymap: Keymap, isMac: boolean): Shortcut[] {
  const one = (command: string, label: string): Shortcut[] => {
    const keys = labelIn(keymap, command, isMac);
    return keys ? [{ keys, label }] : [];
  };
  return list.flatMap((r) => {
    if ('command' in r) return one(r.command, r.label);
    if ('merged' in r) {
      return r.merged.every((m) => isDefaultIn(keymap, m.command, isMac))
        ? [{ keys: r.keys, label: r.label }]
        : r.merged.flatMap((m) => one(m.command, m.label));
    }
    return [r];
  });
}

export function shortcutGroups({
  isMac,
  keymap = EMPTY_KEYMAP,
}: Opts): ShortcutGroup[] {
  const groups = rawGroups(isMac);
  return groups.map((g) => ({
    title: g.title,
    items: rows(g.items, keymap, isMac),
  }));
}

/** A command row in Settings › Shortcuts, worded as in the help overlay. */
export interface CommandRow {
  command: string;
  label: string;
}

// The overlay groups whose rows are not app commands: their keys belong to
// the terminal or to an overlay, and are not rebindable (spec 477
// non-goals). The terminal one is listed read-only in the Shortcuts tab.
const TERMINAL_GROUP = 'Inside a terminal';
const FIXED_GROUPS = new Set([
  TERMINAL_GROUP,
  'Ended session',
  'Launcher & dialogs',
]);

/** The help overlay's command groups, one row per command (merged rows
 * split), for Settings › Shortcuts. */
export function commandGroups(
  isMac: boolean,
): { title: string; items: CommandRow[] }[] {
  return rawGroups(isMac)
    .filter((g) => !FIXED_GROUPS.has(g.title))
    .map((g) => ({
      title: g.title,
      items: g.items.flatMap((r): CommandRow[] =>
        'merged' in r
          ? [...r.merged]
          : 'command' in r
            ? [{ command: r.command, label: r.label }]
            : [],
      ),
    }));
}

/** The terminal's own keys: listed in Settings › Shortcuts, not editable. */
export function terminalShortcuts(isMac: boolean): Shortcut[] {
  const g = rawGroups(isMac).find((x) => x.title === TERMINAL_GROUP);
  return (g?.items ?? []).filter(
    (r): r is Shortcut => 'keys' in r && !('merged' in r),
  );
}

function rawGroups(isMac: boolean): { title: string; items: Row[] }[] {
  const m = (key: string, opts?: ModOpts) => mod(isMac, key, opts);
  const c = (key: string, opts?: ModOpts) => ctrl(isMac, key, opts);
  const vArrows = arrowSeq(isMac, 'up', 'down');
  const hArrows = arrowSeq(isMac, 'left', 'right');
  const groups: { title: string; items: Row[] }[] = [
    {
      title: 'Sessions',
      items: [
        { command: 'new-session', label: 'New session' },
        {
          command: 'new-session-worktree',
          label: 'New session in git worktree',
        },
        { command: 'duplicate-session', label: 'Duplicate session' },
        {
          command: 'duplicate-session-choose-tool',
          label: 'Duplicate session (choose tool)',
        },
        {
          command: 'take-over-session',
          label: 'Take an ACP session over in a terminal',
        },
        {
          command: 'hand-back-session',
          label: 'Hand a taken-over session back to ACP',
        },
        { command: 'close-session', label: 'Close session' },
        { command: 'reopen-closed-session', label: 'Reopen closed session' },
        {
          merged: Array.from({ length: 9 }, (_, i) => ({
            command: `switch-${i + 1}`,
            label: `Switch to session ${i + 1}`,
          })),
          keys: `${m('1')}–${m('9')}`,
          label: 'Switch to session 1–9',
        },
        {
          merged: [
            {
              command: 'next-session',
              label: 'Next session (grid: move down)',
            },
            {
              command: 'prev-session',
              label: 'Previous session (grid: move up)',
            },
          ],
          keys: `${isMac ? '⌘' : 'Ctrl+'}${vArrows}`,
          label: 'Next / previous session (grid: move between tiles)',
        },
        {
          merged: [
            { command: 'grid-left', label: 'Grid: move left' },
            { command: 'grid-right', label: 'Grid: move right' },
          ],
          keys: `${isMac ? '⌘' : 'Ctrl+'}${hArrows}`,
          // The terminal half differs by platform: macLineEditSeq maps ⌘←/→
          // to \x01/\x05 on mac only, so off mac the chord falls through to
          // xterm's \x1b[1;5D/C — word movement, which is what the "Inside a
          // terminal" group lists it as. Saying "start / end of line" on both
          // would contradict that row.
          label: `Grid: move between tiles — in focused mode these reach the terminal (${
            isMac ? 'start / end of line' : 'move by word'
          })`,
        },
        {
          merged: [
            { command: 'move-forward', label: 'Move session forward (wraps)' },
            {
              command: 'move-backward',
              label: 'Move session backward (wraps)',
            },
          ],
          keys: `${isMac ? '⇧⌘' : 'Ctrl+Shift+'}${vArrows}`,
          label: 'Reorder session within its project (wraps)',
        },
        {
          command: 'nav-back',
          label: 'Go back to the previously visited session',
        },
        { command: 'nav-forward', label: 'Go forward again' },
        {
          command: 'next-attention',
          label: 'Next session needing attention (bell)',
        },
        { command: 'jump-back', label: 'Jump back to where you were' },
        { keys: 'Double-click', label: 'Rename (sidebar row or tile title)' },
        {
          keys: `${isMac ? '⌘-' : 'Ctrl+'}click`,
          label:
            'Open the file path under the cursor (programs are revealed, not run)',
        },
        {
          keys: `${isMac ? '⇧⌘-' : 'Ctrl+Shift+'}click`,
          label: 'Open that file in your editor (Settings › Appearance)',
        },
      ],
    },
    {
      title: 'Projects',
      items: [
        { command: 'new-project', label: 'New project' },
        { command: 'delete-project', label: 'Delete active project' },
        {
          merged: [
            { command: 'prev-project', label: 'Previous project' },
            { command: 'next-project', label: 'Next project' },
          ],
          keys: `${m('[')} / ${m(']')}`,
          label: 'Previous / next project',
        },
        { command: 'worktrees', label: 'Worktrees in the active project' },
        { command: 'quick-idea', label: 'Capture an idea' },
        { command: 'idea-inbox', label: 'Ideas in the active project' },
      ],
    },
    {
      title: 'View',
      items: [
        { command: 'toggle-project-grid', label: 'Toggle project grid' },
        { command: 'toggle-all-grid', label: 'Toggle all-sessions grid' },
        {
          command: 'focus-active-session',
          label: 'Grid: focus the active session (single view)',
        },
        { command: 'toggle-sidebar', label: 'Toggle sidebar' },
        {
          command: 'toggle-activity',
          label: 'Agent activity: panel (single view) / activity grid (grid)',
        },
        { command: 'activity-grid', label: 'Agent activity grid' },
        {
          command: 'find-in-session',
          label: 'Find in session (transcript on full-screen agents)',
        },
        {
          merged: [
            { command: 'zoom-in', label: 'Zoom in' },
            { command: 'zoom-out', label: 'Zoom out' },
            { command: 'zoom-reset', label: 'Reset zoom' },
          ],
          keys: `${m('=')} / ${m('-')} / ${m('0')}`,
          label: 'Zoom in / out / reset',
        },
        { command: 'command-palette', label: 'Command palette' },
        { command: 'settings', label: 'Settings (custom agents)' },
        {
          merged: [
            {
              command: 'keyboard-shortcuts',
              label: 'Keyboard shortcuts (this panel)',
            },
          ],
          keys: `${m('?')} or ${m('/')}`,
          label: 'Keyboard shortcuts (this panel)',
        },
      ],
    },
    {
      title: 'Window',
      items: [
        { command: 'new-window', label: 'New window' },
        { command: 'close-window', label: 'Close window' },
        {
          command: 'open-os-terminal',
          label: 'Open OS terminal at session directory',
        },
      ],
    },
    {
      title: 'Inside a terminal',
      items: [
        {
          keys: c('C', { shift: true }),
          label: 'Copy selection (works under mouse-tracking TUIs)',
        },
        { keys: c('V', { shift: true }), label: 'Paste' },
        { keys: c('A', { shift: true }), label: 'Select all' },
        {
          keys: `⇧${keyLabel('enter', isMac)}`,
          label: 'Insert newline in agent input (instead of submitting)',
        },
        {
          keys: keyLabel('delete', isMac),
          label: 'Delete the character after the cursor',
        },
        // Text editing, split by platform because the chords genuinely
        // differ. On macOS word-wise movement is ⌥←/→ (⌃←/→ is Mission
        // Control's space switcher and never reaches the app); off mac
        // it is Ctrl+←/→. Everything here is either written by the app
        // (lib/keymap.ts) or encoded by xterm. See AGENTS.md ›
        // Keybindings Policy.
        ...(isMac
          ? [
              { keys: '⌥⌫', label: 'Delete the word before the cursor' },
              { keys: '⌥⌦', label: 'Delete the word after the cursor' },
              { keys: '⌘⌫', label: 'Delete to start of line' },
              { keys: '⌘⌦', label: 'Delete to end of line' },
              { keys: `⌥${hArrows}`, label: 'Move by word' },
            ]
          : [{ keys: c(hArrows), label: 'Move by word' }]),
      ],
    },
    {
      // The card over a tile whose process has exited. Bare keys: the
      // terminal underneath has nothing left to type into.
      title: 'Ended session',
      items: [
        { keys: keyLabel('enter', isMac), label: 'Close the session' },
        { keys: 'R', label: 'Restart it in place' },
        { keys: 'Esc', label: 'Dismiss the card, keep the tile' },
      ],
    },
    {
      title: 'Launcher & dialogs',
      items: [
        { keys: '1–9', label: 'Pick agent by number' },
        {
          keys: arrowSeq(isMac, 'up', 'down'),
          label: 'Navigate items',
        },
        { keys: 'Tab', label: 'Next item (palette) · next field (launcher)' },
        { keys: keyLabel('enter', isMac), label: 'Confirm' },
        { keys: 'Esc', label: 'Dismiss / cancel' },
        {
          keys: arrowSeq(isMac, 'left', 'right'),
          label: 'Resize sidebar (when resizer focused; ⇧ = larger steps)',
        },
      ],
    },
  ];
  return groups;
}

// Shortcut labels for the command palette, by command id: every command
// with a shortcut under the keymap. A command with none is absent.
export function paletteShortcuts({
  isMac,
  keymap = EMPTY_KEYMAP,
}: Opts): Record<string, string> {
  const map: Record<string, string> = {};
  for (const b of effectiveFor(keymap, isMac).bindings) {
    if (b.command !== null && !(b.command in map)) {
      map[b.command] = labelIn(keymap, b.command, isMac);
    }
  }
  return map;
}
