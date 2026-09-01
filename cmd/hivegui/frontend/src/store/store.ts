// ---------- the app store ----------
//
// One zustand vanilla store holding every piece of app data that used
// to live on the mutable `state` object in app/state.ts. Vanilla (not
// a React-only hook) on purpose: during the React migration both
// paradigms read and write it, and the Playwright harness reaches it
// through window.__hive_state.
//
// The contract every action keeps: an immutable replace of the slices
// it touches. zustand compares by reference, so a Set/Map mutated in
// place would never notify a subscriber, and an action that changes
// nothing never notifies at all (see `set` below). Persistence lives in the
// action that owns the field — there is no separate "save" step to
// forget.
//
// `nav` is the one deliberate exception. lib/nav-history.ts mutates a
// NavHistory in place (pushNav/pruneNav), nothing renders from it, and
// giving it copy-on-write semantics would mean rewriting a tested pure
// module for zero benefit. It stays a stable object reference, mutated
// in place exactly as before.
//
// SessionTerm instances are NOT here — see store/terms.ts.

import { createStore } from 'zustand/vanilla';
import { useStore } from 'zustand';
import { DEFAULT_FONT_SIZE, clampFont } from '../lib/font.js';
import { normalizeView, VIEW_STORAGE_KEY } from '../lib/view.js';
import type { ViewMode } from '../lib/view.js';
import {
  loadCollapsed,
  serializeCollapsed,
  pruneCollapsed,
  COLLAPSED_STORAGE_KEY,
  MINIMIZED_PROJECTS_STORAGE_KEY,
} from '../lib/collapsed.js';
import { createNavHistory, type NavHistory } from '../lib/nav-history.js';
import type { ProjectInfo, SessionInfo } from '../app/state.js';

// Sidebar width bounds. 220 is the design system's sidebar floor
// (docs/design-docs/ui/tokens.md › Spacing); a stored width below it is
// clamped up on load. Mirrored by main.ts's resizer, which is the only
// writer.
export const SIDEBAR_MIN_WIDTH = 220;
export const SIDEBAR_MAX_WIDTH = 480;
export const SIDEBAR_WIDTH_STORAGE_KEY = 'hive.sidebarWidth';
export const FONT_SIZE_STORAGE_KEY = 'hive.fontSize';

export interface AppData {
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
  activeId: string | null;
  currentProjectId: string | null;
  view: ViewMode;
  gridProjectId: string | null;
  fontSize: number;
  sidebarWidth: number;
}

// ---------- persistence helpers ----------
//
// Every read is try/catch'd: private-browsing modes throw on access
// rather than returning null, and losing a persisted preference must
// never take the app down at boot.

function readStorage(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function writeStorage(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* private mode etc. — the preference just won't persist */
  }
}

export function loadSavedView(): ViewMode {
  return normalizeView(readStorage(VIEW_STORAGE_KEY));
}

export function loadSavedCollapsed(): Set<string> {
  return loadCollapsed(readStorage(COLLAPSED_STORAGE_KEY));
}

export function loadSavedMinimizedProjects(): Set<string> {
  return loadCollapsed(readStorage(MINIMIZED_PROJECTS_STORAGE_KEY));
}

export function loadSavedFontSize(): number {
  return clampFont(
    parseInt(readStorage(FONT_SIZE_STORAGE_KEY) ?? '', 10) || DEFAULT_FONT_SIZE,
  );
}

export function loadSavedSidebarWidth(): number {
  const saved = parseInt(readStorage(SIDEBAR_WIDTH_STORAGE_KEY) ?? '', 10);
  return Number.isFinite(saved) ? clampSidebarWidth(saved) : SIDEBAR_MIN_WIDTH;
}

function clampSidebarWidth(w: number): number {
  return Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, w));
}

function persistCollapsed(set: ReadonlySet<string>): void {
  writeStorage(COLLAPSED_STORAGE_KEY, serializeCollapsed(set));
}

function persistMinimizedProjects(set: ReadonlySet<string>): void {
  writeStorage(MINIMIZED_PROJECTS_STORAGE_KEY, serializeCollapsed(set));
}

// ---------- immutable set/map helpers ----------
//
// Each returns the SAME reference when nothing changed, so an action
// that would be a no-op doesn't notify subscribers.

function setWith(set: ReadonlySet<string>, id: string): Set<string> {
  if (set.has(id)) return set as Set<string>;
  const next = new Set(set);
  next.add(id);
  return next;
}

function setWithout(set: ReadonlySet<string>, id: string): Set<string> {
  if (!set.has(id)) return set as Set<string>;
  const next = new Set(set);
  next.delete(id);
  return next;
}

function mapWith<V>(
  map: ReadonlyMap<string, V>,
  id: string,
  value: V,
): Map<string, V> {
  if (map.has(id) && map.get(id) === value) return map as Map<string, V>;
  const next = new Map(map);
  next.set(id, value);
  return next;
}

function mapWithout<V>(
  map: ReadonlyMap<string, V>,
  id: string,
): Map<string, V> {
  if (!map.has(id)) return map as Map<string, V>;
  const next = new Map(map);
  next.delete(id);
  return next;
}

// Returns the SAME array when it is already in order, so a sort that
// changes nothing cannot wake a subscriber — the same contract the
// setWith/mapWith helpers keep.
function byOrder<T extends { order?: number }>(list: T[]): T[] {
  const next = [...list].sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  return next.every((item, i) => item === list[i]) ? list : next;
}

function initialData(): AppData {
  return {
    projects: [], // ProjectInfo[] in display order
    sessions: [], // SessionInfo[] in display order
    collapsed: loadSavedCollapsed(), // project ids that are collapsed — persisted
    minimizedProjects: loadSavedMinimizedProjects(), // project ids pulled out of
    //   the sidebar list into the tray at its bottom; their sessions are
    //   hidden from grid views too. Persisted, like `collapsed`.
    attention: new Set(), // session ids that have unread bells
    attentionReturnId: null, // session to jump back to (⇧⌘B): the one you
    //   were in before the FIRST ⌘B. Written only when empty, so a round
    //   of bells that walks you through several flagged sessions keeps
    //   the original anchor; cleared on use.
    attentionRestored: new Set(), // sessions ⌘B pulled out of the minimized
    //   tray this round; ⇧⌘B puts them back.
    attentionRestoredProjects: new Set(), // same round-trip, one level up:
    //   projects ⌘B revealed to reach a bell inside them.
    nav: createNavHistory(), // back/forward stacks of visited session ids
    //   (Ctrl+- / Ctrl+Shift+-). Deliberately NOT persisted, unlike
    //   `collapsed`: the terminals are gone after a restart anyway.
    //   Mutated in place by lib/nav-history.ts — see the file header.
    minimized: new Set(), // session ids hidden from grid views; restored via tray
    aliveById: new Map(), // session id -> last-seen Alive bool (for transition detection)
    phaseById: new Map(), // session id -> last-seen lifecycle phase (see lib/phase-steps.ts)
    dismissedDead: new Set(), // session ids whose dead overlay user dismissed
    activeId: null,
    currentProjectId: null, // "the project I'm working in"; can be set
    //   without a focused session (so empty projects are reachable /
    //   launchable)
    view: loadSavedView(), // 'single' | 'grid-project' | 'grid-all' — persisted
    gridProjectId: null, // project shown in grid-project mode
    fontSize: loadSavedFontSize(),
    sidebarWidth: loadSavedSidebarWidth(),
  };
}

export const appStore = createStore<AppData>()(() => initialData());

const replace = appStore.setState;
const get = appStore.getState;

// The write every action uses. zustand builds a fresh state object from
// a partial, so `setState({ attention: sameSetRef })` still swaps the
// root reference and wakes every raw subscriber. Comparing first makes
// a no-op action genuinely silent — which is what the immutable helpers
// above are for: they hand back the SAME reference when nothing
// changed, and this turns that into "no notification".
//
// `useAppStore`'s selector subscriptions would not have re-rendered
// either way (the selector output is unchanged), but `appStore.subscribe`
// consumers and anything counting notifications would have.
function set(partial: Partial<AppData>): void {
  const cur = get();
  for (const key of Object.keys(partial) as (keyof AppData)[]) {
    if (!Object.is(cur[key], partial[key])) {
      replace(partial);
      return;
    }
  }
}

// ---------- projects ----------

// Plain replace. Deliberately NOT the project:list handler — see
// applyProjectList below. Assigning a project list must never prune the
// persisted sets: the sidebar render path and the dom tests both set
// projects before the daemon has spoken, and pruning there wipes the
// user's collapse/minimize state instead of tidying it.
export function setProjects(projects: ProjectInfo[]): void {
  set({ projects: projects || [] });
}

// The project:list snapshot, and the only pruning point for the two
// persisted project-id sets: this event is the arrival of authoritative
// project data, and pruning against a not-yet-populated project list
// would wipe the sets instead of tidying them.
export function applyProjectList(projects: ProjectInfo[]): void {
  const list = projects || [];
  const s = get();
  const ids = list.map((p) => p.id);
  const pruned = pruneCollapsed(s.collapsed, ids);
  const prunedMin = pruneCollapsed(s.minimizedProjects, ids);
  if (pruned.changed) persistCollapsed(pruned.set);
  if (prunedMin.changed) persistMinimizedProjects(prunedMin.set);
  set({
    projects: list,
    currentProjectId: s.currentProjectId ?? list[0]?.id ?? null,
    collapsed: pruned.changed ? pruned.set : s.collapsed,
    minimizedProjects: prunedMin.changed ? prunedMin.set : s.minimizedProjects,
  });
}

export function addProject(p: ProjectInfo): void {
  const s = get();
  const projects = s.projects.some((x) => x.id === p.id)
    ? s.projects
    : [...s.projects, p];
  set({
    projects: byOrder(projects),
    // First-ever project: make it current.
    currentProjectId: s.currentProjectId ?? p.id,
  });
}

export function updateProject(p: ProjectInfo): void {
  const s = get();
  // `map` always allocates, so bail out before it when there is nothing
  // to replace — an unknown id, or the very object already stored.
  const i = s.projects.findIndex((x) => x.id === p.id);
  if (i < 0 || s.projects[i] === p) return;
  const projects = s.projects.map((x) => (x.id === p.id ? p : x));
  set({ projects: byOrder(projects) });
}

export function removeProject(id: string): void {
  const s = get();
  if (!s.projects.some((p) => p.id === id)) return;
  const projects = s.projects.filter((p) => p.id !== id);
  const collapsed = setWithout(s.collapsed, id);
  const minimizedProjects = setWithout(s.minimizedProjects, id);
  if (collapsed !== s.collapsed) persistCollapsed(collapsed);
  if (minimizedProjects !== s.minimizedProjects) {
    persistMinimizedProjects(minimizedProjects);
  }
  set({
    projects: byOrder(projects),
    collapsed,
    minimizedProjects,
    currentProjectId:
      s.currentProjectId === id
        ? (projects[0]?.id ?? null)
        : s.currentProjectId,
  });
}

export function setCurrentProjectId(id: string | null): void {
  set({ currentProjectId: id });
}

// ---------- sessions ----------

export function setSessions(sessions: SessionInfo[]): void {
  set({ sessions: sessions || [] });
}

// Drop any per-session bookkeeping whose session no longer exists. A
// session:list snapshot is the only path that can retire a session
// without a per-session `removed` event, so without this the tray
// leaks stale chips and the transition-detection maps grow for the
// life of the process.
export function pruneToLiveSessions(): void {
  const s = get();
  const live = new Set(s.sessions.map((x) => x.id));
  const keep = (id: string) => live.has(id);
  const minimized = new Set([...s.minimized].filter(keep));
  const aliveById = new Map([...s.aliveById].filter(([id]) => keep(id)));
  const phaseById = new Map([...s.phaseById].filter(([id]) => keep(id)));
  set({
    minimized: minimized.size === s.minimized.size ? s.minimized : minimized,
    aliveById: aliveById.size === s.aliveById.size ? s.aliveById : aliveById,
    phaseById: phaseById.size === s.phaseById.size ? s.phaseById : phaseById,
  });
}

export function addSession(s: SessionInfo): void {
  const cur = get().sessions;
  if (cur.some((x) => x.id === s.id)) return;
  set({ sessions: [...cur, s] });
}

export function updateSession(s: SessionInfo): void {
  const cur = get().sessions;
  const i = cur.findIndex((x) => x.id === s.id);
  if (i < 0 || cur[i] === s) return;
  set({ sessions: cur.map((x) => (x.id === s.id ? s : x)) });
}

export function removeSession(id: string): void {
  const cur = get().sessions;
  if (!cur.some((s) => s.id === id)) return;
  set({ sessions: cur.filter((s) => s.id !== id) });
}

export function setAlive(id: string, alive: boolean): void {
  set({ aliveById: mapWith(get().aliveById, id, alive) });
}

export function setSessionPhase(id: string, phase: string): void {
  set({ phaseById: mapWith(get().phaseById, id, phase) });
}

// Everything a `removed` event has to forget about a session, in one
// notify rather than four.
export function forgetSession(id: string): void {
  const s = get();
  set({
    aliveById: mapWithout(s.aliveById, id),
    phaseById: mapWithout(s.phaseById, id),
    dismissedDead: setWithout(s.dismissedDead, id),
    minimized: setWithout(s.minimized, id),
  });
}

// ---------- attention ----------

export function addAttention(id: string): void {
  set({ attention: setWith(get().attention, id) });
}

// Returns whether the id was flagged, mirroring the `Set.delete`
// return value the legacy call sites branch on.
export function clearAttentionFor(id: string): boolean {
  const cur = get().attention;
  const next = setWithout(cur, id);
  if (next === cur) return false;
  set({ attention: next });
  return true;
}

export function setAttention(ids: Set<string>): void {
  set({ attention: new Set(ids) });
}

export function setAttentionReturnId(id: string | null): void {
  set({ attentionReturnId: id });
}

export function addAttentionRestored(id: string): void {
  set({ attentionRestored: setWith(get().attentionRestored, id) });
}

export function addAttentionRestoredProject(pid: string): void {
  set({
    attentionRestoredProjects: setWith(get().attentionRestoredProjects, pid),
  });
}

export function clearAttentionRestored(): void {
  set({
    attentionRestored: new Set(),
    attentionRestoredProjects: new Set(),
  });
}

// ---------- dead-session overlay ----------

export function addDismissedDead(id: string): void {
  set({ dismissedDead: setWith(get().dismissedDead, id) });
}

export function clearDismissedDead(id: string): void {
  set({ dismissedDead: setWithout(get().dismissedDead, id) });
}

// ---------- sidebar collapse / minimize ----------

export function toggleCollapsed(pid: string): void {
  const cur = get().collapsed;
  const next = cur.has(pid) ? setWithout(cur, pid) : setWith(cur, pid);
  persistCollapsed(next);
  set({ collapsed: next });
}

export function setCollapsed(ids: Set<string>): void {
  const next = new Set(ids);
  persistCollapsed(next);
  set({ collapsed: next });
}

export function setMinimizedProjects(ids: Set<string>): void {
  const next = new Set(ids);
  persistMinimizedProjects(next);
  set({ minimizedProjects: next });
}

export function setMinimized(ids: Set<string>): void {
  set({ minimized: new Set(ids) });
}

export function setAliveById(m: Map<string, boolean>): void {
  set({ aliveById: new Map(m) });
}

export function setPhaseById(m: Map<string, string>): void {
  set({ phaseById: new Map(m) });
}

export function setDismissedDead(ids: Set<string>): void {
  set({ dismissedDead: new Set(ids) });
}

export function setAttentionRestored(ids: Set<string>): void {
  set({ attentionRestored: new Set(ids) });
}

export function setAttentionRestoredProjects(ids: Set<string>): void {
  set({ attentionRestoredProjects: new Set(ids) });
}

// Flush the current set to localStorage without changing it. The
// actions above persist as they write, so these only exist for the
// pre-store call sites that still say "save what I just did".
export function saveCollapsed(): void {
  persistCollapsed(get().collapsed);
}

export function saveMinimizedProjects(): void {
  persistMinimizedProjects(get().minimizedProjects);
}

// The state half of view.ts's minimizeProject/restoreProject — those
// keep the repaint, focus handoff and view-floor orchestration.
export function minimizeProject(pid: string): void {
  const next = setWith(get().minimizedProjects, pid);
  persistMinimizedProjects(next);
  set({ minimizedProjects: next });
}

export function restoreProject(pid: string): void {
  const next = setWithout(get().minimizedProjects, pid);
  persistMinimizedProjects(next);
  set({ minimizedProjects: next });
}

export function minimizeSession(id: string): void {
  set({ minimized: setWith(get().minimized, id) });
}

export function restoreSession(id: string): void {
  set({ minimized: setWithout(get().minimized, id) });
}

// ---------- selection + view ----------

// Replaces the whole history object. The app never calls this — nav is
// mutated in place by lib/nav-history.ts — but the state facade needs a
// setter to back AppState's writable `nav`, and the dom tests seed it.
export function setNav(nav: NavHistory): void {
  replace({ nav });
}

export function setActiveId(id: string | null): void {
  set({ activeId: id });
}

export function setView(view: ViewMode, persist = true): void {
  if (persist) writeStorage(VIEW_STORAGE_KEY, view);
  set({ view });
}

export function setGridProjectId(pid: string | null): void {
  set({ gridProjectId: pid });
}

export function setFontSize(n: number): void {
  const next = clampFont(n);
  writeStorage(FONT_SIZE_STORAGE_KEY, String(next));
  set({ fontSize: next });
}

export function setSidebarWidth(w: number): void {
  const next = clampSidebarWidth(w);
  writeStorage(SIDEBAR_WIDTH_STORAGE_KEY, String(next));
  set({ sidebarWidth: next });
}

// ---------- test affordance ----------

// Reset to a freshly-loaded state, optionally seeded. Only the dom/unit
// suites call this — the app itself boots the store exactly once.
export function resetStore(seed: Partial<AppData> = {}): void {
  replace({ ...initialData(), ...seed }, true);
}

// React binding. Exported from here rather than a hooks file: one
// consumer shape, one line.
export function useAppStore<T>(selector: (s: AppData) => T): T {
  return useStore(appStore, selector);
}
