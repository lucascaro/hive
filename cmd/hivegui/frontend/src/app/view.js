// ---------- view / grid / tray / empty state ----------
//
// Moved verbatim from main.js. ensureTerm / setActive /
// focusActiveTerm and the scroll tracer are injected via
// initView(deps) — they live in session-term/focus modules (stage 6)
// and main.js.

import { WindowSetTitle } from '../bridge.js';
import { state } from './state.js';
import { termsHost, setStatus } from './dom.js';
import { orderedSessions, activeProjectId } from './selectors.js';
import { updateSidebarSelection } from './sidebar.js';
import { openLauncher } from './modals/launcher.js';
import { openProjectEditor } from './modals/project-editor.js';
import { buildGridLayout, computeSpatialMove } from '../lib/grid.js';
import { VIEW_STORAGE_KEY } from '../lib/view.js';
import { filterMinimized } from '../lib/minimized.js';
import { snapVisibleTermsToBottom } from '../lib/view-scroll.js';
import { emptyStateModel } from '../lib/empty-state.js';
import { isMac } from '../lib/platform.js';

let deps = {
  ensureTerm: () => {},
  setActive: () => {},
  focusActiveTerm: () => {},
  scrollTrace: { rec: Object.assign(() => {}, { enabled: false }) },
};

export function initView(injected) {
  deps = injected;
}

export function showSingle(id) {
  termsHost.classList.add('single');
  termsHost.classList.remove('grid');
  // Hide everything except the active tile.
  for (const [sid, st] of state.terms) {
    if (sid === id) st.show();
    else st.hide();
    st.host.classList.remove('in-grid', 'active');
  }
  const st = id ? state.terms.get(id) : null;
  if (st) st.ensureAttached();
}

export function switchTo(id) {
  if (id === state.activeId && state.view === 'single') {
    deps.focusActiveTerm();
    return;
  }
  deps.setActive(id);
  let info = null;
  if (id) {
    info = state.sessions.find((s) => s.id === id);
    if (info) deps.ensureTerm(info);
  }
  // Retarget the grid scope if the new session belongs to a different
  // project than the one currently shown in grid-project mode.
  if (state.view === 'grid-project' && info) {
    const pid = info.projectId ?? info.project_id;
    if (pid && pid !== state.gridProjectId) state.gridProjectId = pid;
  }
  if (state.view === 'single') showSingle(id);
  else renderGrid();
  updateSidebarSelection();
  setStatus(info ? info.name : '');
  updateAppTitle();
  // setActive() called focusActiveTerm() before ensureTerm() existed
  // for a brand-new session — re-focus now that the SessionTerm is
  // created and visible. Without this, typing after creating a
  // session lands in whichever terminal had focus before.
  if (id) deps.focusActiveTerm();
}

// updateAppTitle composes "Hive — <session> — <termTitle>" and pushes
// it to both document.title and the native window title bar. The
// termTitle slot is whatever the running TUI most recently set via
// the OSC 0/2 escape sequence; empty if the program never set one.
//
// Throttled with a trailing-edge timer: programs like fish prompts
// or progress encoders can fire OSC 0/2 dozens of times per second,
// and each WindowSetTitle is a Wails IPC round-trip. 100ms keeps the
// title visibly responsive without flooding the bridge.
let _appTitleTimer = null;
export function updateAppTitle() {
  if (_appTitleTimer) return;
  _appTitleTimer = setTimeout(() => {
    _appTitleTimer = null;
    const id = state.activeId;
    const info = id ? state.sessions.find((s) => s.id === id) : null;
    const parts = ['Hive'];
    if (info?.name) parts.push(info.name);
    const t = id ? state.terms.get(id) : null;
    if (t?.termTitle && t.termTitle !== info?.name) parts.push(t.termTitle);
    const title = parts.join(' — ');
    document.title = title;
    try { WindowSetTitle(title); } catch (_) { /* runtime not ready */ }
  }, 100);
}

// switchToProject activates a project: in grid-project mode it
// retargets the grid, and in any mode it makes the project's first
// session the active one. Empty projects are still selectable —
// currentProjectId is set so ⌘N targets them correctly.
export function switchToProject(pid) {
  if (!pid) return;
  state.currentProjectId = pid;
  if (state.view === 'grid-project') state.gridProjectId = pid;
  const sessions = state.sessions
    .filter((s) => (s.projectId ?? s.project_id) === pid)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  if (sessions[0]) switchTo(sessions[0].id);
  else {
    state.activeId = null;
    if (state.view === 'single') showSingle(null);
    else renderGrid();
    updateSidebarSelection();
  }
}

// gridLayout caches the (rows, cols) chosen for the current scope plus
// the per-tile placement so the keyboard navigation logic doesn't have
// to recompute. assignments[i] = { row, col, rowSpan } — tiles above
// last-row empty cells extend downward to fill the grid (matches
// current Hive's behavior). cellMap[row*cols + col] = session index.
let gridLayout = { rows: 1, cols: 1, sessions: [], assignments: [], cellMap: [] };

// renderGrid lays out every tile that should be visible in the
// current grid scope. Tiles for other sessions are hidden but kept
// alive (so their xterm scrollback persists across mode switches).
export function renderGrid() {
  const _t0 = deps.scrollTrace.rec.enabled ? performance.now() : 0;
  termsHost.classList.remove('single');
  termsHost.classList.add('grid');
  const gridSessions = gridScopeSessions();
  const gridIDs = new Set(gridSessions.map((s) => s.id));
  const n = gridSessions.length;

  // Ensure every grid session has a SessionTerm and is attached.
  // Move tiles into the desired DOM order (row-major) so that flexbox
  // / CSS grid honors the navigation order without us having to set
  // grid-row/column explicitly.
  for (const info of gridSessions) {
    const st = deps.ensureTerm(info);
    st.host.classList.add('in-grid');
    st.host.classList.toggle('active', info.id === state.activeId);
    st.host.classList.toggle('attention', state.attention.has(info.id));
    st.ensureAttached();
    termsHost.appendChild(st.host); // re-order to keep DOM == nav order
  }
  // Hide / unmark tiles outside the scope.
  for (const [sid, st] of state.terms) {
    if (!gridIDs.has(sid)) {
      st.host.classList.remove('in-grid', 'active');
      st.host.style.gridRow = '';
      st.host.style.gridColumn = '';
    }
  }

  // Pick (rows, cols) that fills the container, then derive
  // per-tile assignments + the cellMap used by spatial nav. Pure
  // logic lives in lib/grid.js so it can be unit-tested.
  const w = termsHost.clientWidth || 800;
  const h = termsHost.clientHeight || 600;
  const { rows, cols, assignments, cellMap } = buildGridLayout(n, w, h);

  termsHost.style.gridTemplateColumns = `repeat(${cols}, 1fr)`;
  termsHost.style.gridTemplateRows = `repeat(${rows}, 1fr)`;

  // Apply each tile's row span. CSS grid 1-based; row indices are
  // implicit row-major, so we only need to span when rowSpan > 1.
  for (let i = 0; i < n; i++) {
    const a = assignments[i];
    const st = state.terms.get(gridSessions[i].id);
    if (!st) continue;
    if (a.rowSpan > 1) {
      st.host.style.gridRow = `span ${a.rowSpan}`;
    } else {
      st.host.style.gridRow = '';
    }
    st.host.style.gridColumn = '';
  }

  gridLayout = { rows, cols, sessions: gridSessions, assignments, cellMap };

  // Freeze probe: count + time each layout pass. A runaway count (the
  // container ResizeObserver → renderGrid → tile fit → container resize
  // feedback loop) or a single multi-hundred-ms pass points straight at
  // the grid relayout as the stall source. dur is the synchronous cost
  // of this pass; ms is wall-clock so a storm shows as tight spacing.
  if (deps.scrollTrace.rec.enabled) {
    deps.scrollTrace.count('renderGrid');
    deps.scrollTrace.rec('render-grid', {
      n, rows, cols, dur: Math.round(performance.now() - _t0),
    });
  }

  // No explicit refit pass: each tile's ResizeObserver fires when its
  // body box changes (CSS grid cell resized, in-grid class toggled,
  // tile shown/hidden). That's the only place fit.fit() runs.
}

// gridSpatialMove moves the active tile in the given direction.
// Uses cellMap to honor row-spanned tiles: e.g. with 3 sessions in a
// 2x2 grid the bottom-right cell is absorbed by tile 1, so pressing
// "right" from tile 2 lands on tile 1 instead of doing nothing.
export function gridSpatialMove(dCol, dRow) {
  const { sessions } = gridLayout;
  if (sessions.length === 0) return;
  const idx = sessions.findIndex((s) => s.id === state.activeId);
  if (idx < 0) {
    deps.setActive(sessions[0].id);
    renderGrid();
    updateSidebarSelection();
    return;
  }
  const target = computeSpatialMove(gridLayout, idx, dCol, dRow);
  if (target == null) return;
  deps.setActive(sessions[target].id);
  renderGrid();
  updateSidebarSelection();
  setStatus(sessions[target].name);
}

export function shiftActiveProject(delta) {
  if (state.projects.length === 0) return;
  const cur = activeProjectId();
  const i = state.projects.findIndex((p) => p.id === cur);
  if (i < 0) return;
  const next = state.projects[(i + delta + state.projects.length) % state.projects.length];
  state.currentProjectId = next.id;
  if (state.view === 'grid-project') state.gridProjectId = next.id;

  const sessions = state.sessions
    .filter((s) => (s.projectId ?? s.project_id) === next.id)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  if (sessions[0]) {
    deps.setActive(sessions[0].id);
  } else {
    // Empty project — keep the project selected but drop the active
    // session so the user can ⌘N into it. activeProjectId() now
    // returns the empty project because currentProjectId is set.
    state.activeId = null;
  }
  if (state.view === 'single') showSingle(state.activeId);
  else renderGrid();
  updateSidebarSelection();
  setStatus(`${next.name}${sessions.length === 0 ? ' (empty)' : ''}`);
}

// gridScopeSessions returns the list of sessions that should be tiled
// in the current grid view.
export function gridScopeSessions() {
  if (state.view === 'grid-all') {
    return filterMinimized(orderedSessions(), state.minimized);
  }
  if (state.view === 'grid-project') {
    const pid = state.gridProjectId || activeProjectId();
    const scoped = state.sessions
      .filter((s) => (s.projectId ?? s.project_id) === pid)
      .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
    return filterMinimized(scoped, state.minimized);
  }
  return [];
}

// minimizeSession hides a session from grid views by adding its id to
// state.minimized. The session stays alive; its tile is removed on the
// next renderGrid(). Single-session mode is unaffected — the user can
// still switch to a minimized session via the sidebar / palette / ⌘[/].
export function minimizeSession(id) {
  if (!id || state.minimized.has(id)) return;
  state.minimized.add(id);
  // If the active session is the one being minimized while in grid
  // mode, hand focus to the next still-visible session so the focus
  // ring doesn't vanish onto an offscreen tile.
  if (state.activeId === id && state.view !== 'single') {
    const next = gridScopeSessions().find((s) => s.id !== id);
    if (next) deps.setActive(next.id);
  }
  if (state.view !== 'single') {
    renderGrid();
    rebaselineGridReplayCols();
  }
  renderMinimizedTray();
}

// restoreSession removes a session from state.minimized and switches
// to it. Works from any view — switchTo handles the view-aware repaint.
export function restoreSession(id) {
  if (!id) return;
  state.minimized.delete(id);
  renderMinimizedTray();
  switchTo(id);
  if (state.view !== 'single') {
    rebaselineGridReplayCols();
  }
}

// rebaselineGridReplayCols defers a baseline reset to after the next
// two animation frames. The first rAF lets the CSS grid layout settle
// and ResizeObserver fire _onBodyResize on each affected tile (which
// updates this.term.cols via fit.fit() and may arm a 100ms replay
// debounce). The second rAF then snapshots the new term.cols as the
// baseline and clears the pending debounce — turning a layout-driven
// width change into a no-op rather than a spurious scrollback replay.
// Pure user window resizes still flow through the threshold path in
// _onBodyResize and continue to request replays as before.
function rebaselineGridReplayCols() {
  requestAnimationFrame(() => requestAnimationFrame(() => {
    for (const st of state.terms.values()) {
      if (st.host.classList.contains('in-grid')) {
        st.rebaselineReplayCols('layout');
      }
    }
  }));
}

// renderMinimizedTray rebuilds the #minimized-tray chip row from
// state.minimized. Hidden when the set is empty.
export function renderMinimizedTray() {
  const tray = document.getElementById('minimized-tray');
  if (!tray) return;
  tray.innerHTML = '';
  const ids = Array.from(state.minimized);
  if (ids.length === 0) {
    tray.classList.add('hidden');
    return;
  }
  tray.classList.remove('hidden');
  // Preserve display order so the chip row reads top-to-bottom like
  // the sidebar / grid would.
  const ord = orderedSessions().filter((s) => state.minimized.has(s.id));
  for (const info of ord) {
    const chip = document.createElement('button');
    chip.className = 'min-chip';
    chip.type = 'button';
    chip.dataset.sid = info.id;
    chip.title = `Restore ${info.name}`;
    chip.setAttribute('aria-label', `Restore ${info.name}`);
    chip.style.setProperty('--session-color', info.color || '#888');

    const dot = document.createElement('span');
    dot.className = 'min-chip-color';
    chip.append(dot);

    const name = document.createElement('span');
    name.className = 'min-chip-name';
    name.textContent = info.name;
    chip.append(name);

    const pid = info.projectId ?? info.project_id;
    const proj = state.projects.find((p) => p.id === pid);
    if (proj?.name) {
      const projLabel = document.createElement('span');
      projLabel.className = 'min-chip-project';
      projLabel.textContent = proj.name;
      chip.append(projLabel);
    }

    chip.addEventListener('click', (e) => {
      e.stopPropagation();
      restoreSession(info.id);
    });
    tray.append(chip);
  }
  // Minimize/restore changes which sessions are visible without a
  // sidebar render — re-evaluate the empty state here too.
  renderEmptyState();
}

// renderEmptyState shows an actionable hint pane when the current
// scope has nothing to display (first run, empty project, everything
// minimized). Pure model in lib/empty-state.js; this just projects it
// onto the #empty-state element. Cheap enough to call from every
// repaint path — DOM is rebuilt only when the model changes.
export function renderEmptyState() {
  const el = document.getElementById('empty-state');
  if (!el) return;
  const model = emptyStateModel({
    projects: state.projects,
    sessions: state.sessions,
    view: state.view,
    currentProjectId: state.currentProjectId,
    gridProjectId: state.gridProjectId,
    minimized: state.minimized,
    isMac,
  });
  if (!model) {
    el.classList.add('hidden');
    el.dataset.kind = '';
    delete el.dataset.sig;
    return;
  }
  // Key the rebuild off the full model, not just the kind: within
  // 'first-run' the hint/actions vary with projects.length, so a
  // kind-only check would leave stale text and buttons behind.
  const sig = JSON.stringify(model);
  if (el.dataset.sig !== sig) {
    el.dataset.sig = sig;
    el.dataset.kind = model.kind;
    el.innerHTML = '';
    const title = document.createElement('div');
    title.className = 'empty-title';
    title.textContent = model.title;
    const hint = document.createElement('div');
    hint.className = 'empty-hint';
    hint.textContent = model.hint;
    el.append(title, hint);
    if (model.actions.length) {
      const row = document.createElement('div');
      row.className = 'empty-actions';
      for (const a of model.actions) {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.textContent = a.label;
        btn.addEventListener('click', (e) => {
          // The launcher now opens synchronously; without this, the
          // same click bubbles to the document-level outside-click
          // closer and shuts it in the same tick.
          e.stopPropagation();
          if (a.id === 'new-session') openLauncher();
          else if (a.id === 'new-project') openProjectEditor(null);
        });
        row.appendChild(btn);
      }
      el.appendChild(row);
    }
  }
  el.classList.remove('hidden');
}

export function setView(view) {
  state.view = view;
  try { localStorage.setItem(VIEW_STORAGE_KEY, view); } catch {}
  if (view === 'grid-project') {
    state.gridProjectId = activeProjectId();
  }
  if (view === 'single') {
    showSingle(state.activeId);
  } else {
    renderGrid();
  }
  updateSidebarSelection();
  // Toggling grid/fullscreen via the menu blurs the xterm; restore
  // focus so the user can keep typing into the active session.
  deps.focusActiveTerm();
  // Mode switches are deliberate user actions — always snap visible
  // tiles to the bottom. Without this, xterm lands wherever the
  // buffer happened to be (often mid-history), which is jarring.
  //
  // Defer past focusActiveTerm's full focus-retry budget
  // (FOCUS_MAX_RETRIES = 8 frames ≈ 130ms) before snapping. xterm's
  // scrollToBottom() refreshes the renderer, which can fire focusout
  // on the helper textarea — synchronous snap broke focus on Linux,
  // single-rAF snap broke focus on macOS, because each platform's
  // rAF cadence races focusActiveTerm's retry loop differently.
  // A 250ms setTimeout clears the polling window on every platform;
  // a quarter-second pre-snap pause is below the perception threshold
  // for visual settling after a mode change.
  setTimeout(() => {
    if (deps.scrollTrace.rec.enabled) {
      deps.scrollTrace.rec('mode-snap', { view });
    }
    snapVisibleTermsToBottom(state.terms.values());
  }, 250);
  const ord = orderedSessions();
  const active = ord.find((s) => s.id === state.activeId);
  setStatus(`${view}${active ? ' • ' + active.name : ''}`);
  renderEmptyState();
}

// ---------- resize ----------
//
// Per-tile fit is driven by each SessionTerm's own ResizeObserver
// on its body. The only thing left at the page level is re-picking
// (rows, cols) for the grid when the *container* changes shape —
// e.g. landscape ↔ portrait window or sidebar drag — so tiles flow
// from "side-by-side" to "stacked" and back.
//
// rAF coalesces the burst of RO entries during a continuous drag
// into one renderGrid per frame. The guard also dodges the dreaded
// "ResizeObserver loop completed with undelivered notifications"
// warning that fires when a callback synchronously mutates layout.
let _gridReflowQueued = false;
new ResizeObserver(() => {
  // Freeze probe: count every container RO firing (including the ones
  // coalesced away by the queued guard). If this races far ahead of the
  // render-grid count, the container is being resized in a tight loop —
  // the classic ResizeObserver feedback storm.
  if (deps.scrollTrace.rec.enabled) deps.scrollTrace.count('gridContainerResize');
  if (state.view === 'single' || _gridReflowQueued) return;
  _gridReflowQueued = true;
  requestAnimationFrame(() => {
    _gridReflowQueued = false;
    if (state.view !== 'single') renderGrid();
  });
}).observe(termsHost);
