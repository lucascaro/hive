// ---------- the terminal registry ----------
//
// session id -> SessionTerm, deliberately OUTSIDE the reactive store.
//
// A SessionTerm owns an xterm instance, a WebGL slot (lib/webgl-budget.ts,
// 8 process-wide) and a live PTY attachment. Putting one in reactive
// state would invite React to treat it as a value it may recreate, and
// unmount/remount of a mounted terminal is exactly the bug the whole
// migration has to avoid. The render paths read it by id when they need
// a host element, and the pty:* data plane writes straight through it.
//
// MEMBERSHIP, however, is observable — see subscribeTerms below. That is
// the narrowest thing components/TileChrome.tsx needs in order to portal
// chrome into a tile: which ids have a live host, never the SessionTerm
// values themselves. The distinction is the whole point: observable, not
// reactive.

import { useSyncExternalStore } from 'react';

import type { TermTile } from '../app/state.js';

const terms = new Map<string, TermTile>();

// Membership subscription. A monotonic counter rather than a diff: the
// only consumer re-derives an id list from it, and the map is small.
let version = 0;
const listeners = new Set<() => void>();

// Bumps the membership version. Deliberately NOT exported: every write
// to the map goes through setTerm/deleteTerm/clearTerms below, which
// call this themselves. termsMap() hands out the raw map, so a future
// caller COULD mutate it directly and would then need to notify — export
// this at that point, not before.
function notifyTerms(): void {
  version++;
  for (const l of listeners) l();
}

export function subscribeTerms(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function termsVersion(): number {
  return version;
}

export function getTerm(id: string): TermTile | undefined {
  return terms.get(id);
}

export function setTerm(id: string, term: TermTile): void {
  if (terms.get(id) === term) return;
  terms.set(id, term);
  notifyTerms();
}

export function deleteTerm(id: string): boolean {
  const had = terms.delete(id);
  if (had) notifyTerms();
  return had;
}

export function termCount(): number {
  return terms.size;
}

export function allTerms(): IterableIterator<TermTile> {
  return terms.values();
}

// The raw map. Exposed for the two callers that genuinely need the
// object itself: store.ts's hiveStateView (window.__hive_state.terms is
// a Playwright API — specs call .get(id).term.buffer.active) and the
// dom tests, which seed and clear it wholesale.
export function termsMap(): Map<string, TermTile> {
  return terms;
}

export function clearTerms(): void {
  if (terms.size === 0) return;
  terms.clear();
  notifyTerms();
}

// useTermIds returns the ids with a live host, and re-renders the caller
// when that SET changes — never when a SessionTerm's own state moves.
// The snapshot is a cached array keyed on the version so that
// useSyncExternalStore's identity check holds between notifications;
// returning a fresh array every call would loop.
let idsVersion = -1;
let idsSnapshot: readonly string[] = [];

function termIdsSnapshot(): readonly string[] {
  if (idsVersion !== version) {
    idsVersion = version;
    idsSnapshot = [...terms.keys()];
  }
  return idsSnapshot;
}

export function useTermIds(): readonly string[] {
  return useSyncExternalStore(subscribeTerms, termIdsSnapshot, termIdsSnapshot);
}
