// ---------- keyboard scopes: who owns a key ----------
//
// Every key binding in the app, grouped by the scope that owns it, and
// KEY_SCOPES — the ONE ordered list that decides which scope sees a key
// first (spec 478). app/keyboard.ts walks this list; nothing else decides
// precedence. The order encodes shipped bug fixes (a question about
// deleting a worktree must beat the worktree browser underneath it, a
// rename must beat the browser it is in, …), so move a scope only with a
// test that shows why. test/e2e/every-shortcut.spec.ts presses every chord
// here in the running app; a new scope needs a fixture there.
//
// A binding names a command by id (app/commands.ts registers them); this
// module never imports the commands themselves, which keeps it free of
// the action graph. Bindings are chord strings (lib/chord.ts): exact by
// default, with `Shift?` / `Any+` wherever a key deliberately ignores a
// modifier.
//
// Modal precedence reads the store, never DOM classes.

import {
  chordMatches,
  chordsFor,
  parseChord,
  type Chord,
  type KeyEventLike,
  type Keys,
} from '../lib/chord.js';
import { PHASE, phaseOf } from '../lib/phase-steps.js';
import { pluginChord } from '../lib/plugin-api.js';
import { isMac as platformIsMac } from '../lib/platform.js';
import { appStore, isModalOpen, type ModalId } from '../store/store.js';
import { termsMap } from '../store/terms.js';
import { findBoxActive } from './find-session.js';
import { inlineRenameActive } from './inline-rename.js';
import { choiceDialogOpen } from './modals/choice-dialog.js';
import { pluginCommands } from './plugin-host.js';

export interface Binding {
  keys: Keys;
  /** Command id, or null to reserve the chord: it ends dispatch but is
   * left to the terminal, so nothing further down (a plugin) can take it. */
  command: string | null;
  /** false: a held key does not repeat the command. */
  repeat?: false;
}

/**
 * How a scope treats a key once it is active:
 *  - exclusive   owns the keyboard: a binding runs, and any other key is
 *                stopped here (Tab is kept inside `trap`).
 *  - matched     only a binding stops dispatch; other keys go on down.
 *  - text-input  an input owns plain typing: every key without the
 *                platform modifier (and always Escape / Enter) is left to
 *                it and stops dispatch; ⌘/Ctrl chords go on down.
 */
export type Ownership = 'exclusive' | 'matched' | 'text-input';

export interface KeyScope {
  id: string;
  active: () => boolean;
  owns: Ownership;
  /** pageEl id whose focus Tab is kept inside (exclusive scopes only). */
  trap?: string;
  bindings: () => readonly Binding[];
}

const appData = () => appStore.getState();

const ESCAPE: Keys = 'Any+Escape';

function modal(
  id: string,
  modalId: ModalId,
  bindings: readonly Binding[],
  trap?: string,
): KeyScope {
  return {
    id,
    active: () => isModalOpen(modalId),
    owns: 'exclusive',
    trap,
    bindings: () => bindings,
  };
}

// ⌘/ and ⌘? both mean the shortcuts panel: '?' is Shift+/ on a US layout.
// The '?' form only ever fires off macOS — there the Help menu's ⌘/
// accelerator takes both before the webview (menu_darwin.go).
const HELP_CHORD: Keys = ['Mod+Shift?+/', 'Mod+Shift?+?'];

// ---------- the scopes, in precedence order ----------

const findBox: KeyScope = {
  // The find box owns plain typing while its input has focus. This gate,
  // not the input's own listener, is the mechanism: the window listener
  // is capture-phase, so a stopPropagation() on the input could not stop
  // it. The box handles Escape and Enter itself; ⌘A/⌘C/⌘V fall through
  // as ordinary text editing.
  id: 'find-box',
  active: () => findBoxActive(),
  owns: 'text-input',
  bindings: () => [],
};

const inlineRename: KeyScope = {
  // An inline rename owns the keyboard while it is open: Escape cancels
  // the edit, and every other key is text the user is typing. Here rather
  // than in the input's own listener because this runs in the capture
  // phase — the input's stopPropagation cannot win. Without it, Escape in
  // a rename inside the worktree browser closed the whole panel and
  // silently discarded the edit.
  id: 'inline-rename',
  active: () => inlineRenameActive(),
  owns: 'exclusive',
  bindings: () => [{ keys: ESCAPE, command: 'inline-rename.cancel' }],
};

const choiceDialog: KeyScope = {
  // The topmost thing on screen, asking a question that may destroy work.
  // Its root sits over whichever modal asked. The trap is here, not in
  // the dialog's own listener: a dialog opened over a terminal starts
  // with focus elsewhere, so the first Tab would walk into the page.
  id: 'choice-dialog',
  active: () => choiceDialogOpen(),
  owns: 'exclusive',
  trap: 'choice-dialog',
  bindings: () => [{ keys: ESCAPE, command: 'choice-dialog.dismiss' }],
};

// The launcher's own listener handles every key.
const launcher = modal('launcher', 'launcher', []);

// The editor's own listener handles Enter; Escape and the backdrop are
// ModalShell's. Only Tab containment lives here, because a dialog opened
// over a terminal starts with focus outside it.
const projectEditor = modal(
  'project-editor',
  'project-editor',
  [],
  'project-editor',
);

// The palette's own listener owns filtering, selection and Enter. Escape
// is here because that listener only hears keys typed INSIDE the palette:
// anything that moves focus out (the focus pipeline's retry) would leave
// it with no way to close and every key swallowed.
const commandPalette = modal('command-palette', 'command-palette', [
  { keys: ESCAPE, command: 'command-palette.close' },
]);

// A form: Tab walks its inputs inside the trap. The modal's own listener
// also handles Escape; this is the fallback for focus still on the
// terminal, plus the ⌘, toggle-to-close.
const settings = modal(
  'settings',
  'settings',
  [
    { keys: ESCAPE, command: 'settings.close' },
    { keys: 'Mod+Shift?+,', command: 'settings.close' },
  ],
  'settings',
);

const worktrees = modal(
  'worktrees',
  'worktrees',
  [
    { keys: ESCAPE, command: 'worktrees.close' },
    { keys: 'Mod+E', command: 'worktrees.close' },
  ],
  'worktrees',
);

// ⌘I toggles the capture sheet closed; ⇧⌘I goes to the inbox, closing
// the sheet on the way. On macOS the native accelerators reach the same
// commands through the menu, so the chord behaves the same everywhere.
const quickIdea = modal(
  'quick-idea',
  'quick-idea',
  [
    { keys: ESCAPE, command: 'quick-idea.close' },
    { keys: 'Mod+I', command: 'quick-idea.close' },
    { keys: 'Mod+Shift+I', command: 'idea-inbox' },
  ],
  'quick-idea',
);

const ideaInbox = modal(
  'idea-inbox',
  'idea-inbox',
  [
    { keys: ESCAPE, command: 'idea-inbox.close' },
    { keys: 'Mod+Shift+I', command: 'idea-inbox.close' },
    { keys: 'Mod+I', command: 'quick-idea' },
  ],
  'idea-inbox',
);

const helpOverlay = modal(
  'help-overlay',
  'help',
  [
    { keys: ESCAPE, command: 'help-overlay.close' },
    { keys: HELP_CHORD, command: 'help-overlay.close' },
  ],
  'help-overlay',
);

// ⌘/ is the binding this modal shows next to its shortcuts row, so the
// key hands off exactly as the row does rather than being swallowed.
const helpModal = modal(
  'help-modal',
  'help-modal',
  [
    { keys: ESCAPE, command: 'help-modal.close' },
    { keys: HELP_CHORD, command: 'help-modal.shortcuts' },
  ],
  'help-modal',
);

const whatsNew = modal(
  'whats-new',
  'whats-new',
  [{ keys: ESCAPE, command: 'whats-new.close' }],
  'whats-new',
);

// A UI plugin's session view (spec 471): Escape dismisses it, the
// plugin's own controls answer anything else.
const pluginView = modal(
  'plugin-view',
  'plugin-view',
  [{ keys: ESCAPE, command: 'plugin-view.close' }],
  'plugin-view',
);

const buildLog = modal(
  'build-log',
  'build-log',
  [{ keys: ESCAPE, command: 'build-log.close' }],
  'build-log',
);

const blockedTile: KeyScope = {
  // Parked on a worktree decision: Enter re-raises the question for the
  // focused tile. Its "Answer…" button is otherwise mouse-only — Escape
  // dismisses without answering, and xterm's textarea swallows Tab.
  // Before the dead overlay: a session is blocked OR dead, never both.
  id: 'blocked-tile',
  active: () => {
    const id = appData().activeId;
    const s = id ? appData().sessions.find((x) => x.id === id) : undefined;
    return !!s && phaseOf(s) === PHASE.blocked;
  },
  owns: 'matched',
  bindings: () => [
    { keys: 'Shift?+Enter', command: 'session.answer-worktree-question' },
  ],
};

const deadOverlay: KeyScope = {
  // The active session's dead-session overlay. In a grid the user can
  // still click any tile's buttons; this handles the focused one.
  id: 'dead-overlay',
  active: () => {
    const id = appData().activeId;
    return !!id && !!termsMap().get(id)?.deadOverlayShown;
  },
  owns: 'matched',
  bindings: () => [
    { keys: 'Any+Enter', command: 'dead-session.close' },
    { keys: ESCAPE, command: 'dead-session.dismiss' },
    // Bare r only (⌘R keeps whatever it does), and a held key must not
    // queue restarts.
    { keys: 'Shift?+R', command: 'dead-session.restart', repeat: false },
  ],
};

// The app's global chords. Exact matches, except where a key has always
// ignored Shift ('+' is Shift+= on a US layout; digits need Shift on
// AZERTY). Order only matters between chords that overlap, and none do.
const APP_BINDINGS: readonly Binding[] = [
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
  // readline's forward-char. No macOS keydown: the native ⌘F accelerator
  // takes the key and the menu event runs the same command.
  {
    keys: { other: ['Ctrl+Shift+F', 'Ctrl+Shift+[KeyF]'] },
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
  { keys: 'Mod+Shift+ArrowUp', command: 'arrow-shift-up' },
  { keys: 'Mod+Shift+ArrowDown', command: 'arrow-shift-down' },
  { keys: 'Mod+Shift?+[', command: 'prev-project' },
  { keys: 'Mod+Shift?+]', command: 'next-project' },
];

const app: KeyScope = {
  id: 'app',
  active: () => true,
  owns: 'matched',
  bindings: () => APP_BINDINGS,
};

const plugins: KeyScope = {
  // Last on purpose: every core binding above has already had the key,
  // so a plugin can never take one (spec 471).
  id: 'plugins',
  active: () => Object.keys(appData().pluginUI).length > 0,
  owns: 'matched',
  bindings: () =>
    pluginCommands().flatMap((r) => {
      const keys =
        r.bound && r.command.keys ? pluginChord(r.command.keys) : null;
      return keys
        ? [{ keys, command: `plugin:${r.pluginId}:${r.command.id}` }]
        : [];
    }),
};

export const KEY_SCOPES: readonly KeyScope[] = [
  findBox,
  inlineRename,
  choiceDialog,
  launcher,
  projectEditor,
  commandPalette,
  settings,
  worktrees,
  quickIdea,
  ideaInbox,
  helpOverlay,
  helpModal,
  whatsNew,
  pluginView,
  buildLog,
  blockedTile,
  deadOverlay,
  app,
  plugins,
];

// ---------- matching ----------

const parsed = new Map<string, Chord>();
function chord(s: string, isMac: boolean): Chord {
  const k = `${isMac ? 'm' : 'o'}:${s}`;
  let c = parsed.get(k);
  if (!c) {
    c = parseChord(s, isMac);
    parsed.set(k, c);
  }
  return c;
}

/** The first of the scope's bindings this key matches, if any. */
export function matchBinding(
  scope: KeyScope,
  e: KeyEventLike & { repeat?: boolean },
  isMac: boolean = platformIsMac,
): Binding | undefined {
  return scope
    .bindings()
    .find(
      (b) =>
        !(b.repeat === false && e.repeat) &&
        chordsFor(b.keys, isMac).some((s) => chordMatches(chord(s, isMac), e)),
    );
}

export function scopeById(id: string): KeyScope {
  const s = KEY_SCOPES.find((x) => x.id === id);
  if (!s) throw new Error(`no key scope "${id}"`);
  return s;
}

// ---------- queries other modules need ----------

/** The capture sheet and the inbox: ⌘I / ⇧⌘I toggle between them, so
 * neither blocks the other's chord. */
export const IDEA_SCOPES: ReadonlySet<string> = new Set([
  'quick-idea',
  'idea-inbox',
]);

/** Whether a scope that owns the whole keyboard is open, other than the
 * ones named. The menu path uses it to refuse what the keyboard would
 * never have reached. */
export function exclusiveScopeOpen(except: ReadonlySet<string>): boolean {
  return KEY_SCOPES.some(
    (s) => s.owns === 'exclusive' && !except.has(s.id) && s.active(),
  );
}
