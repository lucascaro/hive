// Composition root. Every subsystem lives in src/app/* (pure logic in
// src/lib/*); this file builds the command table, injects the
// cross-module callbacks, and boots the control connection. Keep it
// free of behavior — if a function body wants to live here, it almost
// certainly belongs in a module.

import './theme/theme';
import '@xterm/xterm/css/xterm.css';

import {
  ConnectControl,
  OpenNewWindow,
  CloseWindow,
  OpenTerminalAt,
  LogFrontend,
} from './bridge.js';
import { classifyBeat, jsHeapMB } from './lib/freeze-heartbeat.js';
import { isMac } from './lib/platform.js';
import { paletteShortcuts } from './lib/shortcuts.js';
import { modeHints } from './lib/status.js';
import { state } from './app/state.js';
import * as store from './store/store.js';
import {
  setStatus,
  reportFailure,
  setBootState,
  setModeHint,
  termsHost,
} from './app/dom.js';
import { activeCwd } from './app/selectors.js';
import { scrollTrace } from './app/trace.js';
import {
  openLauncher,
  duplicateActiveSession,
  restartActiveSession,
  duplicateActiveSessionChooseTool,
  initLauncher,
} from './app/modals/launcher.js';
import {
  openProjectEditor,
  initProjectEditor,
} from './app/modals/project-editor.js';
import { initCommandPalette } from './app/modals/command-palette.js';
import {
  initSettings,
  initThemeWatch,
  openSettings,
} from './app/modals/settings.js';
import { initWorktrees } from './app/modals/worktrees.js';
import { openHelpOverlay, initHelpOverlay } from './app/modals/help-overlay.js';
import { wireDaemonEvents, reconnectControl } from './app/events.js';
import { createElement, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { Banners } from './components/Banners.js';
import { BootState } from './components/BootState.js';
import { EmptyState } from './components/EmptyState.js';
import { MinimizedTray } from './components/MinimizedTray.js';
import { Sidebar } from './components/Sidebar.js';
import { GridView } from './components/GridView.js';
import { Launcher } from './components/modals/Launcher.js';
import { Settings } from './components/modals/Settings.js';
import { ChoiceDialog } from './components/modals/ChoiceDialog.js';
import { CommandPalette } from './components/modals/CommandPalette.js';
import { HelpOverlay } from './components/modals/HelpOverlay.js';
import { ProjectEditor } from './components/modals/ProjectEditor.js';
import { Worktrees } from './components/modals/Worktrees.js';
import { StatusBar } from './components/StatusBar.js';
import { VersionFooter } from './components/VersionFooter.js';
import { mustEl, pageEl } from './app/el.js';
import { isDaemonRestarting, initBanners, restartHive } from './app/banners.js';
import {
  closeActiveSession,
  reopenLastClosedSession,
} from './app/undo-close.js';
import {
  switchTo,
  switchToProject,
  updateAppTitle,
  minimizeProject,
  restoreProject,
  minimizeSession,
  restoreSession,
  shiftActiveProject,
  enforceViewFloor,
  initView,
} from './app/view.js';
import {
  initKeyboard,
  toggleSidebar,
  toggleProjectGrid,
  toggleAllGrid,
  confirmAndDeleteProject,
  deleteActiveProject,
  openWorktreesForActiveProject,
  navSession,
  reorderActive,
  switchToNthSession,
  jumpToAttention,
  jumpBack,
  navBack,
  navForward,
} from './app/keyboard.js';
import { ensureTerm, bumpFontSize, resetFontSize } from './app/session-term.js';
import {
  setActive,
  setFocusedTile,
  focusActiveTerm,
  refocusActiveTerm,
  withoutNavHistory,
} from './app/focus.js';

// ---------- command palette table ----------

// Shortcut strings come from lib/shortcuts.ts so the palette and the
// ⌘/ help overlay can't drift from each other.
const PALETTE_KEYS = paletteShortcuts({ isMac });

const paletteCommands = [
  {
    id: 'new-project',
    name: 'New Project…',
    run: () => openProjectEditor(null),
  },
  { id: 'new-session', name: 'New Session', run: () => openLauncher() },
  {
    id: 'new-session-worktree',
    name: 'New Session in Worktree',
    run: () => openLauncher(undefined, { forceWorktree: true }),
  },
  {
    id: 'duplicate-session',
    name: 'Duplicate Session',
    run: duplicateActiveSession,
  },
  {
    id: 'duplicate-session-choose-tool',
    name: 'Duplicate Session (choose tool)…',
    run: duplicateActiveSessionChooseTool,
  },
  { id: 'restart-session', name: 'Restart Session', run: restartActiveSession },
  {
    id: 'delete-project',
    name: 'Delete Active Project…',
    run: () => deleteActiveProject(),
  },
  {
    id: 'worktrees',
    name: 'Worktrees…',
    run: () => openWorktreesForActiveProject(),
  },
  {
    id: 'close-session',
    name: 'Close Session',
    run: () => {
      if (state.activeId) closeActiveSession();
    },
  },
  {
    id: 'reopen-closed-session',
    name: 'Reopen Closed Session',
    run: () => reopenLastClosedSession(),
  },
  {
    id: 'new-window',
    name: 'New Window',
    run: () => OpenNewWindow().catch(reportFailure('new window')),
  },
  {
    id: 'open-os-terminal',
    name: 'Open OS Terminal Here',
    run: () =>
      OpenTerminalAt(activeCwd()).catch(reportFailure('open terminal')),
  },
  {
    id: 'close-window',
    name: 'Close Window',
    run: () => CloseWindow().catch(reportFailure('close window')),
  },
  { id: 'toggle-sidebar', name: 'Toggle Sidebar', run: toggleSidebar },
  {
    id: 'toggle-project-grid',
    name: 'Toggle Project Grid',
    run: toggleProjectGrid,
  },
  {
    id: 'toggle-all-grid',
    name: 'Toggle All Sessions Grid',
    run: toggleAllGrid,
  },
  { id: 'zoom-in', name: 'Zoom In', run: () => bumpFontSize(+1) },
  { id: 'zoom-out', name: 'Zoom Out', run: () => bumpFontSize(-1) },
  { id: 'zoom-reset', name: 'Actual Size', run: () => resetFontSize() },
  { id: 'next-session', name: 'Next Session', run: () => navSession(+1) },
  { id: 'prev-session', name: 'Previous Session', run: () => navSession(-1) },
  { id: 'nav-back', name: 'Go Back', run: navBack },
  { id: 'nav-forward', name: 'Go Forward', run: navForward },
  {
    id: 'next-attention',
    name: 'Next Session Needing Attention',
    run: jumpToAttention,
  },
  { id: 'jump-back', name: 'Jump Back to Where You Were', run: jumpBack },
  {
    id: 'move-forward',
    name: 'Move Session Forward',
    run: () => reorderActive(+1),
  },
  {
    id: 'move-backward',
    name: 'Move Session Backward',
    run: () => reorderActive(-1),
  },
  {
    id: 'next-project',
    name: 'Next Project',
    run: () => shiftActiveProject(+1),
  },
  {
    id: 'prev-project',
    name: 'Previous Project',
    run: () => shiftActiveProject(-1),
  },
  {
    id: 'keyboard-shortcuts',
    name: 'Keyboard Shortcuts',
    run: () => openHelpOverlay(),
  },
  { id: 'settings', name: 'Settings…', run: () => openSettings() },
  { id: 'restart-hive', name: 'Restart Hive…', run: () => restartHive() },
  ...Array.from({ length: 9 }, (_, i) => ({
    id: `switch-${i + 1}`,
    name: `Switch to Session ${i + 1}`,
    run: () => switchToNthSession(i + 1),
  })),
].map((c) => ({ ...c, shortcut: PALETTE_KEYS[c.id] ?? '' }));

// ---------- wiring ----------

// Cross-module callbacks are injected here so the modules stay
// acyclic: modals/sidebar/view/keyboard/events never import the focus
// pipeline or SessionTerm directly. Each modal also registers itself
// with the registry focusSnapshot reads.
initLauncher({ setFocusedTile, refocusActiveTerm });
initProjectEditor({ setFocusedTile, refocusActiveTerm });
initCommandPalette({ commands: paletteCommands, focusActiveTerm });
initSettings({ setFocusedTile, refocusActiveTerm });
initWorktrees({
  setFocusedTile,
  refocusActiveTerm,
  // Resuming work in an existing worktree reuses the agent picker —
  // the choice of tool is the same question as for any new session.
  openSessionIn: (projectId, worktreePath, continueConversation) =>
    openLauncher(projectId, { worktreePath, continueConversation }),
});
initHelpOverlay({ setFocusedTile, focusActiveTerm });
// ---------- React islands ----------
//
// One root per migrated region while the rewrite is in flight; Phase 6
// unmounts them and collapses the sidebar, chrome, modals and grid into
// a single root. The handles are kept so that phase has something to
// unmount — see docs/exec-plans/active/react-ui-rewrite.md.
// The pane starts in focused mode. Set before the first paint rather than
// waiting for showSingle(), which only runs once a session exists —
// #terms.single drives the terminal arrangement in layout.css.
termsHost.classList.add('single');

const reactRoots: Root[] = [];

// Each island renders INTO the element the region already owned, so
// every id, grid row and aria attribute in index.html survives. The
// container-level classes those regions toggle (.hidden, .error,
// .mismatch) are applied by the components' own layout effects — they
// sit outside React's tree.
function mountIsland(el: HTMLElement | null, node: ReactNode): void {
  if (!el) return;
  const root = createRoot(el);
  root.render(node);
  reactRoots.push(root);
}

mountIsland(
  mustEl('projects'),
  createElement(Sidebar, {
    switchTo,
    switchToProject,
    minimizeProject,
    restoreProject,
    minimizeSession,
    restoreSession,
    confirmAndDeleteProject,
    refocusActiveTerm,
    trayEl: pageEl('minimized-projects'),
  }),
);
// The grid shell. It renders nothing — its whole job is to run one
// layout effect against app/grid-layout.ts when the store's view, active
// tile or grid scope moves — so it mounts on its own empty, hidden root
// rather than on #terms, whose children are SessionTerm hosts React must
// never own. Mounted right after the sidebar so it is subscribed before
// the first session list lands.
mountIsland(pageEl('grid-root'), createElement(GridView));
// #banners is `display: contents` (layout.css), so the three banners
// stay direct children of the #app grid and keep their row placement.
mountIsland(pageEl('banners'), createElement(Banners));
mountIsland(
  pageEl('status'),
  createElement(StatusBar, { root: pageEl('status') }),
);
mountIsland(
  pageEl('boot-state'),
  createElement(BootState, { root: pageEl('boot-state') }),
);
mountIsland(
  pageEl('empty-state'),
  createElement(EmptyState, { root: pageEl('empty-state') }),
);
mountIsland(
  pageEl('minimized-tray'),
  createElement(MinimizedTray, {
    root: pageEl('minimized-tray'),
    restoreSession,
  }),
);
// Sidebar footer: hive/hived version + build. It takes its own
// "daemon:stale" subscription, so it fills in once the control
// handshake lands — which is why it mounts BEFORE the modals. The e2e
// specs' boot() gate waits on the first island (#projects), and every
// island after that subscribes a commit later; with the seven modal
// roots ahead of it, the handshake could land in the gap and the footer
// would stay empty with nothing to replay it. No modal can be opened
// before boot finishes, so they are the ones that can afford to wait.
mountIsland(
  pageEl('sidebar-hints'),
  createElement(VersionFooter, { root: pageEl('sidebar-hints') }),
);
// The two Phase 3 modals. Both mount on the root their region already
// owns and stay mounted; the store decides whether anything renders
// inside, and the island toggles the root's `hidden` class.
mountIsland(
  pageEl('launcher'),
  createElement(Launcher, {
    root: pageEl('launcher'),
    setFocusedTile,
  }),
);
mountIsland(
  pageEl('settings'),
  createElement(Settings, { root: pageEl('settings') }),
);
// The five Phase 4 modals. Same shape: the island stays mounted on the
// root its region owns, the store decides whether anything renders
// inside it, and the component toggles the root's `hidden` class.
mountIsland(
  pageEl('worktrees'),
  createElement(Worktrees, { root: pageEl('worktrees') }),
);
mountIsland(
  pageEl('project-editor'),
  createElement(ProjectEditor, { root: pageEl('project-editor') }),
);
mountIsland(
  pageEl('help-overlay'),
  createElement(HelpOverlay, { root: pageEl('help-overlay') }),
);
mountIsland(
  pageEl('choice-dialog'),
  createElement(ChoiceDialog, { root: pageEl('choice-dialog') }),
);
mountIsland(
  pageEl('command-palette'),
  createElement(CommandPalette, { root: pageEl('command-palette') }),
);
initBanners();
initView({ ensureTerm, setActive, focusActiveTerm, scrollTrace });
// Seed the status bar's hint slot. setModeHint is otherwise reached only
// from switchTo() and setView(), and a boot with zero sessions calls
// neither — leaving the first-run screen with no shortcuts at all.
setModeHint(modeHints(state.view, isMac));
initKeyboard({
  bumpFontSize,
  resetFontSize,
  focusActiveTerm,
  withoutNavHistory,
});
wireDaemonEvents({
  switchTo,
  enforceViewFloor,
  updateAppTitle,
  focusActiveTerm,
  refocusActiveTerm,
  isDaemonRestarting,
  scrollTrace,
});

initThemeWatch();

// ---------- sidebar resize ----------
//
// Drag the right edge of the sidebar to resize. Width persists across
// reloads. Constrained to a sane min/max so the resizer can't be lost
// off-screen or eat the whole window.
(function setupSidebarResize() {
  // The 220/480 bounds and the load-time clamp live on the store
  // (SIDEBAR_MIN_WIDTH / SIDEBAR_MAX_WIDTH), which also owns the
  // localStorage round-trip. 220 is the design system's sidebar floor
  // (docs/design-docs/ui/tokens.md › Spacing): below it a project card's
  // margins eat the two-line row's name column.
  const app = document.getElementById('app');
  const handle = document.getElementById('sidebar-resizer');
  if (!app || !handle) return;
  // Preserve the successful narrowing inside nested callbacks.
  const appEl = app;
  const handleEl = handle;
  appEl.style.setProperty(
    '--sidebar-width',
    `${store.appStore.getState().sidebarWidth}px`,
  );
  // #app spans the viewport, so pointer clientX maps directly to sidebar width.
  let dragging = false;
  function endDrag() {
    if (!dragging) return;
    dragging = false;
    document.body.classList.remove('resizing-sidebar');
    handleEl.classList.remove('dragging');
    const px = appEl.style.getPropertyValue('--sidebar-width');
    const w = parseInt(px, 10);
    if (Number.isFinite(w)) store.setSidebarWidth(w);
    // Main pane width change reflows terminals automatically: each
    // tile body's ResizeObserver fits its xterm; the termsHost RO
    // re-picks (rows, cols) for the grid.
  }
  handleEl.addEventListener('pointerdown', (e) => {
    e.preventDefault();
    dragging = true;
    document.body.classList.add('resizing-sidebar');
    handleEl.classList.add('dragging');
    // Capture so we keep getting moves/ups even if the cursor leaves the window.
    handleEl.setPointerCapture(e.pointerId);
  });
  handleEl.addEventListener('pointermove', (e) => {
    if (!dragging) return;
    const w = Math.max(
      store.SIDEBAR_MIN_WIDTH,
      Math.min(store.SIDEBAR_MAX_WIDTH, e.clientX),
    );
    appEl.style.setProperty('--sidebar-width', `${w}px`);
  });
  handleEl.addEventListener('pointerup', endDrag);
  handleEl.addEventListener('pointercancel', endDrag);
  // Belt-and-braces: if focus leaves the window mid-drag, end the drag so a
  // stray mousemove on return doesn't snap the sidebar to the cursor.
  window.addEventListener('blur', endDrag);

  // Keyboard a11y: when the resizer has focus, arrow keys adjust width
  // (Shift = larger step). The width change reflows the main pane;
  // tile-body and termsHost ResizeObservers handle the rest.
  function nudge(delta: number) {
    const cur = parseInt(
      getComputedStyle(appEl).getPropertyValue('--sidebar-width'),
      10,
    );
    const base = Number.isFinite(cur) ? cur : store.SIDEBAR_MIN_WIDTH;
    store.setSidebarWidth(base + delta);
    const w = store.appStore.getState().sidebarWidth;
    appEl.style.setProperty('--sidebar-width', `${w}px`);
  }
  handleEl.addEventListener('keydown', (e) => {
    const step = e.shiftKey ? 50 : 10;
    if (e.key === 'ArrowLeft') {
      e.preventDefault();
      nudge(-step);
    } else if (e.key === 'ArrowRight') {
      e.preventDefault();
      nudge(+step);
    } else if (e.key === 'Home') {
      e.preventDefault();
      nudge(-store.SIDEBAR_MAX_WIDTH);
    } else if (e.key === 'End') {
      e.preventDefault();
      nudge(+store.SIDEBAR_MAX_WIDTH);
    }
  });
})();

// ---------- freeze heartbeat ----------
//
// Always-on, low-rate main-thread heartbeat teed to hivegui.log. The
// idle-freeze (thread parked, 0% CPU, no magenta) leaves no other trace
// on disk, so a "went irresponsive" report has nothing to work from.
// A STALL line means the thread was blocked (busy loop / sync stall); a
// silent log across a freeze means the thread was idle-blocked or the
// process wedged. The periodic "alive" line carries window state so an
// occluded/unfocused window is distinguishable from a real block. The
// first STALL after boot also quantifies the slow-startup hang.
(function startFreezeHeartbeat() {
  const NOMINAL_MS = 1000;
  const ALIVE_EVERY = 3; // ~every 3s — tight enough to watch a mem/gap ramp
  let last = (() => {
    try {
      return performance.now();
    } catch {
      return 0;
    }
  })();
  let beat = 0;
  setInterval(() => {
    let now: number;
    try {
      now = performance.now();
    } catch {
      now = last + NOMINAL_MS;
    }
    const gap = now - last;
    last = now;
    beat += 1;
    const heap = jsHeapMB(
      typeof performance !== 'undefined' ? performance : null,
    );
    const st: Record<string, string | number> = {
      vis: document.visibilityState,
      focus:
        typeof document.hasFocus === 'function'
          ? document.hasFocus()
            ? 1
            : 0
          : '?',
      terms: state.terms.size,
      view: state.view,
    };
    if (heap !== null) st.heapMB = heap;
    const line = classifyBeat({
      gap,
      nominalMs: NOMINAL_MS,
      visible: document.visibilityState === 'visible',
      beat,
      aliveEvery: ALIVE_EVERY,
      state: st,
    });
    if (line) {
      try {
        LogFrontend(line);
      } catch {
        /* bridge absent in tests */
      }
    }
  }, NOMINAL_MS);
})();

// ---------- bootstrap ----------

(async () => {
  const t0 = (() => {
    try {
      return performance.now();
    } catch {
      return 0;
    }
  })();
  try {
    LogFrontend('boot: connecting control');
  } catch {
    /* ignore */
  }
  setStatus('connecting…');
  setBootState('Starting hive…');
  try {
    await ConnectControl();
    setStatus('connected');
    try {
      const dt = (() => {
        try {
          return Math.round(performance.now() - t0);
        } catch {
          return -1;
        }
      })();
      LogFrontend(`boot: control connected in ${dt}ms`);
    } catch {
      /* ignore */
    }
  } catch (err) {
    setStatus(`connect failed: ${err}`, true);
    try {
      LogFrontend(`boot: control connect failed: ${err}`);
    } catch {
      /* ignore */
    }
    // Keep trying. The reconnect loop used to be reachable only from
    // control:disconnect, which needs a connection to have existed —
    // so a daemon that was merely slow to come up left the app dead
    // with a red status line and a black pane until relaunch.
    //
    // Bounded, unlike the mid-session loop: if the daemon never comes
    // up, retrying forever just re-dials a hived that is failing to
    // start. Hand the user a Retry instead.
    void retryBoot();
  }
})();

const BOOT_RETRY_ATTEMPTS = 5;

async function retryBoot(): Promise<void> {
  setBootState('Waiting for the hive daemon…');
  if (await reconnectControl(BOOT_RETRY_ATTEMPTS)) return;
  setStatus('could not reach the hive daemon', true);
  setBootState(
    'Could not reach the hive daemon. Check hived.log for why it failed to start.',
    { retry: () => void retryBoot() },
  );
}
