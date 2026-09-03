// ---------- the terminal registry ----------
//
// session id -> SessionTerm, deliberately OUTSIDE the reactive store.
//
// A SessionTerm owns an xterm instance, a WebGL slot (lib/webgl-budget.ts,
// 8 process-wide) and a live PTY attachment. Putting one in reactive
// state would invite React to treat it as a value it may recreate, and
// unmount/remount of a mounted terminal is exactly the bug the whole
// migration has to avoid. Nothing subscribes to this map: the render
// paths read it by id when they need a host element, and the pty:* data
// plane writes straight through it.

import type { TermTile } from '../app/state.js';

const terms = new Map<string, TermTile>();

export function getTerm(id: string): TermTile | undefined {
  return terms.get(id);
}

export function setTerm(id: string, term: TermTile): void {
  terms.set(id, term);
}

export function deleteTerm(id: string): boolean {
  return terms.delete(id);
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
  terms.clear();
}
