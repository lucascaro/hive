// ---------- view commands ----------
//
// The verbs: switch to a session or project, change view mode, minimize
// and restore, move the selection around the grid. What each of them
// used to do LAST — call renderGrid() or showSingle() — is gone: they
// write the store, and components/GridView.tsx repaints from a layout
// effect. The layout code itself moved verbatim to app/grid-layout.ts in
// Phase 5 of the React rewrite.
//
// ensureTerm / setActive / focusActiveTerm and the scroll tracer are
// still injected via initView(deps) — they live in session-term/focus
// modules and main.ts. Phase 6 deletes the seam with this file.

import { flushSync } from 'react-dom';
import { WindowSetTitle } from '../bridge.js';
import { state, type SessionInfo, type TermTile } from './state.js';
import * as store from '../store/store.js';
import { setStatus, flashStatus, setModeHint } from './dom.js';
import { orderedSessions, activeProjectId } from './selectors.js';
import {
  currentGridLayout,
  gridScopeFor,
  gridScopeSessions,
  initGridLayout,
  rebaselineGridReplayCols,
  spatialTarget,
} from './grid-layout.js';
import { resolveView, type ViewMode } from '../lib/view.js';
import { snapVisibleTermsToBottom } from '../lib/view-scroll.js';
import { readProjectId } from '../lib/wire.js';
import { isMac } from '../lib/platform.js';
import { modeHints } from '../lib/status.js';
import { createScrollTrace, type ScrollTrace } from '../lib/scroll-debug.js';

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
  // the grid layout's `st.host` is what actually fails, as it already does.
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
  // One wiring call for both halves of the view: grid-layout.ts needs
  // the same ensureTerm and tracer, and importing them there directly
  // would close a session-term ↔ grid-layout cycle. Phase 6 deletes both
  // seams together.
  initGridLayout(injected);
}

// withLayout runs the store writes of a command and flushes the React
// work they queue, so GridView's layout effect has already repainted by
// the time the caller reaches its post-layout work (focusActiveTerm,
// snapVisibleTermsToBottom). Without the flush those updates would land
// in a microtask AFTER the caller returned, and the snap would measure a
// tile the grid had not laid out yet — the ordering invariants 3 and 4
// pin. Same pattern the modals adopted in Phases 3-4 for plain listeners.
//
// The depth guard is for the commands that call each other (switchTo →
// fallBackToSingleIfActiveHidden → setView, switchToProject → switchTo):
// nested writes ride the outer flush instead of opening a second one,
// which React does not allow mid-flush. Focus work inside a nested call
// therefore runs before the repaint — harmless, because focusActiveTerm
// retries for 8 frames and the outer command re-focuses at its end.
let _flushDepth = 0;
function withLayout<T>(fn: () => T): T {
  if (_flushDepth > 0) return fn();
  _flushDepth++;
  try {
    return flushSync(fn);
  } finally {
    _flushDepth--;
  }
}

export function switchTo(id: string | null) {
  if (id === state.activeId && state.view === 'single') {
    deps.focusActiveTerm();
    return;
  }
  const info = withLayout(() => {
    deps.setActive(id);
    let found: SessionInfo | undefined;
    if (id) {
      found = state.sessions.find((s) => s.id === id);
      if (found) deps.ensureTerm(found);
    }
    // Retarget the grid scope if the new session belongs to a different
    // project than the one currently shown in grid-project mode.
    if (state.view === 'grid-project' && found) {
      const pid = found.projectId ?? found.project_id;
      if (pid && pid !== state.gridProjectId) store.setGridProjectId(pid);
    }
    // Before painting: a grid view has no tile for a hidden session, so
    // drop to single first rather than rendering a grid the selection
    // isn't in. Every "make this session active" path lands here —
    // sidebar click, ⌘1–⌘9, the menu, switchToProject — so the guard
    // belongs here and not at each caller.
    fallBackToSingleIfActiveHidden();
    return found;
  });
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
  // (via the layout pass above) already re-latched _followBottom;
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
    withLayout(() => store.setActiveId(null));
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

// isSessionHidden answers the one question every "can I switch to this
// with a tile to land on?" caller asks: the session is out of the grid
// either because it was minimized itself, or because its project was.
// keyboard.ts branches on this rather than on state.minimized directly,
// so the two mechanisms can never drift apart. It stays here rather than
// moving to grid-layout.ts with the scope helpers: it reads nothing but
// the store, and keyboard.ts is the heaviest caller.
export function isSessionHidden(id: string): boolean {
  if (state.minimized.has(id)) return true;
  const s = state.sessions.find((x) => x.id === id);
  return !!s && state.minimizedProjects.has(readProjectId(s));
}

// gridSpatialMove moves the active tile in the given direction.
// Uses the cached layout's cellMap to honor row-spanned tiles: e.g. with
// 3 sessions in a 2x2 grid the bottom-right cell is absorbed by tile 1,
// so pressing "right" from tile 2 lands on tile 1 instead of doing nothing.
export function gridSpatialMove(dCol: number, dRow: number) {
  const { sessions } = currentGridLayout();
  if (sessions.length === 0) return;
  const idx = sessions.findIndex((s) => s.id === state.activeId);
  if (idx < 0) {
    withLayout(() => deps.setActive(sessions[0].id));
    return;
  }
  const target = spatialTarget(idx, dCol, dRow);
  if (target == null) return;
  withLayout(() => deps.setActive(sessions[target].id));
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
  const chosen = next;
  const sessions = state.sessions
    .filter((s) => (s.projectId ?? s.project_id) === chosen.id)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  const target = firstVisible(sessions);
  withLayout(() => {
    store.setCurrentProjectId(chosen.id);
    if (state.view === 'grid-project') store.setGridProjectId(chosen.id);
    if (target) {
      // ensureTerm before setActive: the single-mode layout only shows a
      // tile that already exists, and this path never went through
      // switchTo. A session that has not been rendered this run — the
      // normal case after a restart, and now reachable whenever the grid
      // falls back to single — would otherwise leave a blank, unfocusable
      // pane.
      deps.ensureTerm(target);
      deps.setActive(target.id);
    } else {
      // Empty project — keep the project selected but drop the active
      // session so the user can ⌘N into it. activeProjectId() now
      // returns the empty project because currentProjectId is set.
      store.setActiveId(null);
    }
    // Same guard as switchToProject: ⌘[ / ⌘] can land on a project whose
    // sessions the grid filters out.
    fallBackToSingleIfActiveHidden();
  });
  setStatus(`${chosen.name}${sessions.length === 0 ? ' (empty)' : ''}`);
}

// minimizeSession hides a session from grid views by adding its id to
// state.minimized. The session stays alive; its tile leaves on the
// repaint that store write triggers. Single-session mode is unaffected —
// the user can still switch to a minimized session via the sidebar / palette.
export function minimizeSession(id: string | null) {
  if (!id || state.minimized.has(id)) return;
  const wasGrid = state.view !== 'single';
  withLayout(() => {
    store.minimizeSession(id);
    // If the active session is the one being minimized while in grid
    // mode, hand focus to the next still-visible session so the focus
    // ring doesn't vanish onto an offscreen tile.
    if (state.activeId === id && state.view !== 'single') {
      const next = gridScopeSessions().find((s) => s.id !== id);
      if (next) deps.setActive(next.id);
    }
  });
  if (wasGrid) rebaselineGridReplayCols();
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
  const wasGrid = state.view !== 'single';
  withLayout(() => {
    store.minimizeProject(id);
    // Same reason as minimizeSession: don't leave the focus ring on a
    // tile that just stopped being rendered.
    if (state.view !== 'single') {
      const active = state.sessions.find((x) => x.id === state.activeId);
      if (active && readProjectId(active) === id) {
        const next = gridScopeSessions()[0];
        if (next) deps.setActive(next.id);
      }
    }
  });
  if (wasGrid) rebaselineGridReplayCols();
  enforceViewFloor();
}

// restoreProject puts the project back in the sidebar list. There is
// no stored position to restore: minimizing never touches the
// project's Order, so the row reappears exactly where it was.
export function restoreProject(id: string | null) {
  if (!id || !state.minimizedProjects.has(id)) return;
  withLayout(() => store.restoreProject(id));
  if (state.view !== 'single') {
    rebaselineGridReplayCols();
  }
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
  withLayout(() => {
    store.setView(view, opts.persist !== false);
    if (view === 'grid-project') {
      store.setGridProjectId(activeProjectId());
    }
  });
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
}
