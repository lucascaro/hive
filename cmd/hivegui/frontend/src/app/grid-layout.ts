// ---------- grid layout (imperative DOM half of the grid) ----------
//
// Everything in this file was moved out of view.ts by Phase 5 of the
// React rewrite, deliberately unchanged: the order of operations in
// applyGridLayout() encodes several shipped bug fixes (grid template
// before attach, reparent-never-recreate, the deferred attach stagger),
// and re-expressing it as JSX children would lose all of them.
//
// components/GridView.tsx owns the *when*: it subscribes to the store
// and calls into here from one layout effect. This module owns the
// *what*, and nothing here reads React.
//
// The terminal hosts come from store/terms.ts — the registry deliberately
// outside the reactive store, because a SessionTerm holds an xterm, a
// WebGL slot and a live PTY attachment that must never be recreated.

import { LogFrontend } from '../bridge.js';
import { state, type SessionInfo, type TermTile } from './state.js';
import { termsHost } from './dom.js';
import { orderedSessions, activeProjectId } from './selectors.js';
import { getTerm, termsMap } from '../store/terms.js';
import {
  buildGridLayout,
  computeSpatialMove,
  type GridLayout,
} from '../lib/grid.js';
import type { ViewMode } from '../lib/view.js';
import { filterHidden } from '../lib/minimized.js';
import { preserveFocus } from '../lib/preserve-focus.js';
import { createScrollTrace, type ScrollTrace } from '../lib/scroll-debug.js';

// The same injection seam view.ts has, and for the same reason: this
// module is imported by session-term.ts's dependents, so importing
// ensureTerm from it directly would close a cycle. view.ts's initView()
// forwards its deps here, so a caller (main.ts, a dom test) that wires
// the view wires the grid with it. Both seams go away in Phase 6.
export interface GridDeps {
  ensureTerm: (info: SessionInfo) => TermTile;
  scrollTrace: Pick<ScrollTrace, 'rec' | 'count'>;
}

let deps: GridDeps = {
  ensureTerm: () => undefined as unknown as TermTile,
  scrollTrace: createScrollTrace({ enabled: false }),
};

export function initGridLayout(injected: GridDeps) {
  deps = injected;
}

// applySingle hides every tile but the active one. Was showSingle().
export function applySingle(id: string | null) {
  termsHost.classList.add('single');
  termsHost.classList.remove('grid');
  // Hide everything except the active tile.
  for (const [sid, st] of termsMap()) {
    if (sid === id) st.show();
    else st.hide();
    st.host.classList.remove('in-grid', 'active');
  }
  const st = id ? getTerm(id) : null;
  if (st) st.ensureAttached();
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

// Read-only view of the cache for keyboard navigation (view.ts's
// gridSpatialMove). Exported instead of the binding itself so a caller
// cannot leave a stale layout behind.
export function currentGridLayout(): GridLayout & { sessions: SessionInfo[] } {
  return gridLayout;
}

// spatialTarget resolves a directional move against the cached layout.
// Kept next to the cache so the two can't drift.
export function spatialTarget(
  idx: number,
  dCol: number,
  dRow: number,
): number | null {
  return computeSpatialMove(gridLayout, idx, dCol, dRow);
}

// attachDeferred attaches non-active grid tiles one per idle callback so
// the first paint isn't blocked by N synchronous fit()+replay passes.
// ensureAttached is idempotent for the ATTACH itself (it returns early once
// attached), so re-running the layout — which re-queues everything — never
// re-opens a session. It is NOT side-effect-free though: ensureAttached
// re-latches follow-intent and snaps to the bottom on every call, so each
// layout pass (switch, minimize, container resize, …) re-anchors every
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

// applyGridLayout lays out every tile that should be visible in the
// current grid scope. Tiles for other sessions are hidden but kept
// alive (so their xterm scrollback persists across mode switches).
// Was renderGrid().
export function applyGridLayout() {
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
    const existed = termsMap().has(info.id);
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
  // focus.ts's _focusGuard exists to paper over). A layout pass runs on every
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
  // Only log when the pass built new tiles — a layout pass runs on every
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
  for (const [sid, st] of termsMap()) {
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
    const st = getTerm(gridSessions[i].id);
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
  // container ResizeObserver → layout → tile fit → container resize
  // feedback loop) or a single multi-hundred-ms pass points straight at
  // the grid relayout as the stall source. dur is the synchronous cost
  // of this pass; ms is wall-clock so a storm shows as tight spacing.
  // The `renderGrid` / `render-grid` keys keep the pre-Phase-5 names on
  // purpose: they are what a freeze-probe dump is grepped for, and
  // renaming them would make new traces incomparable with every archived
  // one. Same reason the LogFrontend line above still says "renderGrid".
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

// rebaselineGridReplayCols defers a baseline reset to after the next
// two animation frames. The first rAF lets the CSS grid layout settle
// and ResizeObserver fire _onBodyResize on each affected tile (which
// updates this.term.cols via fit.fit() and may arm a 100ms replay
// debounce). The second rAF then snapshots the new term.cols as the
// baseline and clears the pending debounce — turning a layout-driven
// width change into a no-op rather than a spurious scrollback replay.
// Pure user window resizes still flow through the threshold path in
// _onBodyResize and continue to request replays as before.
export function rebaselineGridReplayCols() {
  requestAnimationFrame(() =>
    requestAnimationFrame(() => {
      for (const st of termsMap().values()) {
        if (st.host.classList.contains('in-grid')) {
          st.rebaselineReplayCols('layout');
        }
      }
    }),
  );
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
// into one layout pass per frame. The guard also dodges the dreaded
// "ResizeObserver loop completed with undelivered notifications"
// warning that fires when a callback synchronously mutates layout.
//
// This one stays outside React: a container resize changes no store
// field, so there is nothing for GridView to subscribe to, and routing
// it through a store bump would only add a render to a path that is
// already coalesced to one pass per frame.
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
    if (state.view !== 'single') applyGridLayout();
  });
}).observe(termsHost);
