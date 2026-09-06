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
  namespacedKey,
} from '../lib/collapsed.js';
import { SEEN_KEY } from '../lib/whats-new.js';
import { createNavHistory, type NavHistory } from '../lib/nav-history.js';
import type { PhasePanel } from '../lib/phase-steps.js';
import type { ModeHint } from '../lib/status.js';
import type {
  AppState,
  IdeaInfo,
  ProjectInfo,
  SessionInfo,
  TermTile,
} from '../app/state.js';
import { termsMap } from './terms.js';
import type { WorktreesPayload } from '../lib/worktrees.js';
import type { ChoiceSpec } from '../app/modals/choice-dialog.js';

// Sidebar width bounds. 220 is the design system's sidebar floor
// (docs/design-docs/ui/tokens.md › Spacing); a stored width below it is
// clamped up on load. Mirrored by main.tsx's resizer, which is the only
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
  // Newest release the user has opened the What's New modal on, or null
  // if they never have. Lives in the store rather than in the sidebar
  // button's own state because the modal has TWO entry points — the gift
  // and the command palette — and component state only one of them can
  // reach leaves the dot up after the palette already recorded the read.
  whatsNewSeen: string | null;
  // False until hydratePersistedProjectSets has run. applyProjectList
  // refuses to prune while it is false: pruning an un-hydrated (empty)
  // set persists [] and wipes the user's tray — bug #340 exactly.
  projectSetsHydrated: boolean;
  attentionReturnId: string | null;
  attentionRestored: Set<string>;
  attentionRestoredProjects: Set<string>;
  nav: NavHistory;
  minimized: Set<string>;
  aliveById: Map<string, boolean>;
  phaseById: Map<string, string>;
  tileChrome: Map<string, TileChromeState>;
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
  // Every idea the daemon knows about, newest first, across every
  // project. One flat list rather than a per-project map: the sidebar
  // needs an open count for each card on every render, and the daemon
  // fans out single-idea events that a map would have to find the right
  // bucket for anyway.
  ideas: IdeaInfo[];
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
  | 'quick-idea'
  | 'idea-inbox'
  | 'help'
  | 'whats-new';

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
  // The capture sheet's project is a draft the user can change, so the
  // entry carries only what it opens ON: the project the focused
  // session is in, or the default project when there is none.
  | { id: 'quick-idea'; seq: number; projectId: string }
  | { id: 'idea-inbox'; seq: number; projectId: string; projectName: string }
  | { id: 'help'; seq: number }
  | { id: 'whats-new'; seq: number };

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
// Retry re-enters main.tsx's bounded retryBoot(), and reconstructing that
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

function removeStorage(key: string): void {
  try {
    localStorage.removeItem(key);
  } catch {
    /* private mode etc. — nothing to remove */
  }
}

export function loadSavedView(): ViewMode {
  return normalizeView(readStorage(VIEW_STORAGE_KEY));
}

// The daemon-state-dir id the two project-id keys are suffixed with,
// and the gate that keeps us from writing before we know it. Both are
// module state rather than store state: they are storage plumbing, not
// anything a component renders. resetStore() clears them so a
// namespace cannot leak between vitest files sharing a worker.
let storageNS = '';
let persistProjectSets = false;

function collapsedKey(): string {
  return namespacedKey(COLLAPSED_STORAGE_KEY, storageNS);
}

function minimizedProjectsKey(): string {
  return namespacedKey(MINIMIZED_PROJECTS_STORAGE_KEY, storageNS);
}

// Hydrate the two persisted project-id sets under `ns` — the id of the
// daemon registry this GUI is attached to (App.StateDirID). Call it
// once at boot, BEFORE ConnectControl: the first project:list would
// otherwise prune sets that have not been loaded yet.
//
// An empty ns means the daemon could not be identified. Persistence
// then stays off for the session rather than falling back to the bare
// key: writing that key is what lets one instance clobber another's
// state, and it would resurrect a legacy key a prior migration
// deleted, which the other GUI would then adopt.
export function hydratePersistedProjectSets(ns: string): void {
  if (!ns) return;
  storageNS = ns;
  migrateLegacyProjectSets();
  persistProjectSets = true;
  set({
    collapsed: loadCollapsed(readStorage(collapsedKey())),
    minimizedProjects: loadCollapsed(readStorage(minimizedProjectsKey())),
    projectSetsHydrated: true,
  });
}

// One-time move of the pre-#340 un-namespaced values into this
// instance's namespace. Only ever runs with a non-empty storageNS: with
// an empty one the two key names are identical and "adopt then delete"
// would destroy the value it just read.
function migrateLegacyProjectSets(): void {
  if (!storageNS) return;
  for (const [legacy, next] of [
    [COLLAPSED_STORAGE_KEY, collapsedKey()],
    [MINIMIZED_PROJECTS_STORAGE_KEY, minimizedProjectsKey()],
  ]) {
    const raw = readStorage(legacy);
    if (raw === null) continue;
    if (readStorage(next) === null) writeStorage(next, raw);
    removeStorage(legacy);
  }
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
  if (!persistProjectSets) return;
  writeStorage(collapsedKey(), serializeCollapsed(set));
}

function persistMinimizedProjects(set: ReadonlySet<string>): void {
  if (!persistProjectSets) return;
  writeStorage(minimizedProjectsKey(), serializeCollapsed(set));
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
    collapsed: new Set(), // project ids that are collapsed — persisted, but
    //   NOT loaded here: the storage key is suffixed with the daemon's
    //   state-dir id, which only an async binding can tell us. main.tsx
    //   calls hydratePersistedProjectSets before connecting.
    whatsNewSeen: readStorage(SEEN_KEY), // NOT namespaced per daemon like
    //   the two sets below: "what have I read" is a fact about the person,
    //   not about which registry this window is attached to. Read eagerly
    //   because the sidebar needs it on its first paint, before any
    //   hydration step has run.
    minimizedProjects: new Set(), // project ids pulled out of the sidebar
    //   list into the tray at its bottom; their sessions are hidden from
    //   grid views too. Persisted and hydrated exactly like `collapsed`.
    projectSetsHydrated: false,
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
    tileChrome: new Map(), // session id -> the tile chrome components/TileChrome.tsx
    //   renders. Written by SessionTerm's own methods, whose names, call
    //   sites and timing are unchanged — only the rendering moved. NOT
    //   pruned by pruneToLiveSessions/forgetSession: its lifetime is the
    //   SessionTerm's, and destroy() drops it. A `removed` event can
    //   arrive while the dead tile is still on screen.
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
    ideas: [],
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
  // Before hydration the sets are empty, so pruning would persist [] and
  // wipe the tray. Apply the list, skip the tidy-up (#340).
  if (!s.projectSetsHydrated) {
    set({
      projects: list,
      currentProjectId: s.currentProjectId ?? list[0]?.id ?? null,
    });
    return;
  }
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

// ---------- tile chrome ----------
//
// The state components/TileChrome.tsx renders a terminal tile's header
// and overlays from. It holds only what is genuinely the TILE's, not
// the session's: everything else the header shows — name, worktree,
// project, session state — is read straight from the session list, so
// the tile and the sidebar can never disagree about the same session.
//
// - `phase` is the TILE's phase, not phaseById's. setPhase() updates the
//   tile and never writes back to info; resolving the state icon from
//   the session list instead would repaint it from whatever the last
//   snapshot said — stale for exactly the transition setPhase exists for.
export interface TileChromeState {
  phase: string;
  dead: boolean;
  deadReason: string;
  // The loading panel: whether it is up, and the model it renders.
  //
  // `phasePanel` is stored rather than derived from `phase` because the
  // panel outlives the phase it describes — _revealAfterPhase() holds it
  // past PhaseReady until the replay has painted, and phasePanel(ready)
  // is null. Deriving in the component would blank the steps on the
  // ready edge and leave a bare spinner for that window.
  phaseVisible: boolean;
  phasePanel: PhasePanel | null;
}

export function initialTileChrome(phase: string) {
  return {
    phase,
    dead: false,
    deadReason: '',
    phaseVisible: false,
    phasePanel: null,
  } satisfies TileChromeState;
}

// One write path, so a no-op patch stays genuinely silent: zustand
// compares the map by reference, and mapWith only allocates when the
// value differs. Shallow-comparing the patch first is what makes
// "same phase again" — which setPhase does on every session:list — free.
export function patchTileChrome(
  id: string,
  patch: Partial<TileChromeState>,
): void {
  const cur = get().tileChrome.get(id);
  if (!cur) return;
  let changed = false;
  for (const key of Object.keys(patch) as (keyof TileChromeState)[]) {
    if (!Object.is(cur[key], patch[key])) {
      changed = true;
      break;
    }
  }
  if (!changed) return;
  set({ tileChrome: mapWith(get().tileChrome, id, { ...cur, ...patch }) });
}

export function addTileChrome(id: string, state: TileChromeState): void {
  set({ tileChrome: mapWith(get().tileChrome, id, state) });
}

export function dropTileChrome(id: string): void {
  set({ tileChrome: mapWithout(get().tileChrome, id) });
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
//
// needs_attention is derived server-side and lives on SessionInfo
// itself (session.needs_attention) — there is no local Set here. See
// the frozen transition table in
// docs/exec-plans/completed/336-session-state-model.md. What stays is the
// ⌘B/⇧⌘B round-tripping bookkeeping below, which is genuinely local UI
// state (which sessions/projects a bell round pulled out of the tray).

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

/**
 * Record that the user has read up to `version`.
 *
 * The single writer for the read receipt — both `openWhatsNew` entry points
 * funnel through here, so the persisted value and the dot can never disagree.
 */
export function markWhatsNewSeen(version: string): void {
  if (get().whatsNewSeen === version) return;
  writeStorage(SEEN_KEY, version);
  set({ whatsNewSeen: version });
}

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

// ---------- ideas ----------
//
// LIST_IDEAS replaces the list; every IDEA_EVENT patches one entry.
// Both keep it sorted newest-first, which is the order the inbox and
// the daemon both use.

function byCreatedDesc(list: IdeaInfo[]): IdeaInfo[] {
  // Ties break on id, not on input order: two ideas filed in the same
  // second are common (a burst of `hived idea add`), and a comparator
  // that never returns 0 lets their relative order flip on any
  // re-sort — a row moving under the cursor between the click and the
  // mouseup.
  return [...list].sort((a, b) => {
    if (a.created !== b.created) return a.created < b.created ? 1 : -1;
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  });
}

export function setIdeas(ideas: IdeaInfo[]): void {
  set({ ideas: byCreatedDesc(ideas || []) });
}

export function addIdea(idea: IdeaInfo): void {
  const cur = get().ideas;
  if (cur.some((i) => i.id === idea.id)) return;
  set({ ideas: byCreatedDesc([...cur, idea]) });
}

export function updateIdea(idea: IdeaInfo): void {
  const cur = get().ideas;
  // An `updated` for an idea this window never saw is an add: a GUI
  // that connected after the idea was filed has no other way to learn
  // of it short of a re-list.
  if (!cur.some((i) => i.id === idea.id)) {
    addIdea(idea);
    return;
  }
  set({ ideas: cur.map((i) => (i.id === idea.id ? idea : i)) });
}

export function removeIdea(id: string): void {
  const cur = get().ideas;
  if (!cur.some((i) => i.id === id)) return;
  set({ ideas: cur.filter((i) => i.id !== id) });
}

// openIdeasOf is the sidebar badge's count and the inbox's list: an
// idea that has been marked done is out of the inbox, and one a session
// was started from is still live work the user has not closed out.
//
// Takes the list, not the state, so a component selects the raw `ideas`
// slice and derives from it. A selector that filtered would build a new
// array on every snapshot read, which is the shape useSyncExternalStore
// rejects ("the result of getSnapshot should be cached").
export function openIdeasOf(ideas: IdeaInfo[], projectId: string): IdeaInfo[] {
  return ideas.filter((i) => i.project_id === projectId && i.status !== 'done');
}

// ---------- choice dialog ----------

let choiceSeq = 0;
export function setChoiceDialog(spec: ChoiceSpec | null): void {
  set({ choiceDialog: spec ? { spec, seq: ++choiceSeq } : null });
}

export function resetStore(seed: Partial<AppData> = {}): void {
  storageNS = '';
  persistProjectSets = false;
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
// (`hiveStateView.minimized.add(id)`): the store compares by reference, so an
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
