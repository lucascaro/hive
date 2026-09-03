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
import type { ModeHint } from '../lib/status.js';
import type {
  AppState,
  ProjectInfo,
  SessionInfo,
  TermTile,
} from '../app/state.js';
import { termsMap } from './terms.js';
import type { WorktreesPayload } from '../lib/worktrees.js';
import type { ChoiceSpec } from '../app/modals/choice-dialog.js';

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
  // ---------- chrome (Phase 2) ----------
  // The status bar's RENDERED output, not a second copy of the flash
  // engine: lib/status.ts's createStatus still owns FLASH_MIN_MS and the
  // persistent/transient arbitration, and app/dom.ts hands its render
  // callback straight to setStatusText.
  status: StatusView;
  modeHint: ModeHint[];
  bootState: BootStateView | null;
  banners: Record<BannerSlot, BannerData>;
  // ---------- modals ----------
  // The open modals, innermost last. Since Phase 4 every modal has an
  // entry, so this is both the RENDER signal — which React modal is
  // mounted-visible — and the answer to "does a modal own the keyboard?"
  // (anyModalOpen below). There is no second source to keep it agreeing
  // with; the `.hidden` class each modal toggles is derived from it.
  modals: ModalEntry[];
  // The daemon's last worktree inventory for the open project, or null
  // before the first reply. The daemon answers every mutation with a
  // fresh inventory, so the browser never patches — it re-renders from
  // whatever landed here.
  worktreesPayload: WorktreesPayload | null;
  // The choice dialog is not in `modals`: it is mounted over any of
  // them, and its answer travels back to the caller through a promise
  // rather than through a component. `seq` remounts the body on a
  // re-ask the same way a modal entry's does.
  choiceDialog: ChoiceDialogEntry | null;
}

// A modal is its id plus whatever that opening was parameterised with.
// The launcher carries a request because every one of its openings is
// different (which project, duplicate-from, resume-in-worktree); the
// settings modal has nothing to carry.
export type ModalId =
  | 'launcher'
  | 'settings'
  | 'project-editor'
  | 'command-palette'
  | 'worktrees'
  | 'help';

// `seq` is the opening's generation, minted by openModal. A component
// keys its per-open state off it (`key={entry.seq}`), which is what makes
// a re-open — ⌘T over an already-open launcher — start clean instead of
// looking like no change at all.
export type ModalEntry =
  | { id: 'launcher'; seq: number; req: LauncherRequest }
  | { id: 'settings'; seq: number }
  | { id: 'project-editor'; seq: number; editing: ProjectInfo | null }
  | { id: 'command-palette'; seq: number }
  | { id: 'worktrees'; seq: number; projectId: string; projectName: string }
  | { id: 'help'; seq: number };

// The open question, plus the generation that lets a second ask remount
// the body. The spec is the caller's — see app/modals/choice-dialog.ts,
// which owns the promise the answer resolves.
export interface ChoiceDialogEntry {
  spec: ChoiceSpec;
  seq: number;
}

export interface LauncherRequest {
  projectId: string | null;
  useWorktree: boolean;
  duplicateFrom: SessionInfo | null;
  duplicateCwd: string;
  worktreePath: string;
  continueConversation: boolean;
}

export interface StatusView {
  text: string;
  isError: boolean;
}

// onRetry is a callback, not a serialisable flag: the boot overlay's
// Retry re-enters main.ts's bounded retryBoot(), and reconstructing that
// binding inside a component would move the 5-attempt policy out of the
// composition root.
export interface BootStateView {
  text: string;
  onRetry: (() => void) | null;
}

export type BannerSlot = 'undo-close' | 'daemon' | 'update';

// Per-action overrides, keyed by the action id the component declares.
// Only what actually varies at runtime lives here.
export interface BannerActionData {
  label?: string;
  hidden?: boolean;
  disabled?: boolean;
  /** Extra data-* on the button (the update action's data-action/version). */
  data?: Record<string, string>;
}

// The DATA half of a banner. Its structure — kind, element id, action ids
// and their click handlers — is declared once in components/Banners.tsx,
// which is also where ui/banner.ts's markup went. Threading callbacks and
// action descriptors through the store would just be a second copy of
// that module's API.
export interface BannerData {
  text: string;
  visible: boolean;
  /** Extra data-* on the banner root (dismiss keys, the download URL). */
  data?: Record<string, string>;
  actions?: Record<string, BannerActionData>;
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
    // index.html paints "connecting…" into #status-text before any
    // script runs; StatusBar must not blank it on mount.
    status: { text: 'connecting…', isError: false },
    modeHint: [],
    bootState: { text: 'Starting hive…', onRetry: null },
    banners: {
      'undo-close': EMPTY_BANNER,
      daemon: EMPTY_BANNER,
      // The primary action starts hidden — same as the old markup's
      // display:none default for a banner with nothing to act on.
      // renderUpdateAction reveals it once there is something to do.
      update: { ...EMPTY_BANNER, actions: { action: { hidden: true } } },
    },
    modals: [],
    worktreesPayload: null,
    choiceDialog: null,
  };
}

// Shared seed. Frozen so a caller cannot mutate the shape every slot
// starts from — setBanner always replaces.
const EMPTY_BANNER: BannerData = Object.freeze({ text: '', visible: false });

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
// mutated in place by lib/nav-history.ts — but hiveStateView needs a
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

// ---------- chrome ----------

// The status bar's persistent+flash arbitration is NOT here. app/dom.ts
// owns the createStatus instance and calls this from its render
// callback, so the store holds only what is on screen.
export function setStatusText(text: string, isError: boolean): void {
  const cur = get().status;
  if (cur.text === text && cur.isError === isError) return;
  set({ status: { text, isError } });
}

export function setModeHint(hints: ModeHint[]): void {
  set({ modeHint: hints });
}

export function setBootState(view: BootStateView | null): void {
  set({ bootState: view });
}

// Merge, not replace: every caller patches one or two fields and would
// otherwise have to restate the rest.
//
// `actions` merges TWO levels down, per action id and then per field.
// One level would be a bug, not a shortcut: onUpdateAction patches only
// `{ disabled: true }` on the update banner's primary action, and a
// whole-entry replace there would drop the label renderUpdateAction just
// computed along with the data-action the click handler dispatches on —
// the button would revert to a generic "Update" mid-click.
//
// `data`, by contrast, IS replaced wholesale, and showUpdateBanner
// depends on it: dropping the per-version dismissal key on every show is
// what stops a transient banner ("up to date") from writing a stale
// version into localStorage.
export function setBanner(slot: BannerSlot, patch: Partial<BannerData>): void {
  const cur = get().banners[slot];
  let actions = cur.actions;
  if (patch.actions) {
    actions = { ...cur.actions };
    for (const [id, o] of Object.entries(patch.actions)) {
      actions[id] = { ...actions[id], ...o };
    }
  }
  const next: BannerData = { ...cur, ...patch, actions };
  // A rebuilt record is always a new reference, so `set`'s own
  // reference check cannot see a no-op here — this restores the module's
  // contract that an action changing nothing notifies nobody. It is not
  // hypothetical: wireDaemonBanner writes the same daemonBuild on every
  // control connect, and renderUpdateAction re-derives the same button
  // on every update:progress step.
  if (sameBanner(cur, next)) return;
  set({ banners: { ...get().banners, [slot]: next } });
}

// Compile-time guards for the two comparisons below. A hand-written
// equality silently swallows any field added to the type after it was
// written — the failure mode being that a banner stops updating and no
// test says why. These fail to typecheck instead: add a field to
// BannerData or BannerActionData and the literal is missing a property.
// Keep each key in sync with the branch that reads it in sameBanner().
const BANNER_FIELDS: Record<keyof BannerData, true> = {
  text: true,
  visible: true,
  data: true,
  actions: true,
};
const BANNER_ACTION_FIELDS: Record<keyof BannerActionData, true> = {
  label: true,
  hidden: true,
  disabled: true,
  data: true,
};
void BANNER_FIELDS;
void BANNER_ACTION_FIELDS;

// Shallow all the way down, which is exactly as deep as BannerData goes:
// two string/bool fields, a flat string record, and a record of flat
// records.
function sameBanner(a: BannerData, b: BannerData): boolean {
  if (a.text !== b.text || a.visible !== b.visible) return false;
  if (!sameRecord(a.data, b.data)) return false;
  const ids = new Set([
    ...Object.keys(a.actions ?? {}),
    ...Object.keys(b.actions ?? {}),
  ]);
  for (const id of ids) {
    const x = a.actions?.[id];
    const y = b.actions?.[id];
    if (!x || !y) return false;
    if (
      x.label !== y.label ||
      x.hidden !== y.hidden ||
      x.disabled !== y.disabled
    ) {
      return false;
    }
    if (!sameRecord(x.data, y.data)) return false;
  }
  return true;
}

function sameRecord(
  a: Record<string, string> | undefined,
  b: Record<string, string> | undefined,
): boolean {
  const ak = Object.keys(a ?? {});
  const bk = Object.keys(b ?? {});
  if (ak.length !== bk.length) return false;
  return ak.every((k) => a?.[k] === b?.[k]);
}

export function hideBanner(slot: BannerSlot): void {
  setBanner(slot, { visible: false });
}

// Full replace, unlike setBanner's merge — the merge is what makes a
// patch convenient and is also what makes it useless for a reset (an
// empty `actions` patch leaves every existing entry in place).
export function resetBanner(slot: BannerSlot): void {
  set({ banners: { ...get().banners, [slot]: EMPTY_BANNER } });
}

// ---------- test affordance ----------

// Reset to a freshly-loaded state, optionally seeded. Only the dom/unit
// suites call this — the app itself boots the store exactly once.
// ---------- modals ----------

// openModal pushes, replacing an entry that is already open rather than
// stacking a second copy of it: re-opening the launcher over itself is a
// real gesture (⌘T while it is up) and must leave exactly one.
let modalSeq = 0;
export function openModal(entry: DistributiveOmit<ModalEntry, 'seq'>): void {
  const rest = get().modals.filter((m) => m.id !== entry.id);
  set({ modals: [...rest, { ...entry, seq: ++modalSeq } as ModalEntry] });
}

// Omit over a union distributes only if it is applied per member.
type DistributiveOmit<T, K extends keyof T> = T extends unknown
  ? Omit<T, K>
  : never;

export function closeModal(id: ModalId): void {
  const cur = get().modals;
  const next = cur.filter((m) => m.id !== id);
  if (next.length !== cur.length) set({ modals: next });
}

export function isModalOpen(id: ModalId): boolean {
  return get().modals.some((m) => m.id === id);
}

// modalEntry returns the open entry for `id`, narrowed. The modals that
// carry a payload read it from here when they are outside React
// (keyboard.ts, the gutted modal modules) rather than through a
// selector.
export function modalEntry<T extends ModalId>(
  id: T,
): Extract<ModalEntry, { id: T }> | undefined {
  return get().modals.find((m) => m.id === id) as
    | Extract<ModalEntry, { id: T }>
    | undefined;
}

// anyModalOpen answers "does a modal own the keyboard?" for the focus
// pipeline (app/focus.ts, app/session-term.ts).
//
// Until Phase 4 this was app/modals/registry.ts asking every registered
// root whether it still had the `hidden` class, because the modals that
// had not been ported had no store entry to ask about. They all have one
// now, so the render signal and the keyboard-ownership signal are the
// same fact and there is nothing left to keep in sync. The choice dialog
// counts: it is asking a question that may destroy work, and it sits
// over everything.
export function anyModalOpen(): boolean {
  const s = get();
  return s.modals.length > 0 || s.choiceDialog !== null;
}

// ---------- worktree browser ----------

export function setWorktreesPayload(payload: WorktreesPayload | null): void {
  set({ worktreesPayload: payload });
}

// ---------- choice dialog ----------

let choiceSeq = 0;
export function setChoiceDialog(spec: ChoiceSpec | null): void {
  set({ choiceDialog: spec ? { spec, seq: ++choiceSeq } : null });
}

export function resetStore(seed: Partial<AppData> = {}): void {
  replace({ ...initialData(), ...seed }, true);
}

// React binding. Exported from here rather than a hooks file: one
// consumer shape, one line.
export function useAppStore<T>(selector: (s: AppData) => T): T {
  return useStore(appStore, selector);
}

// ---------- the Playwright state view ----------
//
// `window.__hive_state` is a permanent test API: the e2e-real specs read
// xterm buffers through `state.terms.get(id).term.buffer.active` and poll
// `state.sessions`, and the dom suite seeds its scenarios through the same
// object. Its shape is frozen (test/unit/store.test.ts asserts every field).
//
// Until Phase 6 this object lived in app/state.ts and thirteen production
// modules imported it as a compat facade for the pre-store `state`. None do
// now — they read `appStore.getState()` and `termsMap()` directly — so what
// is left here is only the exposure itself: a live view over the store plus
// the terminal registry, which is deliberately not IN the store
// (store/terms.ts explains why).
//
// The setters stay because the dom tests seed through them. They delegate to
// the owning action, so a plain `hiveStateView.x = v` notifies subscribers.
// What does NOT work is mutating a collection in place
// (`hiveStateView.attention.add(id)`): the store compares by reference, so an
// in-place edit is invisible. Use the actions.

export const hiveStateView: AppState = {
  get projects() {
    return appStore.getState().projects;
  },
  set projects(v: ProjectInfo[]) {
    setProjects(v);
  },
  get sessions() {
    return appStore.getState().sessions;
  },
  set sessions(v: SessionInfo[]) {
    setSessions(v);
  },
  get collapsed() {
    return appStore.getState().collapsed;
  },
  set collapsed(v: Set<string>) {
    setCollapsed(v);
  },
  get minimizedProjects() {
    return appStore.getState().minimizedProjects;
  },
  set minimizedProjects(v: Set<string>) {
    setMinimizedProjects(v);
  },
  get attention() {
    return appStore.getState().attention;
  },
  set attention(v: Set<string>) {
    setAttention(v);
  },
  get attentionReturnId() {
    return appStore.getState().attentionReturnId;
  },
  set attentionReturnId(v: string | null) {
    setAttentionReturnId(v);
  },
  get attentionRestored() {
    return appStore.getState().attentionRestored;
  },
  set attentionRestored(v: Set<string>) {
    setAttentionRestored(v);
  },
  get attentionRestoredProjects() {
    return appStore.getState().attentionRestoredProjects;
  },
  set attentionRestoredProjects(v: Set<string>) {
    setAttentionRestoredProjects(v);
  },
  // Mutated in place by lib/nav-history.ts — the store holds a stable
  // reference, so there is nothing to notify. The setter exists only
  // because AppState types the field writable: without it, a plain
  // `state.nav = …` compiles and then throws at runtime on a
  // getter-only property.
  get nav() {
    return appStore.getState().nav;
  },
  set nav(v: NavHistory) {
    setNav(v);
  },
  get minimized() {
    return appStore.getState().minimized;
  },
  set minimized(v: Set<string>) {
    setMinimized(v);
  },
  get aliveById() {
    return appStore.getState().aliveById;
  },
  set aliveById(v: Map<string, boolean>) {
    setAliveById(v);
  },
  get phaseById() {
    return appStore.getState().phaseById;
  },
  set phaseById(v: Map<string, string>) {
    setPhaseById(v);
  },
  get dismissedDead() {
    return appStore.getState().dismissedDead;
  },
  set dismissedDead(v: Set<string>) {
    setDismissedDead(v);
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
    return appStore.getState().activeId;
  },
  set activeId(v: string | null) {
    setActiveId(v);
  },
  get currentProjectId() {
    return appStore.getState().currentProjectId;
  },
  set currentProjectId(v: string | null) {
    setCurrentProjectId(v);
  },
  get view() {
    return appStore.getState().view;
  },
  set view(v: ViewMode) {
    setView(v);
  },
  get gridProjectId() {
    return appStore.getState().gridProjectId;
  },
  set gridProjectId(v: string | null) {
    setGridProjectId(v);
  },
  get fontSize() {
    return appStore.getState().fontSize;
  },
  set fontSize(v: number) {
    setFontSize(v);
  },
};

// E2E test affordance: expose the view under a dunder name so
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
  window.__hive_state = hiveStateView;
}
