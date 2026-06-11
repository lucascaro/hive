// Single source of truth for keyboard shortcuts. The help overlay
// (⌘/) renders shortcutGroups(); the command palette pulls its
// shortcut column from paletteShortcuts() — both consume this module,
// so the two surfaces cannot drift from each other. (They can still
// drift from the actual handlers in main.js/menu.go, which is why
// every binding change must touch this file too — see AGENTS.md.)
//
// Pure module: no DOM, unit-testable.

// key: a printable key or a symbolic name from KEYS below.
const KEYS = {
  enter: { mac: '↩', other: 'Enter' },
  backspace: { mac: '⌫', other: 'Backspace' },
  up: { mac: '↑', other: 'Up' },
  down: { mac: '↓', other: 'Down' },
  left: { mac: '←', other: 'Left' },
  right: { mac: '→', other: 'Right' },
};

function keyLabel(key, isMac) {
  const k = KEYS[key];
  return k ? (isMac ? k.mac : k.other) : key;
}

// cmd ("⌘" / "Ctrl+"), optionally with shift ("⇧⌘" / "Ctrl+Shift+").
function mod(isMac, key, { shift = false } = {}) {
  const k = keyLabel(key, isMac);
  if (isMac) return (shift ? '⇧⌘' : '⌘') + k;
  return (shift ? 'Ctrl+Shift+' : 'Ctrl+') + k;
}

// Ctrl on every platform (Ctrl+`, Ctrl+Shift+C/V/A — deliberately not
// ⌘ on mac, see main.js comments).
function ctrl(isMac, key, { shift = false } = {}) {
  const k = keyLabel(key, isMac);
  if (isMac) return (shift ? '⌃⇧' : '⌃') + k;
  return (shift ? 'Ctrl+Shift+' : 'Ctrl+') + k;
}

// Arrow-key sequences: mac glyphs read fine run together (↑↓←→);
// word labels need separators so non-mac renders "Up/Down/Left/Right"
// instead of the unreadable "UpDownLeftRight".
function arrowSeq(isMac, ...keys) {
  return keys.map((k) => keyLabel(k, isMac)).join(isMac ? '' : '/');
}

export function shortcutGroups({ isMac }) {
  const m = (key, opts) => mod(isMac, key, opts);
  const c = (key, opts) => ctrl(isMac, key, opts);
  const arrows = arrowSeq(isMac, 'up', 'down', 'left', 'right');
  return [
    {
      title: 'Sessions',
      items: [
        { keys: m('T'), label: 'New session' },
        { keys: m('T', { shift: true }), label: 'New session in git worktree' },
        { keys: m('P'), label: 'Duplicate session' },
        { keys: m('P', { shift: true }), label: 'Duplicate session (choose tool)' },
        { keys: m('W'), label: 'Close session' },
        { keys: `${m('1')}–${m('9')}`, label: 'Switch to session 1–9' },
        { keys: `${isMac ? '⌘' : 'Ctrl+'}${arrows}`, label: 'Next / previous session (spatial move in grid)' },
        { keys: `${isMac ? '⇧⌘' : 'Ctrl+Shift+'}${arrows}`, label: 'Reorder session' },
        { keys: 'Double-click', label: 'Rename (sidebar row or tile title)' },
      ],
    },
    {
      title: 'Projects',
      items: [
        { keys: m('N'), label: 'New project' },
        { keys: m('backspace', { shift: true }), label: 'Delete active project' },
        { keys: `${m('[')} / ${m(']')}`, label: 'Previous / next project' },
      ],
    },
    {
      title: 'View',
      items: [
        { keys: m('G'), label: 'Toggle project grid' },
        { keys: m('G', { shift: true }), label: 'Toggle all-sessions grid' },
        { keys: m('enter'), label: 'Toggle grid ⇄ single' },
        { keys: m('S'), label: 'Toggle sidebar' },
        { keys: `${m('=')} / ${m('-')} / ${m('0')}`, label: 'Zoom in / out / reset' },
        { keys: m('K', { shift: true }), label: 'Command palette' },
        { keys: m('/'), label: 'Keyboard shortcuts (this panel)' },
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
        { keys: c('C', { shift: true }), label: 'Copy selection (works under mouse-tracking TUIs)' },
        { keys: c('V', { shift: true }), label: 'Paste' },
        { keys: c('A', { shift: true }), label: 'Select all' },
        { keys: `⇧${keyLabel('enter', isMac)}`, label: 'Insert newline in agent input (instead of submitting)' },
        ...(isMac ? [{ keys: '⌘⌫', label: 'Clear input line' }] : []),
      ],
    },
    {
      title: 'Launcher & dialogs',
      items: [
        { keys: '1–9', label: 'Pick agent by number' },
        { keys: `${arrowSeq(isMac, 'up', 'down')} / Tab`, label: 'Navigate items' },
        { keys: keyLabel('enter', isMac), label: 'Confirm' },
        { keys: 'Esc', label: 'Dismiss / cancel' },
        { keys: arrowSeq(isMac, 'left', 'right'), label: 'Resize sidebar (when resizer focused; ⇧ = larger steps)' },
      ],
    },
  ];
}

// Shortcut strings for the command palette, by command id. On mac
// these match the glyph style the palette has always used.
export function paletteShortcuts({ isMac }) {
  const m = (key, opts) => mod(isMac, key, opts);
  const c = (key, opts) => ctrl(isMac, key, opts);
  const map = {
    'new-project': m('N'),
    'new-session': m('T'),
    'new-session-worktree': m('T', { shift: true }),
    'duplicate-session': m('P'),
    'duplicate-session-choose-tool': m('P', { shift: true }),
    'restart-session': '',
    'delete-project': m('backspace', { shift: true }),
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
    'next-project': m(']'),
    'prev-project': m('['),
    'keyboard-shortcuts': m('/'),
  };
  for (let i = 1; i <= 9; i++) map[`switch-${i}`] = m(String(i));
  return map;
}

// Sidebar footer hint line. index.html carries the mac-glyph text as
// a static fallback; main.js re-renders it from here at boot so
// non-mac platforms see Ctrl+-style hints that match the bindings.
export function footerHints({ isMac }) {
  const m = (key, opts) => mod(isMac, key, opts);
  return [
    `${m('N')} project`,
    `${m('T')} session`,
    `${m('W')} close`,
    `${m('G')} grid`,
    `${m('K', { shift: true })} commands`,
    `${m('/')} help`,
  ].join(' · ');
}
