// ---------- keyboard scopes: who owns a key ----------
//
// Every key scope in the app with the bindings it owns, and KEY_SCOPES —
// the ONE ordered list that decides which scope sees a key first (spec
// 478). app/keyboard.ts walks this list; nothing else decides
// precedence. The order encodes shipped bug fixes (a question about
// deleting a worktree must beat the worktree browser underneath it, a
// rename must beat the browser it is in, …), so move a scope only with a
// test that shows why.
//
// A binding names a command by id (app/commands.ts registers them); this
// module never imports the commands themselves, which keeps it free of
// the action graph. Bindings are chord strings (lib/chord.ts): exact by
// default, with `Shift?` / `Any+` wherever a key deliberately ignores a
// modifier. The app's global chords are data in lib/bindings.ts, read
// through the user's keymap (spec 477) by app/bindings.ts; a modal's
// chord that mirrors its opener (⌘E closes the worktree browser) follows
// that keymap too.
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
import { isMac as platformIsMac } from '../lib/platform.js';
import { appStore, isModalOpen, type ModalId } from '../store/store.js';
import { termsMap } from '../store/terms.js';
import { findBoxActive } from './find-session.js';
import { inlineRenameActive } from './inline-rename.js';
import { choiceDialogOpen } from './modals/choice-dialog.js';
import { chordsOf, effectiveAppBindings, type Binding } from './bindings.js';
import { pluginCommands } from './plugin-host.js';

export type { Binding } from './bindings.js';

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
  bindings: readonly Binding[] | (() => readonly Binding[]),
  trap?: string,
): KeyScope {
  return {
    id,
    active: () => isModalOpen(modalId),
    owns: 'exclusive',
    trap,
    bindings: typeof bindings === 'function' ? bindings : () => bindings,
  };
}

// A modal's own chords that mirror the app command that opened it (⌘E
// opens and closes the worktree browser). They follow the user's keymap:
// rebinding the opener moves the closer with it.
const mirror = (opener: string, command: string): Binding => ({
  keys: chordsOf(opener),
  command,
});

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

// Settings › Shortcuts' elements that answer their own keys, marked
// data-own-keys (ShortcutsPanel.tsx): the capture button takes every key
// while it has focus — even ⌘, and Escape, which would otherwise close
// Settings — so the key the user presses is recorded, not run; in the
// import preview, Escape cancels the import rather than Settings. It
// binds nothing: exclusive and unmatched, the key goes on to the
// element's own handler. Tab still walks the Settings form.
const shortcutCapture: KeyScope = {
  id: 'shortcut-capture',
  active: () =>
    isModalOpen('settings') &&
    document.activeElement?.closest('#settings [data-own-keys]') != null,
  owns: 'exclusive',
  trap: 'settings',
  bindings: () => [],
};

// A form: Tab walks its inputs inside the trap. The modal's own listener
// also handles Escape; this is the fallback for focus still on the
// terminal, plus the ⌘, toggle-to-close.
const settings = modal(
  'settings',
  'settings',
  () => [
    { keys: ESCAPE, command: 'settings.close' },
    mirror('settings', 'settings.close'),
  ],
  'settings',
);

const worktrees = modal(
  'worktrees',
  'worktrees',
  () => [
    { keys: ESCAPE, command: 'worktrees.close' },
    mirror('worktrees', 'worktrees.close'),
  ],
  'worktrees',
);

// ⌘I toggles the capture sheet closed; ⇧⌘I goes to the inbox, closing
// the sheet on the way. On macOS the native accelerators reach the same
// commands through the menu, so the chord behaves the same everywhere.
const quickIdea = modal(
  'quick-idea',
  'quick-idea',
  () => [
    { keys: ESCAPE, command: 'quick-idea.close' },
    mirror('quick-idea', 'quick-idea.close'),
    mirror('idea-inbox', 'idea-inbox'),
  ],
  'quick-idea',
);

const ideaInbox = modal(
  'idea-inbox',
  'idea-inbox',
  () => [
    { keys: ESCAPE, command: 'idea-inbox.close' },
    mirror('idea-inbox', 'idea-inbox.close'),
    mirror('quick-idea', 'quick-idea'),
  ],
  'idea-inbox',
);

const helpOverlay = modal(
  'help-overlay',
  'help',
  () => [
    { keys: ESCAPE, command: 'help-overlay.close' },
    mirror('keyboard-shortcuts', 'help-overlay.close'),
  ],
  'help-overlay',
);

// ⌘/ is the binding this modal shows next to its shortcuts row, so the
// key hands off exactly as the row does rather than being swallowed.
const helpModal = modal(
  'help-modal',
  'help-modal',
  () => [
    { keys: ESCAPE, command: 'help-modal.close' },
    mirror('keyboard-shortcuts', 'help-modal.shortcuts'),
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

const app: KeyScope = {
  id: 'app',
  active: () => true,
  owns: 'matched',
  // The shipped chords under the user's keymap (app/bindings.ts).
  bindings: () => effectiveAppBindings().bindings,
};

const plugins: KeyScope = {
  // Last on purpose: every core binding above has already had the key,
  // so a plugin can never take one (spec 471).
  id: 'plugins',
  active: () => Object.keys(appData().pluginUI).length > 0,
  owns: 'matched',
  bindings: () =>
    pluginCommands().flatMap((r) =>
      r.chords.map((keys) => ({
        keys,
        command: `plugin:${r.pluginId}:${r.command.id}`,
      })),
    ),
};

export const KEY_SCOPES: readonly KeyScope[] = [
  findBox,
  inlineRename,
  choiceDialog,
  launcher,
  projectEditor,
  commandPalette,
  shortcutCapture,
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
