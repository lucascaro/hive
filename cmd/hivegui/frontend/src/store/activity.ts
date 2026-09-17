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
  // GET_ACTIVITY requests sent and not yet answered.
  pending: number;
  // Answers still to come for requests made before this entry was reset
  // by a restart. Answers arrive in request order on the one control
  // connection, so the next `discard` full frames belong to the previous
  // run and are dropped.
  discard: number;
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
  pending: 0,
  discard: 0,
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
    if (msg.full) {
      // A snapshot is only ever the answer to a request this client made.
      // One for a request from before a restart describes the previous
      // run; one nobody asked for describes nothing current.
      if (e.discard > 0) return { ...e, discard: e.discard - 1 };
      if (e.pending === 0) return e;
      return {
        ...e,
        data: applyActivity(e.data, msg),
        loaded: true,
        pending: e.pending - 1,
        failed: false,
      };
    }
    const data = applyActivity(e.data, msg);
    return data === e.data ? e : { ...e, data };
  });
}

export function requestActivity(id: string): void {
  const e = activityStore.getState().byId.get(id);
  if (e && (e.loaded || e.pending > 0 || e.failed)) return;
  patch(id, (cur) => ({ ...cur, pending: cur.pending + 1 }));
  Promise.resolve()
    .then(() => GetActivity(id))
    .catch(() =>
      patch(id, (cur) => ({
        ...cur,
        pending: Math.max(0, cur.pending - 1),
        failed: true,
      })),
    );
}

// A session list is what the daemon sends on every (re)connect. Snapshots
// taken before it may have missed deltas while the connection was down,
// so every entry is refetched by whoever is showing it; ids the list no
// longer has are dropped. Requests on the old connection will never be
// answered, so nothing is pending or to be discarded any more.
export function resetActivityOnSessionList(liveIds: ReadonlySet<string>): void {
  const { byId } = activityStore.getState();
  if (byId.size === 0) return;
  const m = new Map<string, ActivityEntry>();
  for (const [id, e] of byId) {
    if (liveIds.has(id))
      m.set(id, { ...e, loaded: false, pending: 0, discard: 0, failed: false });
  }
  activityStore.setState({ byId: m });
}

// forgetActivity drops a session's activity: on removal, and on a restart
// or revive, where the daemon starts a fresh run under the same id. Any
// answer still owed for the old run is marked to be discarded.
export function forgetActivity(id: string): void {
  const { byId } = activityStore.getState();
  const e = byId.get(id);
  if (!e) return;
  const m = new Map(byId);
  const owed = e.pending + e.discard;
  if (owed > 0) m.set(id, { ...blank(), discard: owed });
  else m.delete(id);
  activityStore.setState({ byId: m });
}

const EMPTY = emptyActivity();

export type ActivityLoad = 'loading' | 'failed' | 'loaded';

// useSessionActivity subscribes to one session and fetches its snapshot
// while it is not loaded. `enabled` is false for a session the list no
// longer has: a renderer can outlive its session by a commit, and asking
// then would open a request the daemon answers with no_such_session.
export function useSessionActivity(
  id: string,
  enabled = true,
): {
  data: SessionActivity;
  load: ActivityLoad;
} {
  const entry = useStore(activityStore, (s) => s.byId.get(id));
  const loaded = entry?.loaded ?? false;
  const pending = entry?.pending ?? 0;
  const failed = entry?.failed ?? false;
  // biome-ignore lint/correctness/useExhaustiveDependencies: the flags are the trigger; requestActivity reads the store itself
  useEffect(() => {
    if (enabled) requestActivity(id);
  }, [id, enabled, loaded, pending, failed]);
  return {
    data: entry?.data ?? EMPTY,
    load: loaded ? 'loaded' : failed ? 'failed' : 'loading',
  };
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
