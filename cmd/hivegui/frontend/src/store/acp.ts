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
  // The prompt this client sent and the daemon has neither recorded nor
  // refused yet. A refusal (control:error naming this session) moves it
  // to `returned`, for the prompt box to put back; the transcript
  // recording it as a user turn drops it.
  sent: string | null;
  returned: string | null;
}

interface AcpData {
  byId: ReadonlyMap<string, AcpEntry>;
}

export const acpStore = createStore<AcpData>()(() => ({ byId: new Map() }));

const blank = (): AcpEntry => ({
  tx: emptyAcp(),
  requested: false,
  failed: false,
  sent: null,
  returned: null,
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
    // Only a delta records a prompt: a snapshot may hold an older,
    // identical turn, which says nothing about this one.
    const recorded =
      e.sent !== null &&
      !msg.reset &&
      (msg.items ?? []).some(
        (it) =>
          it.kind === 'user' && recordsPrompt(it.text ?? '', e.sent ?? ''),
      );
    return { ...e, tx, requested, sent: recorded ? null : e.sent };
  });
}

// The daemon clips a prompt at 64 KiB of UTF-8 and marks the cut
// (internal/acp truncatedMark); the cut may split a character, which
// encoding/json writes as U+FFFD. So a clipped turn records a prompt
// that starts with what it kept.
const TRUNCATED_MARK = '\n\n[… truncated by Hive]';
function recordsPrompt(recorded: string, sent: string): boolean {
  if (recorded === sent) return true;
  if (!recorded.endsWith(TRUNCATED_MARK)) return false;
  const kept = recorded
    .slice(0, -TRUNCATED_MARK.length)
    .replace(/\uFFFD+$/, '');
  return kept.length > 0 && sent.startsWith(kept);
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
      // A prompt pending on the old connection is dropped: its refusal,
      // if any, will never arrive, and keeping it would hand it back on
      // some later, unrelated refusal. A refused prompt already waiting
      // for its box is kept.
      m.set(id, {
        ...e,
        tx: { ...e.tx, loaded: false },
        requested: false,
        failed: false,
        sent: null,
      });
  }
  acpStore.setState({ byId: m });
}

// noteSentPrompt records a prompt this client just sent, so a refusal
// can give it back.
export function noteSentPrompt(id: string, text: string): void {
  patch(id, (e) => ({ ...e, sent: text, returned: null }));
}

// returnSentPrompt gives the pending prompt back to its box: the call
// never reached the daemon.
export function returnSentPrompt(id: string): void {
  patch(id, (e) =>
    e.sent === null ? e : { ...e, sent: null, returned: e.sent },
  );
}

// useReturnedPrompt is the refused prompt waiting for its box, if any.
export function useReturnedPrompt(id: string): string | null {
  return useStore(acpStore, (s) => s.byId.get(id)?.returned ?? null);
}

// takeReturnedPrompt hands a refused prompt to the box once.
export function takeReturnedPrompt(id: string): void {
  patch(id, (e) => (e.returned === null ? e : { ...e, returned: null }));
}

// Codes the daemon answers an ACP request with when it did not act on
// it (internal/daemon sendACPError). permission_stale is absent: only
// ANSWER_PERMISSION earns it, and the next message clears the card.
const ACP_REFUSALS = new Set([
  'no_such_session',
  'not_acp_session',
  'acp_busy',
  'session_dead',
  'acp_failed',
]);

/**
 * applyAcpError folds a control:error into the session it names: a
 * pending prompt goes back to its box, and a pending snapshot request
 * becomes a failed one instead of loading until the next reconnect.
 * The error carries no request id, so on the rare overlap both happen —
 * either way the request was refused.
 */
export function applyAcpError(err: {
  code?: string;
  session_id?: string;
}): void {
  const id = err?.session_id;
  if (!id || !err.code || !ACP_REFUSALS.has(err.code)) return;
  if (!acpStore.getState().byId.has(id)) return;
  patch(id, (e) => {
    let next = e;
    // An accepted prompt is recorded (and drops `sent`) before PROMPT_ACP
    // returns, so a refusal that finds one still pending is its own.
    if (e.sent !== null) next = { ...next, sent: null, returned: e.sent };
    if (e.requested && !e.tx.loaded)
      next = { ...next, requested: false, failed: true };
    return next;
  });
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
