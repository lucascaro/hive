// ---------- core commands ----------
//
// Every core action the app exposes, by id: the catalog the key bindings
// (app/key-scopes.ts), the native menu (MENU_COMMANDS below) and the
// command palette all run through the command bus (app/command-registry.ts).
// One id per action, so a key, its menu item and its palette row can no
// longer drift into three different behaviours (spec 478).
//
// Core commands are catalogued HERE rather than self-registered by the
// feature modules: a test that vi.mock()s a feature module would silently
// drop a self-registration, and with it every binding that names it.
//
// Every `run` is an arrow wrapper, never a bare function reference: the
// imported binding is read when the command runs, not when this module
// evaluates.
//
// Importing this module registers the catalog and subscribes the menu
// events — keyboard.ts imports it for exactly that.

import {
  CloseWindow,
  EventsOn,
  OpenNewWindow,
  OpenTerminalAt,
} from '../bridge.js';
import { appStore } from '../store/store.js';
import { termsMap } from '../store/terms.js';
import {
  bumpFont,
  captureIdea,
  copyScrollTrace,
  deleteActiveProject,
  focusActiveSession,
  handleArrow,
  jumpBack,
  jumpToAttention,
  navBack,
  navForward,
  navSession,
  openWorktreesForActiveProject,
  reorderActive,
  resetFont,
  showActivityGrid,
  switchToNthSession,
  toggleActivity,
  toggleAllGrid,
  toggleIdeaInbox,
  toggleProjectGrid,
  toggleScrollDebug,
  toggleSidebar,
} from './actions.js';
import { manualUpdateCheck, reloadGui, restartHive } from './banners.js';
import {
  registerCommandSource,
  runCommand,
  type Command,
} from './command-registry.js';
import { reportFailure } from './dom.js';
import { raiseWorktreeChoice } from './events.js';
import { openFindInSession } from './find-session.js';
import { cancelInlineRename } from './inline-rename.js';
import { closeBuildLog } from './modals/build-log.js';
import { dismissChoiceDialog } from './modals/choice-dialog.js';
import {
  closeCommandPalette,
  openCommandPalette,
} from './modals/command-palette.js';
import { closeHelp, handOffToShortcuts, openHelp } from './modals/help.js';
import { closeHelpOverlay, toggleHelpOverlay } from './modals/help-overlay.js';
import { closeIdeaInbox } from './modals/idea-inbox.js';
import {
  duplicateActiveSession,
  duplicateActiveSessionChooseTool,
  openLauncher,
  restartActiveSession,
} from './modals/launcher.js';
import { openProjectEditor } from './modals/project-editor.js';
import { closeQuickIdea } from './modals/quick-idea.js';
import { dismissSettings, openSettings } from './modals/settings.js';
import { openWhatsNew, closeWhatsNew } from './modals/whats-new.js';
import { closeWorktrees } from './modals/worktrees.js';
import { closeSessionView } from './plugin-host.js';
import { orderedSessions, activeCwd } from './selectors.js';
import { isModalOpen } from '../store/store.js';
import { closeActiveSession, reopenLastClosedSession } from './undo-close.js';
import { shiftActiveProject } from './view.js';

const appData = () => appStore.getState();

// The active session's terminal, for the dead-session overlay commands.
const activeTerm = () => {
  const id = appData().activeId;
  return id ? termsMap().get(id) : undefined;
};

// The palette rows, in the order the palette lists them. A title makes a
// command a palette row; its shortcut label comes from lib/shortcuts.ts
// by the same id.
const PALETTE: readonly Command[] = [
  {
    id: 'new-project',
    title: 'New Project…',
    run: () => openProjectEditor(null),
  },
  { id: 'new-session', title: 'New Session', run: () => openLauncher() },
  {
    id: 'new-session-worktree',
    title: 'New Session in Worktree',
    run: () => openLauncher(undefined, { forceWorktree: true }),
  },
  {
    id: 'duplicate-session',
    title: 'Duplicate Session',
    run: () => duplicateActiveSession(),
  },
  {
    id: 'duplicate-session-choose-tool',
    title: 'Duplicate Session (choose tool)…',
    run: () => duplicateActiveSessionChooseTool(),
  },
  {
    id: 'restart-session',
    title: 'Restart Session',
    run: () => restartActiveSession(),
  },
  {
    id: 'delete-project',
    title: 'Delete Active Project…',
    run: () => deleteActiveProject(),
  },
  {
    id: 'worktrees',
    title: 'Worktrees…',
    run: () => openWorktreesForActiveProject(),
  },
  { id: 'whats-new', title: "What's New…", run: () => openWhatsNew() },
  { id: 'help', title: 'Help…', run: () => openHelp() },
  // ⌘I / ⇧⌘I. Toggles, and gated by the open scopes: on macOS the native
  // accelerators take the chord before the webview, so the menu path is
  // the only one that runs there, and it must behave like the keydown
  // path does elsewhere (see captureIdea in actions.ts).
  { id: 'quick-idea', title: 'Capture Idea…', run: () => captureIdea() },
  { id: 'idea-inbox', title: 'Ideas…', run: () => toggleIdeaInbox() },
  {
    id: 'close-session',
    title: 'Close Session',
    // Never declines: ⌘W with no session is consumed and does nothing.
    run: () => closeActiveSession(),
  },
  {
    id: 'reopen-closed-session',
    title: 'Reopen Closed Session',
    run: () => reopenLastClosedSession(),
  },
  {
    id: 'new-window',
    title: 'New Window',
    run: () => {
      OpenNewWindow().catch(reportFailure('new window'));
    },
  },
  {
    id: 'open-os-terminal',
    title: 'Open OS Terminal Here',
    run: () => {
      OpenTerminalAt(activeCwd()).catch(reportFailure('open terminal'));
    },
  },
  {
    id: 'close-window',
    title: 'Close Window',
    run: () => {
      CloseWindow().catch(reportFailure('close window'));
    },
  },
  { id: 'toggle-sidebar', title: 'Toggle Sidebar', run: () => toggleSidebar() },
  {
    id: 'toggle-project-grid',
    title: 'Toggle Project Grid',
    run: () => toggleProjectGrid(),
  },
  {
    id: 'toggle-all-grid',
    title: 'Toggle All Sessions Grid',
    run: () => toggleAllGrid(),
  },
  {
    id: 'toggle-activity',
    title: 'Toggle Agent Activity',
    run: () => toggleActivity(),
  },
  {
    id: 'activity-grid',
    title: 'Agent Activity Grid',
    run: () => showActivityGrid(),
  },
  {
    id: 'find-in-session',
    title: 'Find in Session',
    run: () => openFindInSession(),
  },
  {
    id: 'focus-active-session',
    title: 'Focus Active Session',
    // Declines in single view — there is nothing to zoom into, and ⌘⏎
    // must then reach the agent, which binds it itself.
    run: () => (appData().view === 'single' ? false : focusActiveSession()),
  },
  { id: 'zoom-in', title: 'Zoom In', run: () => bumpFont(+1) },
  { id: 'zoom-out', title: 'Zoom Out', run: () => bumpFont(-1) },
  { id: 'zoom-reset', title: 'Actual Size', run: () => resetFont() },
  { id: 'next-session', title: 'Next Session', run: () => navSession(+1) },
  { id: 'prev-session', title: 'Previous Session', run: () => navSession(-1) },
  { id: 'nav-back', title: 'Go Back', run: () => navBack() },
  { id: 'nav-forward', title: 'Go Forward', run: () => navForward() },
  {
    id: 'next-attention',
    title: 'Next Session Needing Attention',
    run: () => jumpToAttention(),
  },
  {
    id: 'jump-back',
    title: 'Jump Back to Where You Were',
    run: () => jumpBack(),
  },
  {
    id: 'move-forward',
    title: 'Move Session Forward',
    run: () => reorderActive(+1),
  },
  {
    id: 'move-backward',
    title: 'Move Session Backward',
    run: () => reorderActive(-1),
  },
  {
    id: 'next-project',
    title: 'Next Project',
    run: () => shiftActiveProject(+1),
  },
  {
    id: 'prev-project',
    title: 'Previous Project',
    run: () => shiftActiveProject(-1),
  },
  {
    id: 'keyboard-shortcuts',
    title: 'Keyboard Shortcuts',
    // Toggles, and hands off from an open Help modal instead of stacking
    // a second aria-modal dialog on it. The menu needs this on macOS,
    // where the native ⌘/ accelerator means the keydown close path never
    // runs; the palette and the key share it so the three agree.
    run: () => {
      if (isModalOpen('help-modal')) handOffToShortcuts();
      else toggleHelpOverlay();
    },
  },
  { id: 'settings', title: 'Settings…', run: () => openSettings() },
  { id: 'reload-gui', title: 'Reload GUI', run: () => reloadGui() },
  {
    id: 'restart-hive',
    title: 'Restart Daemon… (ends all sessions)',
    run: () => restartHive(),
  },
  ...Array.from({ length: 9 }, (_, i) => ({
    id: `switch-${i + 1}`,
    title: `Switch to Session ${i + 1}`,
    // Declines past the last session, leaving ⌘9 to the terminal.
    run: () => {
      if (i >= orderedSessions().length) return false;
      switchToNthSession(i + 1);
    },
  })),
];

// Reachable from a key or the menu only.
const UNLISTED: readonly Command[] = [
  { id: 'command-palette', run: () => openCommandPalette() },
  { id: 'check-for-updates', run: () => manualUpdateCheck() },
  { id: 'toggle-scroll-debug', run: () => toggleScrollDebug() },
  { id: 'copy-scroll-trace', run: () => copyScrollTrace() },
  // The horizontal arrows. They decline in focused mode (handleArrow
  // returns false): ⌘←/⌘→ are start/end-of-line in the terminal. (⇧⌘↑/↓
  // run move-forward/backward above, which reorder in every view.)
  { id: 'grid-left', run: () => handleArrow(-1, 0, false) },
  { id: 'grid-right', run: () => handleArrow(+1, 0, false) },

  { id: 'inline-rename.cancel', run: () => cancelInlineRename() },
  { id: 'choice-dialog.dismiss', run: () => dismissChoiceDialog() },
  { id: 'command-palette.close', run: () => closeCommandPalette() },
  { id: 'settings.close', run: () => dismissSettings() },
  { id: 'worktrees.close', run: () => closeWorktrees() },
  { id: 'quick-idea.close', run: () => closeQuickIdea() },
  { id: 'idea-inbox.close', run: () => closeIdeaInbox() },
  { id: 'help-overlay.close', run: () => closeHelpOverlay() },
  { id: 'help-modal.close', run: () => closeHelp() },
  { id: 'help-modal.shortcuts', run: () => handOffToShortcuts() },
  { id: 'whats-new.close', run: () => closeWhatsNew() },
  { id: 'plugin-view.close', run: () => closeSessionView('dismissed') },
  { id: 'build-log.close', run: () => closeBuildLog() },

  {
    id: 'session.answer-worktree-question',
    run: () => {
      const id = appData().activeId;
      if (id) raiseWorktreeChoice(id);
    },
  },
  { id: 'dead-session.close', run: () => activeTerm()?._closeDead() },
  { id: 'dead-session.dismiss', run: () => activeTerm()?._dismissDead() },
  { id: 'dead-session.restart', run: () => activeTerm()?._restartDead() },
];

export const CORE_COMMANDS: readonly Command[] = [...PALETTE, ...UNLISTED];

registerCommandSource(() => CORE_COMMANDS, 'core');

// ---------- native menu ----------
//
// cmd/hivegui/menu_darwin.go emits `menu:<item>` events; each runs the
// command with the same meaning. On macOS the menu accelerators take ⌘
// chords before the webview sees them, so for those chords THIS is the
// path that runs — which is why a key and its menu item must share one
// command rather than two lookalike handlers.
export const MENU_COMMANDS: Readonly<Record<string, string>> = {
  'menu:new-session': 'new-session',
  'menu:new-session-worktree': 'new-session-worktree',
  'menu:duplicate-session': 'duplicate-session',
  'menu:duplicate-session-choose-tool': 'duplicate-session-choose-tool',
  'menu:restart-session': 'restart-session',
  'menu:new-project': 'new-project',
  'menu:delete-project': 'delete-project',
  'menu:command-palette': 'command-palette',
  'menu:settings': 'settings',
  'menu:worktrees': 'worktrees',
  'menu:quick-idea': 'quick-idea',
  'menu:idea-inbox': 'idea-inbox',
  'menu:close-session': 'close-session',
  'menu:reopen-closed-session': 'reopen-closed-session',
  'menu:zoom-in': 'zoom-in',
  'menu:zoom-out': 'zoom-out',
  'menu:zoom-reset': 'zoom-reset',
  'menu:toggle-sidebar': 'toggle-sidebar',
  'menu:toggle-project-grid': 'toggle-project-grid',
  'menu:toggle-all-grid': 'toggle-all-grid',
  'menu:toggle-activity': 'toggle-activity',
  'menu:find-in-session': 'find-in-session',
  'menu:activity-grid': 'activity-grid',
  'menu:next-session': 'next-session',
  'menu:prev-session': 'prev-session',
  'menu:move-session-forward': 'move-forward',
  'menu:move-session-backward': 'move-backward',
  'menu:next-attention': 'next-attention',
  'menu:jump-back': 'jump-back',
  'menu:next-project': 'next-project',
  'menu:prev-project': 'prev-project',
  'menu:check-for-updates': 'check-for-updates',
  'menu:reload-gui': 'reload-gui',
  'menu:restart-hive': 'restart-hive',
  'menu:keyboard-shortcuts': 'keyboard-shortcuts',
  'menu:toggle-scroll-debug': 'toggle-scroll-debug',
  'menu:copy-scroll-trace': 'copy-scroll-trace',
  ...Object.fromEntries(
    Array.from({ length: 9 }, (_, i) => [
      `menu:switch-${i + 1}`,
      `switch-${i + 1}`,
    ]),
  ),
};

for (const [event, id] of Object.entries(MENU_COMMANDS)) {
  EventsOn(event, () => {
    runCommand(id);
  });
}
