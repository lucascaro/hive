// Composition root. Every subsystem lives in src/app/* (pure logic in
// src/lib/*); this file builds the command table, injects the
// cross-module callbacks, mounts the single React root, and boots the
// control connection. Keep it free of behavior — if a function body
// wants to live here, it almost certainly belongs in a module.
//
// Bootstrap order, and why each step is where it is:
//
//   1. theme      — ./theme/theme's import stamps data-theme; the
//                   pre-paint half already ran from index.html's inline
//                   script (master plan, Invariant 9).
//   2. hydrate    — importing store/store.js runs initialData(), which
//                   reads view / font size / sidebar width out of
//                   localStorage. It is an import side effect, not a
//                   call, so it is already done by the time any line
//                   below runs. The collapsed / minimized project sets
//                   are the exception: their keys are namespaced by the
//                   daemon's state-dir id, so the bootstrap block at the
//                   bottom hydrates them asynchronously, before connect.
//   3. wire       — the daemon event handlers are registered BEFORE the
//                   root mounts, so a fast handshake writes the store
//                   rather than racing a tree that has not subscribed.
//   4. mount      — one createRoot on #react-root; see components/App.tsx
//                   for why the regions are portals.
//   5. heartbeat  — last: it is a diagnostic and must never be in the
//                   way of first paint.

import './theme/theme';
import '@xterm/xterm/css/xterm.css';

import {
  ConnectControl,
  StateDirID,
  OpenNewWindow,
  CloseWindow,
  OpenTerminalAt,
  LogFrontend,
} from './bridge.js';
import { classifyBeat, jsHeapMB } from './lib/freeze-heartbeat.js';
import { isMac } from './lib/platform.js';
import { paletteShortcuts } from './lib/shortcuts.js';
import { modeHints } from './lib/status.js';
import * as store from './store/store.js';
import { termsMap } from './store/terms.js';

// Live read of the store, for the heartbeat and the command table.
const appData = () => store.appStore.getState();
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
import { openWhatsNew, initWhatsNew } from './app/modals/whats-new.js';
import { wireDaemonEvents, reconnectControl } from './app/events.js';
import { flushSync } from 'react-dom';
import { createRoot } from 'react-dom/client';
import { App } from './components/App.js';
import { mustEl } from './app/el.js';
import {
  isDaemonRestarting,
  initBanners,
  manualUpdateCheck,
  reloadGui,
  restartHive,
} from './app/banners.js';
import {
  closeActiveSession,
  reopenLastClosedSession,
} from './app/undo-close.js';
import {
  switchTo,
  updateAppTitle,
  shiftActiveProject,
  enforceViewFloor,
  initView,
} from './app/view.js';
import {
  initKeyboard,
  toggleSidebar,
  toggleProjectGrid,
  toggleAllGrid,
  focusActiveSession,
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
  { id: 'whats-new', name: "What's New…", run: () => openWhatsNew() },
  {
    id: 'close-session',
    name: 'Close Session',
    run: () => {
      if (appData().activeId) closeActiveSession();
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
  {
    id: 'focus-active-session',
    name: 'Focus Active Session',
    run: focusActiveSession,
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
  { id: 'reload-gui', name: 'Reload GUI', run: () => reloadGui() },
  {
    id: 'restart-hive',
    name: 'Restart Daemon… (ends all sessions)',
    run: () => restartHive(),
  },
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
initWhatsNew({ setFocusedTile, focusActiveTerm });
// ---------- boot the app ----------

// The pane starts in focused mode. Set before the first paint rather than
// waiting for applySingle(), which only runs once a session exists —
// #terms.single drives the terminal arrangement in layout.css.
termsHost.classList.add('single');

initBanners();
initView({ ensureTerm, setActive, focusActiveTerm, scrollTrace });
// Seed the status bar's hint slot. setModeHint is otherwise reached only
// from switchTo() and setView(), and a boot with zero sessions calls
// neither — leaving the first-run screen with no shortcuts at all.
setModeHint(modeHints(appData().view, isMac));
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
  checkForUpdates: () => void manualUpdateCheck(),
  scrollTrace,
});

initThemeWatch();

// ---------- the React root ----------
//
// One root, mounted on the empty hidden #react-root. App renders every
// region as a portal into the element index.html already owns, so this
// container holds nothing and needs no placement in the #app grid — see
// components/App.tsx for why the tree cannot simply own #app.
//
// Mounted after wireDaemonEvents so no handshake can land in a gap
// before the tree is subscribed, and before the heartbeat so the first
// paint is never behind a diagnostic.
// index.html seeds three of the portal targets with static markup so the
// window paints something before any module script runs: #status's two
// slots ("connecting…"), #boot-state's card, and #sidebar-hints' two
// version spans. A portal APPENDS into its container — unlike the island
// roots it replaces, each of which cleared the container it mounted on —
// so that seed has to go first or every one of those ids would exist
// twice. Cleared here, with the first commit flushed synchronously, so
// no frame is ever painted between the two.
for (const id of ['status', 'boot-state', 'sidebar-hints']) {
  mustEl(id).replaceChildren();
}
flushSync(() => createRoot(mustEl('react-root')).render(<App />));

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
      terms: termsMap().size,
      view: appData().view,
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
  // Load the persisted collapse/minimize sets BEFORE connecting. Their
  // storage keys are suffixed with the daemon's state-dir id, which only
  // this binding knows, and the first project:list would prune sets that
  // had not been loaded yet — persisting [] and emptying the tray (#340).
  // A failure here leaves persistence off for the session rather than
  // falling back to the shared un-suffixed key.
  try {
    store.hydratePersistedProjectSets(await StateDirID());
  } catch {
    // Wrapped like every other LogFrontend in this file: the reason
    // StateDirID throws is usually "no bridge", which is exactly when
    // LogFrontend throws too. An unwrapped call here would reject the
    // bootstrap IIFE and ConnectControl below would never run — turning
    // "persistence is off this session" into "the app never connects".
    try {
      LogFrontend('boot: StateDirID failed; project sets not persisted');
    } catch {
      /* ignore */
    }
  }
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
