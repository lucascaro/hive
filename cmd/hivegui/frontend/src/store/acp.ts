// ---------- ACP transcript store (spec 496) ----------
//
// Per-session ACP transcripts, fed by `acp:transcript`. Its own store,
// like store/activity.ts: chunks stream per token for every ACP session,
// and nothing outside the transcript view reads them.
//
// Loading: the view asks for a session's snapshot (GET_ACP_TRANSCRIPT)
// the first time it shows it, and again whenever lib/acp.ts marks the
// copy stale (a delta from a new epoch). The answer arrives as an
// `acp:transcript` reset on the same ordered stream as the deltas.

import { useEffect } from 'react';
import { createStore } from 'zustand/vanilla';
import { useStore } from 'zustand';

import { GetAcpTranscript } from '../bridge.js';
import {
  applyAcp,
  emptyAcp,
  type AcpTranscript,
  type AcpTranscriptMsg,
} from '../lib/acp.js';

export interface AcpEntry {
  tx: AcpTranscript;
  // A snapshot was asked for and has not arrived.
  requested: boolean;
  // The request failed (no control connection). Not retried until the
  // next session list — a reconnect — or the loop would be one request
  // per render.
  failed: boolean;
}

interface AcpData {
  byId: ReadonlyMap<string, AcpEntry>;
}

export const acpStore = createStore<AcpData>()(() => ({ byId: new Map() }));

const blank = (): AcpEntry => ({
  tx: emptyAcp(),
  requested: false,
  failed: false,
});

function patch(id: string, fn: (e: AcpEntry) => AcpEntry): void {
  const { byId } = acpStore.getState();
  const cur = byId.get(id) ?? blank();
  const next = fn(cur);
  if (next === cur && byId.has(id)) return;
  const m = new Map(byId);
  m.set(id, next);
  acpStore.setState({ byId: m });
}

export function applyAcpFrame(msg: AcpTranscriptMsg): void {
  if (!msg?.session_id) return;
  // Deltas for a session nobody has shown are not worth an entry.
  if (!msg.reset && !acpStore.getState().byId.has(msg.session_id)) return;
  patch(msg.session_id, (e) => {
    const tx = applyAcp(e.tx, msg);
    if (tx === e.tx) return e;
    // A reset answers the request; a stale mark asks for another.
    const requested = msg.reset ? false : tx.loaded ? e.requested : false;
    return { ...e, tx, requested };
  });
}

export function requestAcpTranscript(id: string): void {
  const e = acpStore.getState().byId.get(id);
  if (e && (e.tx.loaded || e.requested || e.failed)) return;
  patch(id, (cur) => ({ ...cur, requested: true }));
  Promise.resolve()
    .then(() => GetAcpTranscript(id))
    .catch(() =>
      patch(id, (cur) => ({ ...cur, requested: false, failed: true })),
    );
}

// A session list is what the daemon sends on every (re)connect: deltas
// may have been missed while the connection was down, and a request on
// the old connection will never be answered. Every entry is refetched
// by whoever shows it; ids the list no longer has are dropped.
export function resetAcpOnSessionList(liveIds: ReadonlySet<string>): void {
  const { byId } = acpStore.getState();
  if (byId.size === 0) return;
  const m = new Map<string, AcpEntry>();
  for (const [id, e] of byId) {
    if (liveIds.has(id))
      m.set(id, {
        tx: { ...e.tx, loaded: false },
        requested: false,
        failed: false,
      });
  }
  acpStore.setState({ byId: m });
}

export function forgetAcp(id: string): void {
  const { byId } = acpStore.getState();
  if (!byId.has(id)) return;
  const m = new Map(byId);
  m.delete(id);
  acpStore.setState({ byId: m });
}

const EMPTY = emptyAcp();

export type AcpLoad = 'loading' | 'failed' | 'loaded';

// useAcpTranscript subscribes to one session and fetches its snapshot
// while it is not loaded. `enabled` is false for a session the list no
// longer has, which the daemon would answer with no_such_session.
export function useAcpTranscript(
  id: string,
  enabled = true,
): { tx: AcpTranscript; load: AcpLoad } {
  const entry = useStore(acpStore, (s) => s.byId.get(id));
  const loaded = entry?.tx.loaded ?? false;
  const requested = entry?.requested ?? false;
  const failed = entry?.failed ?? false;
  // biome-ignore lint/correctness/useExhaustiveDependencies: the flags are the trigger; requestAcpTranscript reads the store itself
  useEffect(() => {
    if (enabled) requestAcpTranscript(id);
  }, [id, enabled, loaded, requested, failed]);
  return {
    tx: entry?.tx ?? EMPTY,
    load: loaded ? 'loaded' : failed ? 'failed' : 'loading',
  };
}
