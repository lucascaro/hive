// ---------- app state: types + the compat view ----------
//
// This file used to own the app's data as one mutable object. The data
// now lives in store/store.ts (zustand) and store/terms.ts (the
// terminal registry); what remains here is the type surface every
// module imports, plus a `state` facade that reads and writes through
// to them.
//
// The facade is migration scaffolding, deleted in Phase 6 of the React
// rewrite. New code should import the store actions directly.

import { appStore as store } from '../store/store.js';
import * as actions from '../store/store.js';
import { termsMap } from '../store/terms.js';
import type { NavHistory } from '../lib/nav-history.js';
import type { ViewMode } from '../lib/view.js';
import type { ReplayFlags, ReplayXterm } from '../lib/scrollback.js';
import type { xtermTheme } from '../theme/theme.js';

export interface SessionInfo {
  id: string;
  name?: string;
  agent?: string;
  color?: string;
  order?: number;
  alive?: boolean;
  project_id?: string;
  projectId?: string;
  worktree_path?: string;
  worktreePath?: string;
  worktree_branch?: string;
  worktreeBranch?: string;
  // Why the dead-session overlay reads: events.ts:174 and
  // session-term.ts:1269 both fall back off it.
  last_error?: string;
  lastError?: string;
  // Lifecycle phase (internal/wire/control.go Phase*). Absent/empty
  // means ready — the daemon omits it in the steady state.
  phase?: string;
  // OSC 0/2 window title the running program most recently set, read off
  // the daemon's VT mirror (internal/wire/control.go SessionInfo.Title).
  // Daemon-owned and in-memory only, so it is absent for a session with
  // no live process and after a daemon restart. Single-spelled: the
  // daemon emits `title` and there is no camelCase variant to fall back
  // to.
  title?: string;
}

export interface ProjectInfo {
  id: string;
  name?: string;
  cwd?: string;
  color?: string;
  order?: number;
}

// A structural view of SessionTerm (app/session-term.ts). Wave 3 typed
// the registry `unknown` so its files couldn't pretend to know the class;
// wave 5b reversed that, because view.ts and focus.ts touch these members
// at ~30 sites and the alternative is 12 unchecked `as` casts at the
// `state.terms.get()` calls.
//
// Wave 6 was expected to replace this with SessionTerm's own type once
// that file converted. It deliberately did NOT: `Map<string, SessionTerm>`
// would force every DOM-test stub to spell out 53 fields instead of 16.
// The interface stays, listing only what app modules actually reach for,
// and SessionTerm satisfies it structurally — which is the check
// `state.terms.set()` already performs at every insertion site.
//
// `term` stays optional and structural rather than `import { Terminal }`
// so a TermTile is still assignable to SnapTarget (lib/view-scroll.ts) —
// which view.ts relies on at snapVisibleTermsToBottom(). The shared
// `term` key is also what keeps that assignment out of TS's weak-type
// check, since every SnapTarget member is optional.
//
// Extends ReplayFlags and types `term` as ReplayXterm so a TermTile is
// accepted where lib/scrollback.ts wants a ReplayTerm — events.ts hands
// tiles straight to handleScrollbackEvent/abandonReplays, and the
// alternative was a cast at each of those five call sites.
export interface TermTile extends ReplayFlags {
  host: HTMLElement;
  termTitle?: string;
  // Required, not optional: session-term.ts:514,520 always initializes
  // both and every reader branches on the value, never on absence
  // (scrollback.ts:28 states the rule).
  attached: boolean;
  needsReattach: boolean;
  // Timestamp of the last replay event, used by the scroll-jump
  // detector to label a following up-move (session-term.ts:682,816).
  _lastReplayTs?: number;
  // `options` is here for applyFontSize and applyXtermTheme
  // (session-term.ts), the two app-side writers of xterm's live config.
  // Optional like the rest of the intersection: the DOM-test stubs omit
  // `term` entirely.
  term?:
    | (ReplayXterm & {
        focus?: () => void;
        options?: {
          fontSize?: number;
          theme?: Partial<ReturnType<typeof xtermTheme>>;
        };
      })
    | null;
  // Dead-session overlay. Required for the same reason as `attached`:
  // session-term.ts:613 initializes it and setDead writes it on every
  // transition, so readers branch on the value, never on absence.
  // keyboard.ts routes Enter/Escape to the two handlers when it's shown.
  deadOverlayShown: boolean;
  _closeDead(): void;
  _dismissDead(): void;
  show(): void;
  hide(): void;
  ensureAttached(): void;
  rebaselineReplayCols(reason: string): void;
  // The single resize entry point. applyFontSize (session-term.ts) calls it
  // explicitly because a font-size change doesn't resize the body box, so
  // the ResizeObserver never fires on its own.
  _onBodyResize(): void;
  setInfo(info: SessionInfo): void;
  // Patches the tile header's state icon from current info + attention
  // without a full setInfo — the attention paths (events.ts bell /
  // clearAttention) call it directly since they don't touch name/title.
  // Optional: DOM-test stubs that never render a tile header can omit it.
  refreshStateIcon?(): void;
  // Both params are optional because the implementation defaults them
  // (`name || ''`, `color || '#888'`) and ProjectInfo's fields are optional.
  setProject(name?: string, color?: string): void;
  setDead(isDead: boolean, reason?: string): void;
  // Lifecycle phase (lib/phase-steps.ts). `phase` is required for the
  // same reason as `attached`: SessionTerm initializes it and every
  // reader branches on the value.
  phase: string;
  setPhase(phase: string): void;
  // Called on scrollback_replay_done to drop the loading panel once
  // the terminal has painted.
  revealAfterReplay(): void;
  writeData(b64: string): void;
  destroy(): void;
}

export interface AppState {
  projects: ProjectInfo[];
  sessions: SessionInfo[];
  collapsed: Set<string>;
  minimizedProjects: Set<string>;
  attention: Set<string>;
  attentionReturnId: string | null;
  attentionRestored: Set<string>;
  attentionRestoredProjects: Set<string>;
  nav: NavHistory;
  minimized: Set<string>;
  aliveById: Map<string, boolean>;
  phaseById: Map<string, string>;
  dismissedDead: Set<string>;
  terms: Map<string, TermTile>;
  activeId: string | null;
  currentProjectId: string | null;
  view: ViewMode;
  gridProjectId: string | null;
  fontSize: number;
}

// ---------- the compat view ----------
//
// `state` used to BE the app's data. It is now a facade: every field
// reads through to store/store.ts, and `terms` to store/terms.ts.
//
// It exists so the ~200 read sites across app/ keep compiling unchanged
// while the migration proceeds region by region, and because
// window.__hive_state — a Playwright API, see below — has to keep its
// exact shape. Phase 6 of the React migration deletes this object and
// moves the window exposure into store/store.ts.
//
// Writes: the setters delegate to the owning store action, so a plain
// `state.x = v` still behaves. What does NOT work any more is mutating
// a collection in place (`state.attention.add(id)`): the store's
// equality is reference-based, so an in-place edit is invisible to
// subscribers. Every such site in src/ has been converted to an action;
// use the actions in new code.

export const state: AppState = {
  get projects() {
    return store.getState().projects;
  },
  set projects(v: ProjectInfo[]) {
    actions.setProjects(v);
  },
  get sessions() {
    return store.getState().sessions;
  },
  set sessions(v: SessionInfo[]) {
    actions.setSessions(v);
  },
  get collapsed() {
    return store.getState().collapsed;
  },
  set collapsed(v: Set<string>) {
    actions.setCollapsed(v);
  },
  get minimizedProjects() {
    return store.getState().minimizedProjects;
  },
  set minimizedProjects(v: Set<string>) {
    actions.setMinimizedProjects(v);
  },
  get attention() {
    return store.getState().attention;
  },
  set attention(v: Set<string>) {
    actions.setAttention(v);
  },
  get attentionReturnId() {
    return store.getState().attentionReturnId;
  },
  set attentionReturnId(v: string | null) {
    actions.setAttentionReturnId(v);
  },
  get attentionRestored() {
    return store.getState().attentionRestored;
  },
  set attentionRestored(v: Set<string>) {
    actions.setAttentionRestored(v);
  },
  get attentionRestoredProjects() {
    return store.getState().attentionRestoredProjects;
  },
  set attentionRestoredProjects(v: Set<string>) {
    actions.setAttentionRestoredProjects(v);
  },
  // Mutated in place by lib/nav-history.ts — the store holds a stable
  // reference, so there is nothing to notify. The setter exists only
  // because AppState types the field writable: without it, a plain
  // `state.nav = …` compiles and then throws at runtime on a
  // getter-only property.
  get nav() {
    return store.getState().nav;
  },
  set nav(v: NavHistory) {
    actions.setNav(v);
  },
  get minimized() {
    return store.getState().minimized;
  },
  set minimized(v: Set<string>) {
    actions.setMinimized(v);
  },
  get aliveById() {
    return store.getState().aliveById;
  },
  set aliveById(v: Map<string, boolean>) {
    actions.setAliveById(v);
  },
  get phaseById() {
    return store.getState().phaseById;
  },
  set phaseById(v: Map<string, string>) {
    actions.setPhaseById(v);
  },
  get dismissedDead() {
    return store.getState().dismissedDead;
  },
  set dismissedDead(v: Set<string>) {
    actions.setDismissedDead(v);
  },
  // The registry object itself is stable (store/terms.ts owns it), so
  // an assignment refills it rather than swapping the reference — the
  // dom tests assign a fresh Map in setup and Playwright holds on to
  // window.__hive_state.terms across navigations.
  get terms() {
    return termsMap();
  },
  set terms(v: Map<string, TermTile>) {
    const reg = termsMap();
    reg.clear();
    for (const [k, t] of v) reg.set(k, t);
  },
  get activeId() {
    return store.getState().activeId;
  },
  set activeId(v: string | null) {
    actions.setActiveId(v);
  },
  get currentProjectId() {
    return store.getState().currentProjectId;
  },
  set currentProjectId(v: string | null) {
    actions.setCurrentProjectId(v);
  },
  get view() {
    return store.getState().view;
  },
  set view(v: ViewMode) {
    actions.setView(v);
  },
  get gridProjectId() {
    return store.getState().gridProjectId;
  },
  set gridProjectId(v: string | null) {
    actions.setGridProjectId(v);
  },
  get fontSize() {
    return store.getState().fontSize;
  },
  set fontSize(v: number) {
    actions.setFontSize(v);
  },
};

// E2E test affordance: expose the state facade under a dunder name so
// Playwright specs can read xterm buffer contents via
// state.terms.get(id).term.buffer.active. Gated on the Vite mock/real
// env vars so production builds drop this — the gates are inlined to
// string literals by Vite at build time, so the whole block is dead
// code in a normal wails build.
if (
  typeof window !== 'undefined' &&
  (import.meta.env.VITE_WAILS_MOCK === '1' ||
    import.meta.env.VITE_WAILS_REAL === '1')
) {
  window.__hive_state = state;
}

// Re-exported for the modules (and tests) that imported them from here
// before the store existed. The store owns the storage keys now.
export {
  loadSavedView,
  loadSavedCollapsed,
  loadSavedMinimizedProjects,
  saveCollapsed,
  saveMinimizedProjects,
} from '../store/store.js';
