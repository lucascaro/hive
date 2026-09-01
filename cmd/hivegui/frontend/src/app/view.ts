// ---------- view / grid / tray / empty state ----------
//
// Moved verbatim from main.js. ensureTerm / setActive /
// focusActiveTerm and the scroll tracer are injected via
// initView(deps) — they live in session-term/focus modules (stage 6)
// and main.ts.

import { WindowSetTitle, LogFrontend } from '../bridge.js';
import { state, type SessionInfo, type TermTile } from './state.js';
import * as store from '../store/store.js';
import { termsHost, setStatus, flashStatus, setModeHint } from './dom.js';
import { orderedSessions, activeProjectId } from './selectors.js';
import { updateSidebarSelection, renderSidebar } from './sidebar.js';
import { openLauncher } from './modals/launcher.js';
import { openProjectEditor } from './modals/project-editor.js';
import {
  buildGridLayout,
  computeSpatialMove,
  type GridLayout,
} from '../lib/grid.js';
import { resolveView, type ViewMode } from '../lib/view.js';
import { filterHidden } from '../lib/minimized.js';
import { snapVisibleTermsToBottom } from '../lib/view-scroll.js';
import { emptyStateModel } from '../lib/empty-state.js';
import { readProjectId } from '../lib/wire.js';
import { isMac } from '../lib/platform.js';
import { modeHints } from '../lib/status.js';
import { button } from '../ui/button.js';
import { createScrollTrace, type ScrollTrace } from '../lib/scroll-debug.js';
import { chip } from '../ui/chip.js';
import { sessionState } from '../lib/session-state.js';
import { preserveFocus } from '../lib/preserve-focus.js';

// Per-module deps (view wants focusActiveTerm where sidebar wants
// refocusActiveTerm). Exported so wave 7 can check main.ts's injection.
export interface ViewDeps {
  ensureTerm: (info: SessionInfo) => TermTile;
  setActive: (id: string | null) => void;
  focusActiveTerm: () => void;
  // Only the two members this module reaches for, not the whole
  // ScrollTrace: the real trace.ts export satisfies it, and so do the
  // test stubs, which carry no ring/counters. Requiring more than the
  // consumer uses is the same over-tightening 5a rejected for the modals.
  scrollTrace: Pick<ScrollTrace, 'rec' | 'count'>;
}

let deps: ViewDeps = {
  // Pre-initView stub. There is no TermTile to hand back, and the cast
  // is confined to this default: every real path goes through
  // initView(). Kept as a no-op rather than a throw because switchTo()
  // discards the return value, so an uninjected call is a no-op today —
  // renderGrid's `st.host` is what actually fails, as it already does.
  ensureTerm: () => undefined as unknown as TermTile,
  setActive: () => {},
  focusActiveTerm: () => {},
  // A real disabled tracer instead of a hand-rolled `{ rec }` literal:
  // that literal was already missing `.count()`, which two call sites
  // below use. enabled:false short-circuits inside both rec and count,
  // so the no-op behavior is unchanged and the stub can't drift again.
  scrollTrace: createScrollTrace({ enabled: false }),
};

export function initView(injected: ViewDeps) {
  deps = injected;
}

export function showSingle(id: string | null) {
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

export function switchTo(id: string | null) {
  if (id === state.activeId && state.view === 'single') {
    deps.focusActiveTerm();
    return;
  }
  deps.setActive(id);
  let info: SessionInfo | null | undefined = null;
  if (id) {
    info = state.sessions.find((s) => s.id === id);
    if (info) deps.ensureTerm(info);
  }
  // Retarget the grid scope if the new session belongs to a different
  // project than the one currently shown in grid-project mode.
  if (state.view === 'grid-project' && info) {
    const pid = info.projectId ?? info.project_id;
    if (pid && pid !== state.gridProjectId) store.setGridProjectId(pid);
  }
  // Before painting: a grid view has no tile for a hidden session, so
  // drop to single first rather than rendering a grid the selection
  // isn't in. Every "make this session active" path lands here —
  // sidebar click, ⌘1–⌘9, the menu, switchToProject — so the guard
  // belongs here and not at each caller.
  fallBackToSingleIfActiveHidden();
  if (state.view === 'single') showSingle(id);
  else renderGrid();
  updateSidebarSelection();
  setStatus(info ? (info.name ?? '') : '');
  // fallBackToSingleIfActiveHidden above can drop us out of a grid, so
  // the hint is recomputed here too — a "focus / move" hint on a single
  // pane is exactly the lying hint AGENTS.md forbids.
  setModeHint(modeHints(state.view, isMac));
  updateAppTitle();
  // setActive() called focusActiveTerm() before ensureTerm() existed
  // for a brand-new session — re-focus now that the SessionTerm is
  // created and visible. Without this, typing after creating a
  // session lands in whichever terminal had focus before.
  if (id) deps.focusActiveTerm();
  // Focusing a pane is a deliberate "show me the latest". ensureAttached
  // (via showSingle/renderGrid above) already re-latched _followBottom;
  // this makes the move visible immediately for an already-attached tile
  // whose attach replay won't re-fire. Skips detached/zero-height terms,
  // so a still-deferring tile is a no-op here (Change A catches it on attach).
  if (id) {
    const st = state.terms.get(id);
    if (st) snapVisibleTermsToBottom([st]);
  }
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
let _appTitleTimer: number | null = null;
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
    try {
      WindowSetTitle(title);
    } catch (_) {
      /* runtime not ready */
    }
  }, 100);
}

// switchToProject activates a project: in grid-project mode it
// retargets the grid, and in any mode it makes the project's first
// session the active one. Empty projects are still selectable —
// currentProjectId is set so ⌘N targets them correctly.
export function switchToProject(pid: string) {
  if (!pid) return;
  store.setCurrentProjectId(pid);
  if (state.view === 'grid-project') store.setGridProjectId(pid);
  const sessions = state.sessions
    .filter((s) => (s.projectId ?? s.project_id) === pid)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  const target = firstVisible(sessions);
  if (target) {
    switchTo(target.id);
  } else {
    store.setActiveId(null);
    if (state.view === 'single') showSingle(null);
    else renderGrid();
    updateSidebarSelection();
  }
}

// firstVisible picks the session a project should activate: the first
// one that still has a tile in the current view, falling back to the
// first of all when every one of them is hidden. Landing on a visible
// sibling is what keeps a project with one individually-minimized
// session from tearing the user out of grid mode.
function firstVisible(sessions: SessionInfo[]): SessionInfo | undefined {
  return sessions.find((s) => !isSessionHidden(s.id)) ?? sessions[0];
}

// fallBackToSingleIfActiveHidden drops out of a grid view when the
// session that was just made active has no tile in it. Selecting a
// minimized project (chip click) deliberately does NOT
// un-minimize it — but the grid filters its sessions out, so in a grid
// view the selection would move with nothing appearing and
// focusActiveTerm would hand keystrokes to an invisible terminal. That
// is the same failure navGo's isSessionHidden branch exists to prevent;
// single mode ignores the filter, so falling back to it is the fix that
// keeps "select without restoring" working.
function fallBackToSingleIfActiveHidden() {
  if (state.view === 'single') return;
  const id = state.activeId;
  if (!id || !isSessionHidden(id)) return;
  // persist: false — this is a forced fallback, not a preference. The
  // user's saved grid mode has to survive a detour through a minimized
  // session, or one chip click silently rewrites it.
  setView('single', { persist: false });
}

// gridLayout caches the (rows, cols) chosen for the current scope plus
// the per-tile placement so the keyboard navigation logic doesn't have
// to recompute. assignments[i] = { row, col, rowSpan } — tiles above
// last-row empty cells extend downward to fill the grid (matches
// current Hive's behavior). cellMap[row*cols + col] = session index.
let gridLayout: GridLayout & { sessions: SessionInfo[] } = {
  rows: 1,
  cols: 1,
  sessions: [],
  assignments: [],
  cellMap: [],
};

// attachDeferred attaches non-active grid tiles one per idle callback so
// the first paint isn't blocked by N synchronous fit()+replay passes.
// ensureAttached is idempotent for the ATTACH itself (it returns early once
// attached), so re-running renderGrid — which re-queues everything — never
// re-opens a session. It is NOT side-effect-free though: ensureAttached
// re-latches follow-intent and snaps to the bottom on every call, so each
// renderGrid pass (switch, minimize, container resize, …) re-anchors every
// grid tile to the newest output. That is the intended "grid always shows
// the latest" behavior — the cost is that a background tile can't be parked
// scrolled up in history across a relayout.
// requestIdleCallback isn't available in all webviews; fall back to a
// short-timeout chain so the stagger still happens.
const _ric = (cb: () => void) =>
  typeof requestIdleCallback === 'function'
    ? requestIdleCallback(cb, { timeout: 500 })
    : setTimeout(cb, 16);
function attachDeferred(terms: TermTile[]) {
  let i = 0;
  const step = () => {
    if (i >= terms.length) return;
    const st = terms[i++];
    // Skip tiles that left the grid before their turn came up.
    if (st.host.classList.contains('in-grid')) st.ensureAttached();
    if (i < terms.length) _ric(step);
  };
  if (terms.length) _ric(step);
}

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

  // Pick (rows, cols) that fills the container and apply the grid template
  // BEFORE the attach loop. The active tile's ensureAttached() runs a
  // synchronous fit.fit() that measures its body box — if the template
  // isn't set yet, it measures the pre-grid (single/stale) width and
  // rebaselineReplayCols('first-attach') anchors the replay baseline to
  // that wrong width. The tile's ResizeObserver then fires once the
  // template lays it out, sees a >=REPLAY_COL_THRESHOLD delta vs the stale
  // baseline, and arms a SECOND scrollback replay on top of the attach
  // replay — the double-restream that visibly jumps the active tile on
  // grid entry. buildGridLayout only reads container dims + n, so it has
  // no dependency on the attach loop. (Per-tile rowSpan is applied below,
  // after ensureTerm creates the terms — it only affects row height, not
  // the cols-driven replay trigger.)
  const w = termsHost.clientWidth || 800;
  const h = termsHost.clientHeight || 600;
  const { rows, cols, assignments, cellMap } = buildGridLayout(n, w, h);
  termsHost.style.gridTemplateColumns = `repeat(${cols}, 1fr)`;
  termsHost.style.gridTemplateRows = `repeat(${rows}, 1fr)`;

  // Startup-fan-out probe: how many tiles this pass builds+attaches and
  // the synchronous cost of the loop. grid-all attaches every session at
  // once; ensureTerm builds a new xterm + WebGL addon and ensureAttached
  // runs a synchronous fit() per tile — the suspected slow-startup stall.
  // (ensureAttached's await is fire-and-forget here, so this captures the
  // synchronous DOM/construction cost; the per-tile open latency lands in
  // the "ensureAttached" feLog lines.)
  const _fanoutStart = (() => {
    try {
      return performance.now();
    } catch {
      return 0;
    }
  })();
  let _built = 0;

  // Ensure every grid session has a SessionTerm; attach lazily. Every
  // grid tile is on-screen (the grid fits all N into the viewport), but
  // attaching all N synchronously runs N fit()s + kicks off N replays in
  // one main-thread pass — the startup drag. Attach the ACTIVE tile now
  // so the user's focus is live immediately; defer the rest to idle
  // callbacks so they stream in without blocking the first paint.
  // Move tiles into the desired DOM order (row-major) so that flexbox
  // / CSS grid honors the navigation order without us having to set
  // grid-row/column explicitly.
  const _deferred: TermTile[] = [];
  const _wanted: HTMLElement[] = [];
  for (const info of gridSessions) {
    const existed = state.terms.has(info.id);
    const st = deps.ensureTerm(info);
    if (!existed) _built += 1;
    st.host.classList.add('in-grid');
    st.host.classList.toggle('active', info.id === state.activeId);
    st.host.classList.toggle('attention', state.attention.has(info.id));
    if (info.id === state.activeId) st.ensureAttached();
    else _deferred.push(st);
    _wanted.push(st.host);
  }
  // Re-order to keep DOM == nav order — but ONLY when the order actually
  // moved. appendChild on an already-attached node is a remove+insert, and
  // the browser blurs whatever is focused inside it (the very blur
  // focus.ts's _focusGuard exists to paper over). renderGrid runs on every
  // repaint, and most of them change no order at all — killing a non-active
  // session leaves the survivors exactly where they were — so an
  // unconditional re-parent dropped keyboard focus for nothing.
  const _domOrder = Array.from(termsHost.children).filter((c) =>
    _wanted.includes(c as HTMLElement),
  );
  if (
    _domOrder.length !== _wanted.length ||
    _wanted.some((h, i) => _domOrder[i] !== h)
  )
    // A genuine re-order still moves nodes, so it still blurs. Put focus
    // back on the same element afterwards — it survived, it just moved.
    preserveFocus(termsHost, () => {
      for (const host of _wanted) termsHost.appendChild(host);
    });
  attachDeferred(_deferred);
  // Only log when the pass built new tiles — renderGrid runs on every
  // repaint (switch, minimize, resize, …), and an unconditional line
  // would spam the log with built=0 sync=0ms noise on every grid touch.
  if (_built > 0) {
    try {
      const _ms = (() => {
        try {
          return Math.round(performance.now() - _fanoutStart);
        } catch {
          return -1;
        }
      })();
      LogFrontend(
        `renderGrid fanout tiles=${n} built=${_built} sync=${_ms}ms view=${state.view}`,
      );
    } catch {
      /* bridge absent in tests */
    }
  }
  // Hide / unmark tiles outside the scope.
  for (const [sid, st] of state.terms) {
    if (!gridIDs.has(sid)) {
      st.host.classList.remove('in-grid', 'active');
      st.host.style.gridRow = '';
      st.host.style.gridColumn = '';
    }
  }

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
      n,
      rows,
      cols,
      dur: Math.round(performance.now() - _t0),
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
export function gridSpatialMove(dCol: number, dRow: number) {
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
  setStatus(sessions[target].name ?? '');
}

export function shiftActiveProject(delta: number) {
  if (state.projects.length === 0) return;
  const cur = activeProjectId();
  const i = state.projects.findIndex((p) => p.id === cur);
  if (i < 0) return;
  // Step over minimized projects: a project you put in the tray is out
  // of the keyboard rotation entirely (amends #250, which listed ⌘[/]
  // among the ways a minimized project stays reachable — the sidebar
  // chip, the sidebar and ⌘K are). Nothing visible to move to → stay.
  const m = state.projects.length;
  const step = Math.sign(delta) || 1;
  let next = null as (typeof state.projects)[number] | null;
  for (let k = 1; k < m && !next; k++) {
    const cand = state.projects[(((i + step * k) % m) + m) % m];
    if (!state.minimizedProjects.has(cand.id)) next = cand;
  }
  if (!next) return;
  store.setCurrentProjectId(next.id);
  if (state.view === 'grid-project') store.setGridProjectId(next.id);

  const sessions = state.sessions
    .filter((s) => (s.projectId ?? s.project_id) === next.id)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  const target = firstVisible(sessions);
  if (target) {
    // ensureTerm before setActive: showSingle only shows a tile that
    // already exists, and this path never went through switchTo. A
    // session that has not been rendered this run — the normal case
    // after a restart, and now reachable whenever the grid falls back
    // to single — would otherwise leave a blank, unfocusable pane.
    deps.ensureTerm(target);
    deps.setActive(target.id);
  } else {
    // Empty project — keep the project selected but drop the active
    // session so the user can ⌘N into it. activeProjectId() now
    // returns the empty project because currentProjectId is set.
    store.setActiveId(null);
  }
  if (state.view === 'single') showSingle(state.activeId);
  else renderGrid();
  // Same guard as switchToProject: ⌘[ / ⌘] can land on a project whose
  // sessions the grid filters out.
  fallBackToSingleIfActiveHidden();
  updateSidebarSelection();
  setStatus(`${next.name}${sessions.length === 0 ? ' (empty)' : ''}`);
}

// gridScopeFor returns the sessions a given grid view would tile, for a
// view the app is not necessarily in yet — setView needs the count
// before it commits to the mode.
export function gridScopeFor(view: ViewMode, projectId?: string) {
  if (view === 'grid-all') {
    return filterHidden(
      orderedSessions(),
      state.minimized,
      state.minimizedProjects,
    );
  }
  if (view === 'grid-project') {
    const scoped = state.sessions
      .filter((s) => (s.projectId ?? s.project_id) === projectId)
      .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
    return filterHidden(scoped, state.minimized, state.minimizedProjects);
  }
  return [];
}

// gridScopeSessions returns the list of sessions that should be tiled
// in the current grid view.
export function gridScopeSessions() {
  return gridScopeFor(state.view, state.gridProjectId || activeProjectId());
}

// isSessionHidden answers the one question every "can I switch to this
// with a tile to land on?" caller asks: the session is out of the grid
// either because it was minimized itself, or because its project was.
// keyboard.ts branches on this rather than on state.minimized directly,
// so the two mechanisms can never drift apart.
export function isSessionHidden(id: string): boolean {
  if (state.minimized.has(id)) return true;
  const s = state.sessions.find((x) => x.id === id);
  return !!s && state.minimizedProjects.has(readProjectId(s));
}

// hiddenSessionIds is the union the empty-state model needs: it asks
// "is every session in scope hidden?", which must count both kinds.
function hiddenSessionIds(): Set<string> {
  const ids = new Set(state.minimized);
  for (const s of state.sessions) {
    if (state.minimizedProjects.has(readProjectId(s))) ids.add(s.id);
  }
  return ids;
}

// minimizeSession hides a session from grid views by adding its id to
// state.minimized. The session stays alive; its tile is removed on the
// next renderGrid(). Single-session mode is unaffected — the user can
// still switch to a minimized session via the sidebar / palette.
export function minimizeSession(id: string | null) {
  if (!id || state.minimized.has(id)) return;
  store.minimizeSession(id);
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
  // The sidebar row carries the same toggle, so it has to learn the
  // new state — renderMinimizedTray only rebuilds the chip row.
  renderSidebar();
  enforceViewFloor();
}

// enforceViewFloor drops back to focused mode when the current grid's
// scope has fallen below two tiles (last sibling killed, or minimized
// away) — the same degenerate one-tile grid setView refuses to enter.
export function enforceViewFloor() {
  if (state.view === 'single') return;
  if (gridScopeSessions().length >= 2) return;
  setView('single');
}

// restoreSession removes a session from state.minimized and switches
// to it. Works from any view — switchTo handles the view-aware repaint.
export function restoreSession(id: string | null) {
  if (!id) return;
  store.restoreSession(id);
  // A session can be hidden by its project rather than by itself, and
  // callers (⌘B, nav history) only know "make this one visible" — so
  // reveal whichever of the two is holding it back.
  const s = state.sessions.find((x) => x.id === id);
  const pid = readProjectId(s);
  if (pid && state.minimizedProjects.has(pid)) restoreProject(pid);
  renderMinimizedTray();
  renderSidebar();
  switchTo(id);
  if (state.view !== 'single') {
    rebaselineGridReplayCols();
  }
}

// minimizeProject takes a whole project out of the sidebar list and
// out of grid views in one move: the chip tray at the bottom of the
// sidebar becomes its only row. Its sessions keep running and stay
// reachable (sidebar chip, ⌘K) — this is the project-level
// twin of minimizeSession, and it repaints on the same three axes:
// focus handoff, grid, view floor.
export function minimizeProject(id: string | null) {
  if (!id || state.minimizedProjects.has(id)) return;
  store.minimizeProject(id);
  // Same reason as minimizeSession: don't leave the focus ring on a
  // tile that just stopped being rendered.
  if (state.view !== 'single') {
    const active = state.sessions.find((x) => x.id === state.activeId);
    if (active && readProjectId(active) === id) {
      const next = gridScopeSessions()[0];
      if (next) deps.setActive(next.id);
    }
    renderGrid();
    rebaselineGridReplayCols();
  }
  renderSidebar();
  enforceViewFloor();
}

// restoreProject puts the project back in the sidebar list. There is
// no stored position to restore: minimizing never touches the
// project's Order, so the row reappears exactly where it was.
export function restoreProject(id: string | null) {
  if (!id || !state.minimizedProjects.has(id)) return;
  store.restoreProject(id);
  renderSidebar();
  if (state.view !== 'single') {
    renderGrid();
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
  requestAnimationFrame(() =>
    requestAnimationFrame(() => {
      for (const st of state.terms.values()) {
        if (st.host.classList.contains('in-grid')) {
          st.rebaselineReplayCols('layout');
        }
      }
    }),
  );
}

// renderMinimizedTray rebuilds the #minimized-tray chip row from
// state.minimized. Hidden when the set is empty.
export function renderMinimizedTray() {
  const tray = document.getElementById('minimized-tray');
  if (!tray) return;
  tray.innerHTML = '';
  if (state.minimized.size === 0) {
    tray.classList.add('hidden');
    renderEmptyState();
    return;
  }
  tray.classList.remove('hidden');
  // Display order, so the chip row reads left-to-right like the sidebar
  // reads top-to-bottom.
  for (const info of orderedSessions().filter((s) =>
    state.minimized.has(s.id),
  )) {
    const proj = state.projects.find((p) => p.id === readProjectId(info));
    const el = chip({
      label: info.name ?? '',
      sublabel: proj?.name,
      color: info.color,
      state: sessionState(info, state.attention.has(info.id)),
      ariaLabel: `Restore ${info.name}`,
      onClick: () => restoreSession(info.id),
    });
    el.dataset.sid = info.id;
    tray.append(el);
  }
  // Minimize/restore changes which sessions are visible without a sidebar
  // render — re-evaluate the empty state here too.
  renderEmptyState();
}

// renderEmptyState shows an actionable hint pane when the current
// scope has nothing to display (first run, empty project, everything
// minimized). Pure model in lib/empty-state.ts; this just projects it
// onto the #empty-state element. Cheap enough to call from every
// repaint path — DOM is rebuilt only when the model changes.
export function renderEmptyState() {
  // getElementById + a guard, deliberately not el.ts's mustEl/pageEl:
  // this runs on every repaint, not at load, so absence is a tolerated
  // branch (keyboard.ts imports this module into DOM tests that mount
  // only the markup they exercise) rather than a contract to assert.
  const el = document.getElementById('empty-state');
  if (!el) return;
  const model = emptyStateModel({
    projects: state.projects,
    sessions: state.sessions,
    view: state.view,
    // `?? undefined`, not a widening of EmptyStateInput to `| null`: its
    // `= ''` parameter defaults fire on undefined only, so accepting null
    // there would change which default applies.
    currentProjectId: state.currentProjectId ?? undefined,
    gridProjectId: state.gridProjectId ?? undefined,
    minimized: hiddenSessionIds(),
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
      // patterns.md: one primary action, the rest default.
      model.actions.forEach((a, i) => {
        row.appendChild(
          button({
            label: a.label,
            kind: i === 0 ? 'primary' : 'default',
            icon: 'plus',
            onClick: (e) => {
              // The launcher now opens synchronously; without this, the
              // same click bubbles to the document-level outside-click
              // closer and shuts it in the same tick.
              e.stopPropagation();
              if (a.id === 'new-session') openLauncher();
              else if (a.id === 'new-project') openProjectEditor(null);
            },
          }),
        );
      });
      el.appendChild(row);
    }
  }
  el.classList.remove('hidden');
}

export function setView(view: ViewMode, opts: { persist?: boolean } = {}) {
  // A grid of one tile looks like focused mode but loses the focused-mode
  // keybindings, so ⌘G / ⇧⌘G below the floor stay where they are.
  // The startup restore of a persisted view goes through here too.
  const target = resolveView(
    view,
    view === 'single' ? 0 : gridScopeFor(view, activeProjectId()).length,
  );
  if (target !== view) flashStatus('only one session — staying focused');
  view = target;
  store.setView(view, opts.persist !== false);
  if (view === 'grid-project') {
    store.setGridProjectId(activeProjectId());
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
  // The left slot names the session; the mode moved to the right slot,
  // where it is spelled as the shortcut that leaves it.
  setStatus(active ? (active.name ?? '') : '');
  setModeHint(modeHints(view, isMac));
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
  if (deps.scrollTrace.rec.enabled)
    deps.scrollTrace.count('gridContainerResize');
  if (state.view === 'single' || _gridReflowQueued) return;
  _gridReflowQueued = true;
  requestAnimationFrame(() => {
    _gridReflowQueued = false;
    if (state.view !== 'single') renderGrid();
  });
}).observe(termsHost);
