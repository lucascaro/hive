// Single source of truth for keyboard shortcuts. The help overlay
// (⌘/) renders shortcutGroups(); the command palette pulls its
// shortcut column from paletteShortcuts() — both consume this module,
// so the two surfaces cannot drift from each other. (They can still
// drift from the actual handlers in main.tsx/menu.go, which is why
// every binding change must touch this file too — see AGENTS.md.)
//
// The full drift surface for a GUI binding change is five files:
//   1. the handler — app/keyboard.ts (+ lib/keymap.ts for a predicate)
//   2. this file — shortcutGroups() AND paletteShortcuts()
//   3. the palette command table — main.tsx
//   4. the native macOS menu — cmd/hivegui/menu_darwin.go (⌘ chords only;
//      Ctrl-only chords are deliberately JS-side, see the Ctrl+` comment
//      in app/keyboard.ts)
//   5. the user-facing shortcut table in README.md
//
// Pure module: no DOM, unit-testable.

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
function mod(
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

// Session back/forward is the one binding that is not simply
// "cmd on mac, ctrl elsewhere" or "ctrl everywhere": it is Ctrl on
// macOS but Ctrl+Alt on Windows/Linux, because plain Ctrl+- is
// already zoom-out there. See lib/keymap.ts navHistoryKey.
function ctrlAlt(
  isMac: boolean,
  key: string,
  { shift = false }: ModOpts = {},
): string {
  const k = keyLabel(key, isMac);
  if (isMac) return (shift ? '⌃⇧' : '⌃') + k;
  return (shift ? 'Ctrl+Alt+Shift+' : 'Ctrl+Alt+') + k;
}

// Arrow-key sequences: mac glyphs read fine run together (↑↓←→);
// word labels need separators so non-mac renders "Up/Down/Left/Right"
// instead of the unreadable "UpDownLeftRight".
function arrowSeq(isMac: boolean, ...keys: string[]): string {
  return keys.map((k) => keyLabel(k, isMac)).join(isMac ? '' : '/');
}

export function shortcutGroups({ isMac }: { isMac: boolean }): ShortcutGroup[] {
  const m = (key: string, opts?: ModOpts) => mod(isMac, key, opts);
  const c = (key: string, opts?: ModOpts) => ctrl(isMac, key, opts);
  const ca = (key: string, opts?: ModOpts) => ctrlAlt(isMac, key, opts);
  const vArrows = arrowSeq(isMac, 'up', 'down');
  const hArrows = arrowSeq(isMac, 'left', 'right');
  return [
    {
      title: 'Sessions',
      items: [
        { keys: m('T'), label: 'New session' },
        { keys: m('T', { shift: true }), label: 'New session in git worktree' },
        { keys: m('P'), label: 'Duplicate session' },
        {
          keys: m('P', { shift: true }),
          label: 'Duplicate session (choose tool)',
        },
        { keys: m('W'), label: 'Close session' },
        { keys: m('Z'), label: 'Reopen closed session' },
        { keys: `${m('1')}–${m('9')}`, label: 'Switch to session 1–9' },
        {
          keys: `${isMac ? '⌘' : 'Ctrl+'}${vArrows}`,
          label: 'Next / previous session (grid: move between tiles)',
        },
        {
          keys: `${isMac ? '⌘' : 'Ctrl+'}${hArrows}`,
          label:
            'Grid: move between tiles — in focused mode these reach the terminal (start / end of line)',
        },
        {
          keys: `${isMac ? '⇧⌘' : 'Ctrl+Shift+'}${vArrows}`,
          label: 'Reorder session within its project (wraps)',
        },
        { keys: ca('-'), label: 'Go back to the previously visited session' },
        { keys: ca('-', { shift: true }), label: 'Go forward again' },
        { keys: m('B'), label: 'Next session needing attention (bell)' },
        { keys: m('B', { shift: true }), label: 'Jump back to where you were' },
        { keys: 'Double-click', label: 'Rename (sidebar row or tile title)' },
      ],
    },
    {
      title: 'Projects',
      items: [
        { keys: m('N'), label: 'New project' },
        {
          keys: m('backspace', { shift: true }),
          label: 'Delete active project',
        },
        { keys: `${m('[')} / ${m(']')}`, label: 'Previous / next project' },
        { keys: m('E'), label: 'Worktrees in the active project' },
      ],
    },
    {
      title: 'View',
      items: [
        { keys: m('G'), label: 'Toggle project grid' },
        { keys: m('G', { shift: true }), label: 'Toggle all-sessions grid' },
        {
          keys: m('enter'),
          label: 'Grid: focus the active session (single view)',
        },
        { keys: m('S'), label: 'Toggle sidebar' },
        {
          keys: `${m('=')} / ${m('-')} / ${m('0')}`,
          label: 'Zoom in / out / reset',
        },
        { keys: m('K', { shift: true }), label: 'Command palette' },
        { keys: m(','), label: 'Settings (custom agents)' },
        {
          keys: `${m('?')} or ${m('/')}`,
          label: 'Keyboard shortcuts (this panel)',
        },
      ],
    },
    {
      title: 'Window',
      items: [
        { keys: m('N', { shift: true }), label: 'New window' },
        { keys: m('W', { shift: true }), label: 'Close window' },
        { keys: c('`'), label: 'Open OS terminal at session directory' },
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
        ...(isMac ? [{ keys: '⌘⌫', label: 'Clear input line' }] : []),
      ],
    },
    {
      title: 'Launcher & dialogs',
      items: [
        { keys: '1–9', label: 'Pick agent by number' },
        {
          keys: `${arrowSeq(isMac, 'up', 'down')} / Tab`,
          label: 'Navigate items',
        },
        { keys: keyLabel('enter', isMac), label: 'Confirm' },
        { keys: 'Esc', label: 'Dismiss / cancel' },
        {
          keys: arrowSeq(isMac, 'left', 'right'),
          label: 'Resize sidebar (when resizer focused; ⇧ = larger steps)',
        },
      ],
    },
  ];
}

// Shortcut strings for the command palette, by command id. On mac
// these match the glyph style the palette has always used.
export function paletteShortcuts({
  isMac,
}: {
  isMac: boolean;
}): Record<string, string> {
  const m = (key: string, opts?: ModOpts) => mod(isMac, key, opts);
  const c = (key: string, opts?: ModOpts) => ctrl(isMac, key, opts);
  const ca = (key: string, opts?: ModOpts) => ctrlAlt(isMac, key, opts);
  const map: Record<string, string> = {
    'new-project': m('N'),
    'new-session': m('T'),
    'new-session-worktree': m('T', { shift: true }),
    'duplicate-session': m('P'),
    'duplicate-session-choose-tool': m('P', { shift: true }),
    'restart-session': '',
    'delete-project': m('backspace', { shift: true }),
    worktrees: m('E'),
    'close-session': m('W'),
    'new-window': m('N', { shift: true }),
    'open-os-terminal': c('`'),
    'close-window': m('W', { shift: true }),
    'toggle-sidebar': m('S'),
    'toggle-project-grid': m('G'),
    'toggle-all-grid': m('G', { shift: true }),
    'zoom-in': m('='),
    'zoom-out': m('-'),
    'zoom-reset': m('0'),
    'next-session': m('down'),
    'prev-session': m('up'),
    'move-forward': m('down', { shift: true }),
    'move-backward': m('up', { shift: true }),
    'nav-back': ca('-'),
    'nav-forward': ca('-', { shift: true }),
    'next-attention': m('B'),
    'jump-back': m('B', { shift: true }),
    'next-project': m(']'),
    'prev-project': m('['),
    // ⌘/ is what the macOS menu item displays, so the palette matches it.
    // ⌘? also works (see menu_darwin.go); the overlay lists both.
    'keyboard-shortcuts': m('/'),
    settings: m(','),
  };
  for (let i = 1; i <= 9; i++) map[`switch-${i}`] = m(String(i));
  return map;
}
