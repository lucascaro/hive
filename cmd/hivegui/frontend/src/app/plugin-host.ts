// ---------- UI plugin host (spec 471) ----------
//
// Loads the app-side half of every enabled plugin that declares a "ui"
// entry (docs/plugins.md), and owns everything a plugin can reach: the
// frozen `hive` object each one gets, its subscriptions, its stylesheet
// and its settings writes.
//
// Containment is the point of most of this file. A plugin module runs in
// the page, fully trusted, and the app has one React root — so a plugin
// that throws, never finishes loading, or floods updates must cost that
// plugin and nothing else:
//   * import() and activate() each race a timeout; a late answer is
//     ignored through a per-load generation.
//   * every callback a plugin hands us (events, commands, badges,
//     banners) runs inside try/catch, and render throws are caught by
//     PluginBoundary (components/PluginSurfaces.tsx);
//   * any of those marks the plugin "failed": its surfaces unmount, its
//     subscriptions and stylesheet go, and Settings offers Disable.
//   * settings.set is coalesced — one write in flight, latest wins.
// A synchronous busy loop is out of reach: it holds the thread the host
// would need to stop it (freeze-heartbeat only reports it).
//
// With no UI plugin enabled the host does nothing past one store
// subscription.

import * as React from 'react';
import { flushSync } from 'react-dom';
import {
  EventsOn,
  ListPlugins,
  PluginAssetBase,
  SetPluginConfig,
} from '../bridge.js';
import { releaseFocus } from '../lib/focus-trap.js';
import { isMac } from '../lib/platform.js';
import {
  checkContributions,
  chordMatches,
  PLUGIN_API_VERSION,
  resolveCommands,
  type PluginContributions,
  type ResolvedCommand,
} from '../lib/plugin-api.js';
import { paletteShortcuts, shortcutGroups } from '../lib/shortcuts.js';
import {
  anyModalOpen,
  appStore,
  closeModal,
  isModalOpen,
  modalEntry,
  openModal,
  setPluginConfigOverlay,
  setPluginPanel,
  setPluginUI,
  useAppStore,
} from '../store/store.js';
import type { PluginInfo, SessionInfo } from './state.js';

export const IMPORT_TIMEOUT_MS = 10_000;
export const ACTIVATE_TIMEOUT_MS = 5_000;
/** How long a surface may stay suspended before the plugin is failed. */
export const RENDER_TIMEOUT_MS = 5_000;
/** The daemon's cap on a UI plugin's stored settings (wire.MaxPluginConfig). */
export const MAX_CONFIG_BYTES = 64 << 10;

export type ViewResult = 'closed' | 'dismissed';

export interface PluginHostDeps {
  /** The host components plugins may render (hive.components). */
  components: Record<string, unknown>;
  switchTo: (id: string) => void;
  refocusActiveTerm: () => void;
  /** Tests inject this; the app uses a real dynamic import. */
  importer?: (url: string) => Promise<unknown>;
}

interface Loaded {
  gen: number;
  key: string;
  offs: (() => void)[];
  link: HTMLLinkElement | null;
  contrib: PluginContributions | null;
  failed: boolean;
}

let deps: PluginHostDeps | null = null;
let importer: (url: string) => Promise<unknown> = (url) =>
  import(/* @vite-ignore */ url);
let assetBase: Promise<string> | null = null;
const loaded = new Map<string, Loaded>();
let gens = 0;

const appData = () => appStore.getState();

/** The load's identity: a reinstall or a manifest change reloads. */
function loadKey(p: PluginInfo): string {
  return [p.version, p.commit ?? '', p.ui?.entry ?? '', p.ui?.style ?? ''].join(
    '\u0000',
  );
}

function wantsUI(p: PluginInfo): boolean {
  return (
    !!p.ui &&
    p.enabled &&
    p.status === 'running' &&
    p.api_version === PLUGIN_API_VERSION
  );
}

function assetURL(base: string, p: PluginInfo, path: string): string {
  const v = encodeURIComponent(`${p.version}-${p.commit ?? ''}`);
  return `${base}/plugins/${encodeURIComponent(p.id)}/${path
    .split('/')
    .map(encodeURIComponent)
    .join('/')}?v=${v}`;
}

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

function withTimeout<T>(p: Promise<T>, ms: number, what: string): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const t = setTimeout(
      () => reject(new Error(`${what} did not finish within ${ms / 1000}s`)),
      ms,
    );
    p.then(
      (v) => {
        clearTimeout(t);
        resolve(v);
      },
      (e: unknown) => {
        clearTimeout(t);
        reject(e);
      },
    );
  });
}

function current(id: string, gen: number): Loaded | null {
  const l = loaded.get(id);
  return l && l.gen === gen && !l.failed ? l : null;
}

// Tears down everything a load acquired. Never throws: a plugin's own
// deactivate() is the one piece of its code this runs.
function release(id: string, l: Loaded): void {
  for (const off of l.offs.splice(0)) {
    try {
      off();
    } catch {
      /* the bridge's off is ours; nothing to do */
    }
  }
  l.link?.remove();
  l.link = null;
  if (l.contrib?.deactivate) {
    try {
      l.contrib.deactivate();
    } catch (e) {
      console.warn(`plugin ${id}: deactivate threw`, e);
    }
  }
  const view = modalEntry('plugin-view');
  if (view?.pluginId === id) closeSessionView('dismissed');
  if (appData().pluginPanel === id) setPluginPanel(null);
}

/** Marks a plugin failed: its surfaces unmount and Settings shows why.
 * Stays failed until it is disabled, reinstalled or re-enabled. */
export function failPlugin(id: string, err: unknown): void {
  const l = loaded.get(id);
  if (!l || l.failed) return;
  l.failed = true;
  console.warn(`plugin ${id} failed:`, err);
  release(id, l);
  l.contrib = null;
  setPluginUI(id, { status: 'failed', error: errText(err) });
}

function unload(id: string): void {
  const l = loaded.get(id);
  if (!l) return;
  loaded.delete(id);
  if (!l.failed) release(id, l);
  setPluginUI(id, null);
}

function load(p: PluginInfo): void {
  const id = p.id;
  const gen = ++gens;
  const l: Loaded = {
    gen,
    key: loadKey(p),
    offs: [],
    link: null,
    contrib: null,
    failed: false,
  };
  loaded.set(id, l);
  setPluginUI(id, { status: 'loading' });
  assetBase ??= PluginAssetBase().catch(() => '');
  const ui = p.ui;
  if (!ui) return;
  assetBase
    .then((base) => {
      if (!current(id, gen)) return undefined;
      if (ui.style) {
        const link = document.createElement('link');
        link.rel = 'stylesheet';
        link.href = assetURL(base, p, ui.style);
        link.dataset.plugin = id;
        document.head.appendChild(link);
        l.link = link;
      }
      return withTimeout(
        importer(assetURL(base, p, ui.entry)),
        IMPORT_TIMEOUT_MS,
        'loading the module',
      );
    })
    .then((mod) => {
      if (!current(id, gen)) return undefined;
      const activate = (mod as { default?: unknown } | undefined)?.default;
      if (typeof activate !== 'function') {
        throw new Error('the module has no default export activate(hive)');
      }
      return withTimeout(
        Promise.resolve().then(() => activate(makeApi(id, gen))),
        ACTIVATE_TIMEOUT_MS,
        'activate()',
      );
    })
    .then((contrib) => {
      if (!current(id, gen)) return;
      l.contrib = checkContributions(contrib);
      setPluginUI(id, { status: 'active', contrib: l.contrib });
    })
    .catch((e: unknown) => {
      if (current(id, gen)) failPlugin(id, e);
    });
}

function reconcile(plugins: PluginInfo[]): void {
  const want = new Map(plugins.filter(wantsUI).map((p) => [p.id, p]));
  for (const [id, l] of loaded) {
    const p = want.get(id);
    if (!p || loadKey(p) !== l.key) unload(id);
  }
  for (const [id, p] of want) {
    if (!loaded.has(id)) load(p);
  }
}

// ---------- the `hive` object ----------

function configOf(
  plugins: PluginInfo[],
  overlay: Readonly<Record<string, Record<string, unknown>>>,
  id: string,
): Record<string, unknown> {
  if (overlay[id]) return overlay[id];
  const c = plugins.find((p) => p.id === id)?.config;
  return c && typeof c === 'object' ? (c as Record<string, unknown>) : {};
}

// Drops a plugin's overlay once the daemon has echoed the value it
// holds: from then on the stored config is the truth again.
function settleOverlay(plugins: PluginInfo[]): void {
  const overlay = appData().pluginConfigOverlay;
  for (const id of Object.keys(overlay)) {
    const stored = plugins.find((p) => p.id === id)?.config;
    if (stored && JSON.stringify(stored) === JSON.stringify(overlay[id])) {
      setPluginConfigOverlay(id, null);
    }
  }
}

// One SET_PLUGIN_CONFIG in flight per plugin; later writes replace the
// queued one. A plugin that writes in a loop costs one round trip at a
// time, never a queue.
const configQueue = new Map<string, { busy: boolean; next: object | null }>();

function pumpConfig(id: string): void {
  const q = configQueue.get(id);
  if (!q) return;
  const v = q.next;
  q.next = null;
  if (!v) {
    q.busy = false;
    return;
  }
  q.busy = true;
  SetPluginConfig(id, v as Record<string, unknown>)
    .catch((e: unknown) => {
      console.warn(`plugin ${id}: saving settings failed`, e);
      // Back to what the daemon holds: drop the optimistic value and
      // re-list, so the UI stops showing a write that did not land.
      setPluginConfigOverlay(id, null);
      ListPlugins().catch(() => {});
    })
    .finally(() => pumpConfig(id));
}

function setConfig(id: string, value: unknown): void {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error('settings.set takes a plain object');
  }
  if (JSON.stringify(value).length > MAX_CONFIG_BYTES) {
    throw new Error(`settings are limited to ${MAX_CONFIG_BYTES} bytes`);
  }
  // Seen at once by settings.get()/use(); the daemon's echo settles it.
  setPluginConfigOverlay(id, value as Record<string, unknown>);
  let q = configQueue.get(id);
  if (!q) {
    q = { busy: false, next: null };
    configQueue.set(id, q);
  }
  q.next = value;
  if (!q.busy) pumpConfig(id);
}

let viewResolve: ((r: ViewResult) => void) | null = null;

function openSessionView(
  id: string,
  sessionId: string,
  props: unknown,
): Promise<ViewResult> {
  if (!loaded.get(id)?.contrib?.sessionView?.modal) {
    return Promise.reject(new Error('this plugin contributes no session view'));
  }
  viewResolve?.('dismissed');
  viewResolve = null;
  openModal({ id: 'plugin-view', pluginId: id, sessionId, props });
  return new Promise<ViewResult>((resolve) => {
    viewResolve = resolve;
  });
}

/** Closes the open plugin view, resolving its openSessionView promise. */
export function closeSessionView(result: ViewResult = 'closed'): void {
  if (!isModalOpen('plugin-view')) return;
  const r = viewResolve;
  viewResolve = null;
  releaseFocus(document.getElementById('plugin-view'));
  flushSync(() => closeModal('plugin-view'));
  r?.(result);
  if (!anyModalOpen()) deps?.refocusActiveTerm();
}

function togglePanel(id: string): void {
  if (!loaded.get(id)?.contrib?.sessionView?.panel) {
    throw new Error('this plugin contributes no panel');
  }
  setPluginPanel(appData().pluginPanel === id ? null : id);
  deps?.refocusActiveTerm();
}

function makeApi(id: string, gen: number) {
  const live = () => current(id, gen) !== null;
  // Wails hands most events over as a JSON string; plugins get the value.
  const parse = (v: unknown) => {
    if (typeof v !== 'string') return v;
    try {
      return JSON.parse(v);
    } catch {
      return v;
    }
  };
  return Object.freeze({
    apiVersion: PLUGIN_API_VERSION,
    pluginId: id,
    React,
    components: Object.freeze({ ...(deps?.components ?? {}) }),
    useSessions: (): SessionInfo[] => useAppStore((s) => s.sessions),
    getSessions: (): SessionInfo[] => appData().sessions,
    useActiveSessionId: (): string | null => useAppStore((s) => s.activeId),
    getActiveSessionId: (): string | null => appData().activeId,
    on(event: string, cb: (...args: unknown[]) => void): () => void {
      if (typeof event !== 'string' || typeof cb !== 'function') {
        throw new Error('hive.on(event, callback)');
      }
      const l = current(id, gen);
      if (!l) return () => {};
      const off = EventsOn(event, (...args: unknown[]) => {
        if (!live()) return;
        try {
          cb(...args.map(parse));
        } catch (e) {
          failPlugin(id, e);
        }
      });
      l.offs.push(off);
      return () => {
        const i = l.offs.indexOf(off);
        if (i >= 0) l.offs.splice(i, 1);
        off();
      };
    },
    actions: Object.freeze({
      switchTo: (sessionId: string) => deps?.switchTo(sessionId),
    }),
    settings: Object.freeze({
      get: () => configOf(appData().plugins, appData().pluginConfigOverlay, id),
      use: () => {
        const plugins = useAppStore((s) => s.plugins);
        const overlay = useAppStore((s) => s.pluginConfigOverlay);
        return React.useMemo(
          () => configOf(plugins, overlay, id),
          [plugins, overlay],
        );
      },
      set: (value: unknown) => setConfig(id, value),
    }),
    openSessionView: (sessionId: string, props?: unknown) =>
      openSessionView(id, sessionId, props),
    closeSessionView: () => {
      if (modalEntry('plugin-view')?.pluginId === id)
        closeSessionView('closed');
    },
    togglePanel: () => togglePanel(id),
  });
}

export type HiveAPI = ReturnType<typeof makeApi>;

// ---------- commands and chords ----------

let coreLabels: Set<string> | null = null;

function core(): Set<string> {
  if (!coreLabels) {
    coreLabels = new Set(
      Object.values(paletteShortcuts({ isMac })).filter(Boolean),
    );
    for (const g of shortcutGroups({ isMac })) {
      for (const item of g.items) coreLabels.add(item.keys);
    }
  }
  return coreLabels;
}

/** Every active plugin's commands, chords resolved against core and
 * against each other (plugins in id order, first one wins). */
export function pluginCommands(
  state: Readonly<
    Record<string, { status: string; contrib?: PluginContributions }>
  > = appData().pluginUI,
): ResolvedCommand[] {
  const plugins = Object.keys(state)
    .sort()
    .flatMap((id) => {
      const st = state[id];
      const cmds = st.status === 'active' ? st.contrib?.commands : undefined;
      return cmds?.length ? [{ id, commands: cmds }] : [];
    });
  return resolveCommands(plugins, core(), isMac, (m) => console.warn(m));
}

/** Runs a plugin command, failing the plugin if it throws. */
export function runPluginCommand(r: ResolvedCommand): void {
  try {
    r.command.run();
  } catch (e) {
    failPlugin(r.pluginId, e);
  }
}

/** keyboard.ts calls this last in its ⌘/Ctrl chain, so a core binding
 * always wins. True when a plugin command took the key. */
export function dispatchPluginChord(e: KeyboardEvent): boolean {
  if (Object.keys(appData().pluginUI).length === 0) return false;
  const hit = pluginCommands().find(
    (r) => r.bound && r.command.keys && chordMatches(r.command.keys, e),
  );
  if (!hit) return false;
  runPluginCommand(hit);
  return true;
}

// ---------- lifecycle ----------

let unsubscribe: (() => void) | null = null;

export function initPluginHost(d: PluginHostDeps): void {
  deps = d;
  if (d.importer) importer = d.importer;
  unsubscribe?.();
  let prev = appData().plugins;
  reconcile(prev);
  unsubscribe = appStore.subscribe((s) => {
    if (s.plugins === prev) return;
    prev = s.plugins;
    reconcile(prev);
    settleOverlay(prev);
  });
}

/** Test-only: forget every load and subscription. */
export function resetPluginHostForTest(): void {
  unsubscribe?.();
  unsubscribe = null;
  for (const id of [...loaded.keys()]) unload(id);
  configQueue.clear();
  viewResolve = null;
  assetBase = null;
  coreLabels = null;
  importer = (url) => import(/* @vite-ignore */ url);
}
