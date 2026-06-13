// Composition root. Every subsystem lives in src/app/* (pure logic in
// src/lib/*); this file builds the command table, injects the
// cross-module callbacks, and boots the control connection. Keep it
// free of behavior — if a function body wants to live here, it almost
// certainly belongs in a module.

import '@xterm/xterm/css/xterm.css';

import {
  ConnectControl, KillSession, OpenNewWindow, CloseWindow, OpenTerminalAt,
} from './bridge.js';
import { isMac } from './lib/platform.js';
import { paletteShortcuts, footerHints } from './lib/shortcuts.js';
import { state } from './app/state.js';
import { setStatus, reportFailure } from './app/dom.js';
import { activeCwd } from './app/selectors.js';
import { scrollTrace } from './app/trace.js';
import {
  openLauncher, duplicateActiveSession, restartActiveSession,
  duplicateActiveSessionChooseTool, initLauncher,
} from './app/modals/launcher.js';
import { openProjectEditor, initProjectEditor } from './app/modals/project-editor.js';
import { initCommandPalette } from './app/modals/command-palette.js';
import { openHelpOverlay, initHelpOverlay } from './app/modals/help-overlay.js';
import { initSidebar } from './app/sidebar.js';
import { wireDaemonEvents } from './app/events.js';
import { isDaemonRestarting, initBanners } from './app/banners.js';
import {
  switchTo, switchToProject, updateAppTitle, renderMinimizedTray,
  renderEmptyState, shiftActiveProject, initView,
} from './app/view.js';
import {
  initKeyboard, toggleSidebar, toggleProjectGrid, toggleAllGrid,
  confirmAndDeleteProject, deleteActiveProject, navSession, reorderActive,
  switchToNthSession,
} from './app/keyboard.js';
import { ensureTerm, bumpFontSize, resetFontSize } from './app/session-term.js';
import {
  setActive, setFocusedTile, focusActiveTerm, refocusActiveTerm, initFocus,
} from './app/focus.js';

// ---------- command palette table ----------

// Shortcut strings come from lib/shortcuts.js so the palette and the
// ⌘/ help overlay can't drift from each other.
const PALETTE_KEYS = paletteShortcuts({ isMac });

const paletteCommands = [
  { id: 'new-project',          name: 'New Project…',                run: () => openProjectEditor(null) },
  { id: 'new-session',          name: 'New Session',                 run: () => openLauncher() },
  { id: 'new-session-worktree', name: 'New Session in Worktree',     run: () => openLauncher(undefined, { forceWorktree: true }) },
  { id: 'duplicate-session',    name: 'Duplicate Session',           run: duplicateActiveSession },
  { id: 'duplicate-session-choose-tool', name: 'Duplicate Session (choose tool)…', run: duplicateActiveSessionChooseTool },
  { id: 'restart-session',      name: 'Restart Session',             run: restartActiveSession },
  { id: 'delete-project',       name: 'Delete Active Project…',      run: () => deleteActiveProject() },
  { id: 'close-session',        name: 'Close Session',               run: () => { if (state.activeId) KillSession(state.activeId, false).catch(reportFailure('close')); } },
  { id: 'new-window',           name: 'New Window',                  run: () => OpenNewWindow().catch(reportFailure('new window')) },
  { id: 'open-os-terminal',     name: 'Open OS Terminal Here',       run: () => OpenTerminalAt(activeCwd()).catch(reportFailure('open terminal')) },
  { id: 'close-window',         name: 'Close Window',                run: () => CloseWindow().catch(reportFailure('close window')) },
  { id: 'toggle-sidebar',       name: 'Toggle Sidebar',              run: toggleSidebar },
  { id: 'toggle-project-grid',  name: 'Toggle Project Grid',         run: toggleProjectGrid },
  { id: 'toggle-all-grid',      name: 'Toggle All Sessions Grid',    run: toggleAllGrid },
  { id: 'zoom-in',              name: 'Zoom In',                     run: () => bumpFontSize(+1) },
  { id: 'zoom-out',             name: 'Zoom Out',                    run: () => bumpFontSize(-1) },
  { id: 'zoom-reset',           name: 'Actual Size',                 run: () => resetFontSize() },
  { id: 'next-session',         name: 'Next Session',                run: () => navSession(+1) },
  { id: 'prev-session',         name: 'Previous Session',            run: () => navSession(-1) },
  { id: 'move-forward',         name: 'Move Session Forward',        run: () => reorderActive(+1) },
  { id: 'move-backward',        name: 'Move Session Backward',       run: () => reorderActive(-1) },
  { id: 'next-project',         name: 'Next Project',                run: () => shiftActiveProject(+1) },
  { id: 'prev-project',         name: 'Previous Project',            run: () => shiftActiveProject(-1) },
  { id: 'keyboard-shortcuts',   name: 'Keyboard Shortcuts',          run: () => openHelpOverlay() },
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
initHelpOverlay({ setFocusedTile, focusActiveTerm });
initSidebar({ switchTo, switchToProject, confirmAndDeleteProject, renderEmptyState, refocusActiveTerm });
initBanners();
initView({ ensureTerm, setActive, focusActiveTerm, scrollTrace });
initKeyboard({ bumpFontSize, resetFontSize, focusActiveTerm });
initFocus({ ensureTerm });
wireDaemonEvents({
  switchTo, renderMinimizedTray, updateAppTitle, focusActiveTerm,
  refocusActiveTerm, isDaemonRestarting, scrollTrace,
});

// Sidebar footer hints: the static HTML text is the mac-glyph
// fallback; re-render from the shared shortcut table so non-mac
// platforms see Ctrl+-style hints that match the real bindings.
const footerHintsEl = document.getElementById('sidebar-hints');
if (footerHintsEl) footerHintsEl.textContent = footerHints({ isMac });

// ---------- sidebar resize ----------
//
// Drag the right edge of the sidebar to resize. Width persists across
// reloads. Constrained to a sane min/max so the resizer can't be lost
// off-screen or eat the whole window.
(function setupSidebarResize() {
  const MIN = 140, MAX = 480;
  const app = document.getElementById('app');
  const handle = document.getElementById('sidebar-resizer');
  if (!app || !handle) return;
  const saved = parseInt(localStorage.getItem('hive.sidebarWidth') || '', 10);
  if (Number.isFinite(saved)) {
    app.style.setProperty('--sidebar-width', `${Math.max(MIN, Math.min(MAX, saved))}px`);
  }
  // #app spans the viewport, so pointer clientX maps directly to sidebar width.
  let dragging = false;
  function endDrag() {
    if (!dragging) return;
    dragging = false;
    document.body.classList.remove('resizing-sidebar');
    handle.classList.remove('dragging');
    const px = app.style.getPropertyValue('--sidebar-width');
    const w = parseInt(px, 10);
    if (Number.isFinite(w)) localStorage.setItem('hive.sidebarWidth', String(w));
    // Main pane width change reflows terminals automatically: each
    // tile body's ResizeObserver fits its xterm; the termsHost RO
    // re-picks (rows, cols) for the grid.
  }
  handle.addEventListener('pointerdown', (e) => {
    e.preventDefault();
    dragging = true;
    document.body.classList.add('resizing-sidebar');
    handle.classList.add('dragging');
    // Capture so we keep getting moves/ups even if the cursor leaves the window.
    handle.setPointerCapture(e.pointerId);
  });
  handle.addEventListener('pointermove', (e) => {
    if (!dragging) return;
    const w = Math.max(MIN, Math.min(MAX, e.clientX));
    app.style.setProperty('--sidebar-width', `${w}px`);
  });
  handle.addEventListener('pointerup', endDrag);
  handle.addEventListener('pointercancel', endDrag);
  // Belt-and-braces: if focus leaves the window mid-drag, end the drag so a
  // stray mousemove on return doesn't snap the sidebar to the cursor.
  window.addEventListener('blur', endDrag);

  // Keyboard a11y: when the resizer has focus, arrow keys adjust width
  // (Shift = larger step). The width change reflows the main pane;
  // tile-body and termsHost ResizeObservers handle the rest.
  function nudge(delta) {
    const cur = parseInt(getComputedStyle(app).getPropertyValue('--sidebar-width'), 10);
    const base = Number.isFinite(cur) ? cur : 200;
    const w = Math.max(MIN, Math.min(MAX, base + delta));
    app.style.setProperty('--sidebar-width', `${w}px`);
    localStorage.setItem('hive.sidebarWidth', String(w));
  }
  handle.addEventListener('keydown', (e) => {
    const step = e.shiftKey ? 50 : 10;
    if (e.key === 'ArrowLeft')       { e.preventDefault(); nudge(-step); }
    else if (e.key === 'ArrowRight') { e.preventDefault(); nudge(+step); }
    else if (e.key === 'Home')       { e.preventDefault(); nudge(-MAX); }
    else if (e.key === 'End')        { e.preventDefault(); nudge(+MAX); }
  });
})();

// ---------- bootstrap ----------

(async () => {
  setStatus('connecting…');
  try {
    await ConnectControl();
    setStatus('connected');
  } catch (err) {
    setStatus(`connect failed: ${err}`, true);
  }
})();
