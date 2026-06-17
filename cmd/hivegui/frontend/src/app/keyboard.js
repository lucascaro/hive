// ---------- keyboard, menu actions, app commands ----------
//
// Moved verbatim from main.js. Font helpers and focusActiveTerm are
// injected via initKeyboard(deps) (they move in stage 6); everything
// else is imported from the sibling modules.

import {
  EventsOn, KillSession, KillProject, Confirm, UpdateSession,
  OpenNewWindow, CloseWindow, OpenTerminalAt, SetClipboardText, Notify,
} from '../bridge.js';
import { state } from './state.js';
import { reportFailure } from './dom.js';
import { orderedSessions, activeCwd, activeProjectId } from './selectors.js';
import { cmdOrCtrl } from '../lib/platform.js';
import {
  launcherEl, launcherState, moveLauncherSelection, activateLauncherSelection,
  openLauncher, closeLauncher, duplicateActiveSession,
  duplicateActiveSessionChooseTool, restartActiveSession,
} from './modals/launcher.js';
import { editorEl, openProjectEditor } from './modals/project-editor.js';
import { openCommandPalette } from './modals/command-palette.js';
import { openHelpOverlay, closeHelpOverlay, toggleHelpOverlay } from './modals/help-overlay.js';
import {
  switchTo, setView, gridSpatialMove, shiftActiveProject,
} from './view.js';
import { manualUpdateCheck } from './banners.js';

let deps = {
  bumpFontSize: () => {},
  resetFontSize: () => {},
  focusActiveTerm: () => {},
};

export function initKeyboard(injected) {
  deps = injected;
}


window.addEventListener('keydown', (e) => {
  if (!launcherEl.classList.contains('hidden')) {
    const handle = (fn) => { e.preventDefault(); e.stopPropagation(); fn(); };
    if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey)) return handle(() => moveLauncherSelection(+1));
    if (e.key === 'ArrowUp'   || (e.key === 'Tab' && e.shiftKey))  return handle(() => moveLauncherSelection(-1));
    if (e.key === 'Enter')   return handle(activateLauncherSelection);
    if (e.key === 'Escape')  return handle(closeLauncher);
    if (cmdOrCtrl(e) && (e.key === 'n' || e.key === 'N')) return handle(closeLauncher);
    // Digit shortcut: 1–9 picks the corresponding row. Skipped when
    // a modifier is held so things like ⌘1 (browser tab switch) and
    // ⌘+ aren't swallowed.
    if (!e.metaKey && !e.ctrlKey && !e.altKey && /^[1-9]$/.test(e.key)) {
      const i = parseInt(e.key, 10) - 1;
      if (i < launcherState.items.length) {
        return handle(() => {
          launcherState.selected = i;
          activateLauncherSelection();
        });
      }
    }
  }
  if (!editorEl.classList.contains('hidden')) {
    return; // editor's own listener handles keys
  }
  const _palette = document.getElementById('command-palette');
  if (_palette && !_palette.classList.contains('hidden')) {
    return; // palette's own listener handles keys
  }
  const _help = document.getElementById('help-overlay');
  if (_help && !_help.classList.contains('hidden')) {
    if (e.key === 'Escape' || (cmdOrCtrl(e) && e.key === '/')) {
      e.preventDefault();
      e.stopPropagation();
      closeHelpOverlay();
    } else if (e.key === 'Tab') {
      // aria-modal promises focus stays inside the dialog. The close
      // button is its only focusable element, so trap Tab on it —
      // otherwise focus walks into the page (eventually a hidden
      // terminal's textarea) and keystrokes leak behind the backdrop.
      e.preventDefault();
      e.stopPropagation();
      document.getElementById('help-overlay-close')?.focus();
    }
    return; // overlay owns the keyboard while open
  }

  // Dead-session overlay: route Enter/Escape to the active session's
  // overlay if it's shown. In grid mode the user can still click any
  // tile's buttons directly; this just handles the focused tile.
  if (state.activeId) {
    const t = state.terms.get(state.activeId);
    if (t?.deadOverlayShown) {
      if (e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); t._closeDead(); return; }
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); t._dismissDead(); return; }
    }
  }

  const swallow = () => { e.preventDefault(); e.stopPropagation(); };

  // Ctrl+` opens an OS terminal at the active session's worktree.
  // Mirrors VS Code; intentionally Ctrl on every platform — macOS
  // reserves ⌘` for native window cycling, so we never bind to it.
  // Handled before the ⌘/Ctrl gate below so it fires on mac too.
  if (e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey && e.code === 'Backquote') {
    swallow();
    OpenTerminalAt(activeCwd()).catch(reportFailure('open terminal'));
    return;
  }

  const meta = cmdOrCtrl(e);
  if (!meta) return;

  if (e.key === '=' || e.key === '+') {
    swallow();
    deps.bumpFontSize(+1);
    return;
  }
  if (e.key === '-' || e.key === '_') {
    swallow();
    deps.bumpFontSize(-1);
    return;
  }
  if (e.key === '0') {
    swallow();
    deps.resetFontSize();
    return;
  }

  if ((e.key === 'k' || e.key === 'K') && e.shiftKey) {
    swallow();
    openCommandPalette();
    return;
  }
  if (e.key === '/') {
    swallow();
    openHelpOverlay();
    return;
  }
  if (e.key === 'p' || e.key === 'P') {
    swallow();
    if (e.shiftKey) duplicateActiveSessionChooseTool();
    else duplicateActiveSession();
  } else if (e.key === 't' || e.key === 'T') {
    swallow();
    if (e.shiftKey) openLauncher(undefined, { forceWorktree: true });
    else openLauncher();
  } else if (e.key === 'Backspace' && e.shiftKey) {
    swallow();
    deleteActiveProject();
  } else if (e.key === 's' || e.key === 'S') {
    swallow();
    // Route through toggleSidebar so the keyboard path stays in
    // lockstep with the menu / command-palette path (including the
    // post-reflow refocus added for #208 R3). Inline class flips
    // here previously skipped the refocus and stranded keystrokes
    // on document.body after a prior window resize.
    toggleSidebar();
  } else if (e.key === 'g' || e.key === 'G') {
    swallow();
    if (e.shiftKey) {
      setView(state.view === 'grid-all' ? 'single' : 'grid-all');
    } else {
      setView(state.view === 'grid-project' ? 'single' : 'grid-project');
    }
  } else if (e.key === 'Enter') {
    // ⌘Enter mirrors ⌘G: in a grid mode it maximizes the active
    // tile back to single mode; in single mode it expands to a
    // per-project grid for context.
    swallow();
    if (state.view === 'single') setView('grid-project');
    else setView('single');
  } else if (e.key === 'n' || e.key === 'N') {
    swallow();
    if (e.shiftKey) {
      OpenNewWindow().catch(reportFailure('new window'));
    } else {
      // ⌘N — new project. (⌥⌘N is reserved by macOS Spotlight.)
      openProjectEditor(null);
    }
  } else if (e.key === 'w' || e.key === 'W') {
    swallow();
    if (e.shiftKey) {
      CloseWindow().catch(reportFailure('close window'));
    } else if (state.activeId) {
      // force=false: lets the daemon refuse with worktree_dirty if
      // the worktree has uncommitted changes; the control:error
      // handler then shows a confirm dialog and retries with force.
      KillSession(state.activeId, false).catch(reportFailure('close'));
    }
  } else if (/^[1-9]$/.test(e.key)) {
    const idx = parseInt(e.key, 10) - 1;
    const ord = orderedSessions();
    if (idx < ord.length) {
      swallow();
      switchTo(ord[idx].id);
    }
  } else if (e.key === 'ArrowLeft') {
    swallow();
    if (state.view !== 'single') gridSpatialMove(-1, 0);
    else moveActiveSession(-1, e.shiftKey);
  } else if (e.key === 'ArrowRight') {
    swallow();
    if (state.view !== 'single') gridSpatialMove(+1, 0);
    else moveActiveSession(+1, e.shiftKey);
  } else if (e.key === 'ArrowUp') {
    swallow();
    if (state.view !== 'single') gridSpatialMove(0, -1);
    else moveActiveSession(-1, e.shiftKey);
  } else if (e.key === 'ArrowDown') {
    swallow();
    if (state.view !== 'single') gridSpatialMove(0, +1);
    else moveActiveSession(+1, e.shiftKey);
  } else if (e.key === '[') {
    swallow();
    shiftActiveProject(-1);
  } else if (e.key === ']') {
    swallow();
    shiftActiveProject(+1);
  }
}, true);

// ---------- menu actions ----------
//
// Native menu items emit `menu:<action>` events from cmd/hivegui/menu.go.
// They dispatch to the same handlers as the keyboard listener above so the
// menu and keyboard stay in lockstep — when you add a shortcut, add it
// here AND in menu.go.

export function toggleSidebar() {
  document.getElementById('app').classList.toggle('sidebar-hidden');
  // Layout reflow → tile bodies resize → ResizeObserver fits xterm,
  // and fit.fit() can synchronously fire focusout on the helper-
  // textarea as the canvas re-sizes. Without re-asserting focus,
  // keystrokes strand on document.body even though the visual
  // .term-focused stays correctly pinned on the active tile (#208 R3).
  //
  // Sync-fire focusActiveTerm so setFocusedTile's rAF retry loop is
  // armed before the first focusout; staggered delayed re-fires catch
  // any focusout that escapes the standard 8-frame retry budget
  // (later RO callbacks, WebGL canvas swap, DPR settle). All calls
  // are idempotent — re-focusing an already-focused element is a no-op.
  deps.focusActiveTerm();
  setTimeout(() => deps.focusActiveTerm(), 32);
  setTimeout(() => deps.focusActiveTerm(), 100);
  setTimeout(() => deps.focusActiveTerm(), 250);
}

export function toggleProjectGrid() {
  setView(state.view === 'grid-project' ? 'single' : 'grid-project');
}

export function toggleAllGrid() {
  setView(state.view === 'grid-all' ? 'single' : 'grid-all');
}

export function navSession(delta) {
  if (state.view !== 'single') {
    gridSpatialMove(delta > 0 ? +1 : -1, 0);
  } else {
    moveActiveSession(delta, false);
  }
}

export function reorderActive(delta) {
  if (state.view === 'single') moveActiveSession(delta, true);
  else gridSpatialMove(delta > 0 ? +1 : -1, 0);
}

export function switchToNthSession(n) {
  const ord = orderedSessions();
  if (n - 1 < ord.length) switchTo(ord[n - 1].id);
}

// Debug: arm/disarm the scroll tracer. trace.js latches hive.debug at
// module load, so the new state only takes effect after a reload — do it
// here so the user never needs the devtools console to flip the gate.
function toggleScrollDebug() {
  let on = false;
  try { on = localStorage.getItem('hive.debug') === '1'; } catch { /* storage off */ }
  try { localStorage.setItem('hive.debug', on ? '0' : '1'); } catch { /* storage off */ }
  location.reload();
}

// Debug: copy the captured scroll trace to the clipboard via the Go side
// (works without devtools and without a clipboard user-gesture). Reuses
// window.__hive_dumpscroll (trace.js) so the dump shape matches what the
// e2e harness and bug reports expect.
function copyScrollTrace() {
  const dump = typeof window.__hive_dumpscroll === 'function'
    ? window.__hive_dumpscroll()
    : { enabled: false, ring: window.__hive_scrolltrace || [], lastJump: null };
  SetClipboardText(JSON.stringify(dump)).catch(reportFailure('copy scroll trace'));
  const n = dump.ring?.length ?? 0;
  const body = dump.enabled
    ? `Copied ${n} scroll event${n === 1 ? '' : 's'} to the clipboard.`
    : 'Scroll debug is OFF — run "Toggle Scroll Debug" first, reload, reproduce, then copy.';
  Notify('Hive', 'Scroll trace', body, 'scroll-trace').catch(() => {});
}

const menuActions = {
  'menu:new-session': () => openLauncher(),
  'menu:new-session-worktree': () => openLauncher(undefined, { forceWorktree: true }),
  'menu:duplicate-session': duplicateActiveSession,
  'menu:duplicate-session-choose-tool': duplicateActiveSessionChooseTool,
  'menu:restart-session': restartActiveSession,
  'menu:new-project': () => openProjectEditor(null),
  'menu:delete-project': () => deleteActiveProject(),
  'menu:command-palette': () => openCommandPalette(),
  'menu:close-session': () => { if (state.activeId) KillSession(state.activeId, false).catch(reportFailure('close')); },
  'menu:zoom-in': () => deps.bumpFontSize(+1),
  'menu:zoom-out': () => deps.bumpFontSize(-1),
  'menu:zoom-reset': () => deps.resetFontSize(),
  'menu:toggle-sidebar': toggleSidebar,
  'menu:toggle-project-grid': toggleProjectGrid,
  'menu:toggle-all-grid': toggleAllGrid,
  'menu:next-session': () => navSession(+1),
  'menu:prev-session': () => navSession(-1),
  'menu:move-session-forward': () => reorderActive(+1),
  'menu:move-session-backward': () => reorderActive(-1),
  'menu:next-project': () => shiftActiveProject(+1),
  'menu:prev-project': () => shiftActiveProject(-1),
  'menu:check-for-updates': () => manualUpdateCheck(),
  // Must toggle, not just open: the native ⌘/ accelerator intercepts
  // the key before the webview on macOS, so the keydown close path
  // (Escape/⌘/ in the window listener) never sees ⌘/ while the menu
  // owns it.
  'menu:keyboard-shortcuts': () => toggleHelpOverlay(),
  'menu:toggle-scroll-debug': () => toggleScrollDebug(),
  'menu:copy-scroll-trace': () => copyScrollTrace(),
};
for (const [name, fn] of Object.entries(menuActions)) {
  EventsOn(name, fn);
}
for (let i = 1; i <= 9; i++) {
  EventsOn(`menu:switch-${i}`, () => switchToNthSession(i));
}

// ---------- delete project ----------

// confirmAndDeleteProject is the single confirm + KillProject path
// shared by the sidebar ✕ button and the ⇧⌘⌫ shortcut. Kept as one
// function so the prompt text and killSessions logic can't drift.
export async function confirmAndDeleteProject(proj) {
  if (!proj) return;
  const sessions = state.sessions.filter(
    (s) => (s.projectId ?? s.project_id) === proj.id,
  );
  const msg = sessions.length
    ? `Delete project "${proj.name}" and kill ${sessions.length} session${sessions.length === 1 ? '' : 's'}?`
    : `Delete project "${proj.name}"?`;
  const ok = await Confirm('Delete project', msg);
  if (!ok) return;
  KillProject(proj.id, sessions.length > 0).catch(reportFailure('delete project'));
}

export function deleteActiveProject() {
  const pid = activeProjectId();
  confirmAndDeleteProject(state.projects.find((p) => p.id === pid));
}

// moveActiveSession walks the (project_order, session_order) list.
// reorder=true moves the session within its project only.
export function moveActiveSession(delta, reorder) {
  const ord = orderedSessions();
  const n = ord.length;
  if (n === 0) return;
  const idx = ord.findIndex((s) => s.id === state.activeId);
  if (idx < 0) {
    switchTo(ord[0].id);
    return;
  }
  if (reorder) {
    const cur = state.sessions.find((s) => s.id === state.activeId);
    const sib = state.sessions
      .filter((s) => (s.projectId ?? s.project_id) === (cur.projectId ?? cur.project_id))
      .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
    const sIdx = sib.findIndex((s) => s.id === state.activeId);
    const next = (sIdx + delta + sib.length) % sib.length;
    UpdateSession(state.activeId, '', '', next).catch(reportFailure('reorder'));
    return;
  }
  const next = (idx + delta + n) % n;
  switchTo(ord[next].id);
}
