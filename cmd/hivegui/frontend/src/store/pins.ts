// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// ---------- pinned responses store (spec 566) ----------
//
// Per-session pin headers (newest first) and the bodies the user has
// opened. Its own store, apart from activity: activity is dropped on a
// restart or revive, and pins outlive both.
//
// Loading: a renderer asks for a session's headers (GET_PINS) when it
// shows a session with pins. The answer is one or more `pins:page`
// frames, the last marked done. Deltas (`pins:changed`) can land between
// those pages, so a load remembers what changed meanwhile: an added pin
// is merged by id (never counted twice), a removed one is kept out of
// later pages, and after a clear the rest of the pages are ignored —
// they describe pins that no longer exist, while every pin added after
// the clear arrives as its own delta.

import { useEffect } from 'react';
import { createStore } from 'zustand/vanilla';
import { useStore } from 'zustand';

import { GetPinBody, GetPins } from '../bridge.js';

export interface Pin {
  id: string;
  title: string;
  created_at: number;
  body_len: number;
}

export interface PinsPage {
  session_id: string;
  pins: Pin[] | null;
  done: boolean;
}

export interface PinBody {
  session_id: string;
  id: string;
  body: string;
}

export interface PinsChanged {
  session_id: string;
  added?: Pin;
  removed_ids?: string[];
  cleared?: boolean;
}

export interface PinsEntry {
  headers: readonly Pin[];
  bodies: ReadonlyMap<string, string>;
  loaded: boolean;
  loading: boolean;
  failed: boolean;
  // While loading: ids removed by a delta, kept out of later pages.
  removed: ReadonlySet<string>;
  // While loading: a clear landed, so the rest of the pages are stale.
  ignorePages: boolean;
  // While loading: ids a delta added since the request. The load's first
  // page replaces every other header — what this client held before may
  // include pins removed while it was not listening (a reconnect).
  added: ReadonlySet<string>;
  // The next page is the load's first.
  fresh: boolean;
}

interface PinsData {
  byId: ReadonlyMap<string, PinsEntry>;
  // The inspector's Pinned section, collapsed or not. One choice for
  // every session: it is how much room the user wants pins to take.
  collapsed: boolean;
}

export const pinsStore = createStore<PinsData>()(() => ({
  byId: new Map(),
  collapsed: false,
}));

const blank = (): PinsEntry => ({
  headers: [],
  bodies: new Map(),
  loaded: false,
  loading: false,
  failed: false,
  removed: new Set(),
  ignorePages: false,
  added: new Set(),
  fresh: false,
});

function patch(id: string, fn: (e: PinsEntry) => PinsEntry): void {
  const { byId } = pinsStore.getState();
  const cur = byId.get(id) ?? blank();
  const next = fn(cur);
  if (next === cur && byId.has(id)) return;
  const m = new Map(byId);
  m.set(id, next);
  pinsStore.setState({ byId: m });
}

// newestFirst merges pins by id and sorts them newest first. Daemon ids
// sort in creation order.
function newestFirst(a: readonly Pin[], b: readonly Pin[]): Pin[] {
  const m = new Map<string, Pin>();
  for (const p of a) m.set(p.id, p);
  for (const p of b) m.set(p.id, p);
  return [...m.values()].sort((x, y) =>
    x.id < y.id ? 1 : x.id > y.id ? -1 : 0,
  );
}

export function applyPinsPage(msg: PinsPage): void {
  if (!msg?.session_id) return;
  patch(msg.session_id, (e) => {
    if (!e.loading) return e; // nobody asked
    const done = !!msg.done;
    const pins = e.ignorePages
      ? []
      : (msg.pins ?? []).filter((p) => !e.removed.has(p.id));
    const base = e.fresh
      ? e.headers.filter((p) => e.added.has(p.id))
      : e.headers;
    const headers = newestFirst(base, pins);
    if (!done) return { ...e, headers, fresh: false };
    return {
      ...e,
      headers,
      loaded: true,
      loading: false,
      failed: false,
      removed: new Set(),
      ignorePages: false,
      added: new Set(),
      fresh: false,
    };
  });
}

export function applyPinsChanged(msg: PinsChanged): void {
  if (!msg?.session_id) return;
  patch(msg.session_id, (e) => {
    if (msg.cleared) {
      return { ...e, headers: [], bodies: new Map(), ignorePages: e.loading };
    }
    let { headers, bodies, removed, added } = e;
    if (msg.added) {
      headers = newestFirst(headers, [msg.added]);
      if (e.loading) added = new Set([...added, msg.added.id]);
    }
    const gone = msg.removed_ids ?? [];
    if (gone.length > 0) {
      const drop = new Set(gone);
      headers = headers.filter((p) => !drop.has(p.id));
      const b = new Map(bodies);
      for (const id of gone) b.delete(id);
      bodies = b;
      if (e.loading) removed = new Set([...removed, ...gone]);
    }
    return { ...e, headers, bodies, removed, added };
  });
}

export function applyPinBody(msg: PinBody): void {
  if (!msg?.session_id || !msg.id) return;
  patch(msg.session_id, (e) => {
    if (!e.headers.some((p) => p.id === msg.id)) return e;
    const b = new Map(e.bodies);
    b.set(msg.id, msg.body ?? '');
    return { ...e, bodies: b };
  });
}

export function requestPins(id: string): void {
  const e = pinsStore.getState().byId.get(id);
  if (e && (e.loaded || e.loading || e.failed)) return;
  patch(id, (cur) => ({
    ...cur,
    loading: true,
    removed: new Set(),
    ignorePages: false,
    added: new Set(),
    fresh: true,
  }));
  Promise.resolve()
    .then(() => GetPins(id))
    .catch(() =>
      patch(id, (cur) => ({ ...cur, loading: false, failed: true })),
    );
}

// requestPinBody fetches a body once; it is answered as `pins:body`.
export function requestPinBody(sessionId: string, pinId: string): void {
  const e = pinsStore.getState().byId.get(sessionId);
  if (e?.bodies.has(pinId)) return;
  Promise.resolve()
    .then(() => GetPinBody(sessionId, pinId))
    .catch(() => {
      /* the row keeps saying "Loading…"; reopening it retries */
    });
}

// pinBody resolves with a pin's body, fetching it if this client has not
// yet, or rejects when it does not arrive in time (the pin was removed,
// or the daemon is gone).
export function pinBody(
  sessionId: string,
  pinId: string,
  timeoutMs = 5000,
): Promise<string> {
  const have = pinsStore.getState().byId.get(sessionId)?.bodies.get(pinId);
  if (have !== undefined) return Promise.resolve(have);
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      unsub();
      reject(new Error('the pin did not arrive'));
    }, timeoutMs);
    const unsub = pinsStore.subscribe((st) => {
      const b = st.byId.get(sessionId)?.bodies.get(pinId);
      if (b === undefined) return;
      clearTimeout(timer);
      unsub();
      resolve(b);
    });
    requestPinBody(sessionId, pinId);
  });
}

// A session list arrives on every (re)connect: refetch what may have
// missed deltas, and drop sessions that are gone.
export function resetPinsOnSessionList(liveIds: ReadonlySet<string>): void {
  const { byId } = pinsStore.getState();
  if (byId.size === 0) return;
  const m = new Map<string, PinsEntry>();
  for (const [id, e] of byId) {
    if (liveIds.has(id))
      m.set(id, { ...blank(), headers: e.headers, bodies: e.bodies });
  }
  pinsStore.setState({ byId: m });
}

// forgetPins drops a removed session's pins. Never called on a restart
// or revive: pins outlive both.
export function forgetPins(id: string): void {
  const { byId } = pinsStore.getState();
  if (!byId.has(id)) return;
  const m = new Map(byId);
  m.delete(id);
  pinsStore.setState({ byId: m });
}

export function setPinsCollapsed(collapsed: boolean): void {
  pinsStore.setState({ collapsed });
}

const NONE: readonly Pin[] = [];
const NO_BODIES: ReadonlyMap<string, string> = new Map();

// useSessionPins subscribes to one session's pins, fetching them while
// the session has any (count from SessionInfo.pin_count) and they are
// not loaded.
export function useSessionPins(
  id: string,
  count: number,
): { pins: readonly Pin[]; bodies: ReadonlyMap<string, string> } {
  const entry = useStore(pinsStore, (s) => s.byId.get(id));
  const loaded = entry?.loaded ?? false;
  const loading = entry?.loading ?? false;
  const failed = entry?.failed ?? false;
  // biome-ignore lint/correctness/useExhaustiveDependencies: the flags are the trigger; requestPins reads the store itself
  useEffect(() => {
    if (count > 0) requestPins(id);
  }, [id, count > 0, loaded, loading, failed]);
  return { pins: entry?.headers ?? NONE, bodies: entry?.bodies ?? NO_BODIES };
}

export function usePinsCollapsed(): boolean {
  return useStore(pinsStore, (s) => s.collapsed);
}
