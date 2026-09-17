// ---------- agent activity store (spec 416) ----------
//
// Per-session tool calls and plan, fed by `activity:event`. Its own store
// rather than a slice of appStore: frames arrive per tool call for every
// session, and nothing outside the activity renderers reads them, so
// keeping them out of AppData keeps them out of every other selector's
// path.
//
// Loading: a renderer asks for a session's snapshot (GET_ACTIVITY) the
// first time it shows it. The daemon answers on the control stream as a
// full frame, which is what marks the entry loaded. Deltas for every
// session arrive regardless and are folded in whether or not the entry
// is loaded.

import { useEffect, useSyncExternalStore } from 'react';
import { createStore } from 'zustand/vanilla';
import { useStore } from 'zustand';

import { GetActivity } from '../bridge.js';
import {
  applyActivity,
  emptyActivity,
  type ActivityMsg,
  type SessionActivity,
} from '../lib/activity.js';

export interface ActivityEntry {
  data: SessionActivity;
  loaded: boolean;
  inFlight: boolean;
  // The request failed (no control connection, unknown session). Not
  // retried until the next session list — a reconnect — or the loop
  // would be one GET_ACTIVITY per render.
  failed: boolean;
}

interface ActivityData {
  byId: ReadonlyMap<string, ActivityEntry>;
}

export const activityStore = createStore<ActivityData>()(() => ({
  byId: new Map(),
}));

const blank = (): ActivityEntry => ({
  data: emptyActivity(),
  loaded: false,
  inFlight: false,
  failed: false,
});

function patch(id: string, fn: (e: ActivityEntry) => ActivityEntry): void {
  const { byId } = activityStore.getState();
  const cur = byId.get(id) ?? blank();
  const next = fn(cur);
  if (next === cur && byId.has(id)) return;
  const m = new Map(byId);
  m.set(id, next);
  activityStore.setState({ byId: m });
}

export function applyActivityFrame(msg: ActivityMsg): void {
  if (!msg?.session_id) return;
  patch(msg.session_id, (e) => {
    const data = applyActivity(e.data, msg);
    if (msg.full) return { data, loaded: true, inFlight: false, failed: false };
    return data === e.data ? e : { ...e, data };
  });
}

export function requestActivity(id: string): void {
  const e = activityStore.getState().byId.get(id);
  if (e && (e.loaded || e.inFlight || e.failed)) return;
  patch(id, (cur) => ({ ...cur, inFlight: true }));
  Promise.resolve()
    .then(() => GetActivity(id))
    .catch(() =>
      patch(id, (cur) => ({ ...cur, inFlight: false, failed: true })),
    );
}

// A session list is what the daemon sends on every (re)connect. Snapshots
// taken before it may have missed deltas while the connection was down,
// so every entry is refetched by whoever is showing it; ids the list no
// longer has are dropped.
export function resetActivityOnSessionList(liveIds: ReadonlySet<string>): void {
  const { byId } = activityStore.getState();
  if (byId.size === 0) return;
  const m = new Map<string, ActivityEntry>();
  for (const [id, e] of byId) {
    if (liveIds.has(id))
      m.set(id, { ...e, loaded: false, inFlight: false, failed: false });
  }
  activityStore.setState({ byId: m });
}

export function forgetActivity(id: string): void {
  const { byId } = activityStore.getState();
  if (!byId.has(id)) return;
  const m = new Map(byId);
  m.delete(id);
  activityStore.setState({ byId: m });
}

const EMPTY = emptyActivity();

// useSessionActivity subscribes to one session and fetches its snapshot
// while it is not loaded.
export function useSessionActivity(id: string): SessionActivity {
  const entry = useStore(activityStore, (s) => s.byId.get(id));
  const loaded = entry?.loaded;
  const inFlight = entry?.inFlight;
  const failed = entry?.failed;
  // biome-ignore lint/correctness/useExhaustiveDependencies: the flags are the trigger; requestActivity reads the store itself
  useEffect(() => {
    requestActivity(id);
  }, [id, loaded, inFlight, failed]);
  return entry?.data ?? EMPTY;
}

// ---------- shared clock ----------
//
// Age labels and the stale flip move with time, not with events. One
// interval for every mounted renderer, running only while one is.
const TICK_MS = 5000;
let now = Date.now();
let timer: ReturnType<typeof setInterval> | null = null;
const tickers = new Set<() => void>();

function subscribeNow(fn: () => void): () => void {
  tickers.add(fn);
  if (!timer) {
    now = Date.now();
    timer = setInterval(() => {
      now = Date.now();
      for (const t of tickers) t();
    }, TICK_MS);
  }
  return () => {
    tickers.delete(fn);
    if (tickers.size === 0 && timer) {
      clearInterval(timer);
      timer = null;
    }
  };
}

// With no interval running, `now` is refreshed lazily: at most once per
// tick, so two reads in one render agree.
function readNow(): number {
  if (!timer && Date.now() - now >= TICK_MS) now = Date.now();
  return now;
}

export function useNow(): number {
  return useSyncExternalStore(subscribeNow, readNow);
}
