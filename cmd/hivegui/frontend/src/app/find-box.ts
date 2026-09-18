// The in-session find box (spec 431).
//
// One ⌘F, two sources. Which one is used is decided from the terminal's
// buffer type: the normal buffer is searched with @xterm/addon-search
// and highlighted in place; the alternate screen — which keeps no
// scrollback, so the terminal holds one screenful — is searched against
// the agent's on-disk transcript, via the daemon.
//
// This module owns the orchestration. Pure decisions live in lib/find.ts
// and the rendering lives in components/FindBox.tsx.

import { flushSync } from 'react-dom';
import {
  findSourceFor,
  mayUseSearchAddon,
  newestFirstIndex,
  reanchorIndex,
  stepIndex,
  type FindSource,
} from '../lib/find.js';
import {
  appStore,
  initialFind,
  patchTileChrome,
  type FindState,
  type TranscriptLine,
  type TranscriptMatch,
} from '../store/store.js';
import type { SearchHit } from './state.js';

/** Window of transcript lines fetched around the active match. */
const WINDOW_LINES = 80;

/** Matches requested per search. Mirrors the daemon's own cap. */
const MAX_MATCHES = 500;

/**
 * Debounce for re-running the current query when a session produces
 * output. Long enough that a flooding session does not re-search per
 * chunk, short enough that "it appeared while I was looking" still
 * feels immediate.
 */
const OUTPUT_DEBOUNCE_MS = 250;

/**
 * Second, later refresh after output stops. Covers the agent writing its
 * transcript record only once a message is complete.
 */
const SETTLE_REFRESH_MS = 1500;

export interface FindDeps {
  /** Asks the daemon to search a session's transcript. */
  searchTranscript(
    sessionID: string,
    query: string,
    maxMatches: number,
  ): Promise<void>;
  /** Asks the daemon for a window of transcript lines. */
  getTranscriptLines(
    sessionID: string,
    reqID: number,
    center: number,
    count: number,
  ): Promise<void>;
  /** The SessionTerm for a session, or null when it is not mounted. */
  term(sessionID: string): FindTerm | null;
  /** Hands keyboard focus back to the active terminal. */
  focusActiveTerm(): void;
}

/**
 * The slice of SessionTerm this module needs, structurally — so tests
 * pass a plain object and main.tsx passes the real tile.
 *
 * Every member is optional because TermTile's are: the DOM-test stubs
 * render no terminal at all, and a tile without one is a normal state
 * here rather than an error.
 */
export interface FindTerm {
  bufferType?(): string | undefined;
  searchNext?(query: string): SearchHit;
  /** Starts a new query from the bottom — the newest match. */
  searchNewest?(query: string): SearchHit;
  searchPrev?(query: string): SearchHit;
  clearSearch?(): void;
  beginSearch?(): void;
  endSearch?(): void;
}

const NO_HIT: SearchHit = { index: 0, total: 0, capped: false };

let deps: FindDeps;
let outputTimer: ReturnType<typeof setTimeout> | null = null;
let settleTimer: ReturnType<typeof setTimeout> | null = null;

export function initFindBox(d: FindDeps) {
  deps = d;
}

function find(sessionID: string): FindState | null {
  return appStore.getState().tileChrome.get(sessionID)?.find ?? null;
}

function patchFind(sessionID: string, patch: Partial<FindState>) {
  const cur = find(sessionID);
  if (!cur) return;
  patchTileChrome(sessionID, { find: { ...cur, ...patch } });
}

/**
 * Opens the box for a session, or refocuses it when already open.
 *
 * Toggle-and-refocus rather than plain open because on macOS the entry
 * point is the native menu accelerator, which fires on every press —
 * a second ⌘F must not stack a second box.
 */
export function openFindBox(sessionID: string) {
  const term = deps.term(sessionID);
  const source = findSourceFor(term?.bufferType?.());
  if (find(sessionID)) {
    focusFindInput(sessionID);
    return;
  }
  patchTileChrome(sessionID, { find: initialFind(source) });
  // Claim the viewport before any search moves it, so the four
  // bottom-follow yank sites stop dragging the reader back.
  term?.beginSearch?.();
  if (source === 'transcript') {
    // Open on the tail of the transcript rather than a blank box: the
    // empty query is answered with the line count, and the window
    // request below fills the view.
    void deps.searchTranscript(sessionID, '', MAX_MATCHES);
  }
  focusFindInput(sessionID);
}

export function closeFindBox(sessionID: string) {
  if (!find(sessionID)) return;
  if (outputTimer) {
    clearTimeout(outputTimer);
    outputTimer = null;
  }
  if (settleTimer) {
    clearTimeout(settleTimer);
    settleTimer = null;
  }
  const term = deps.term(sessionID);
  term?.clearSearch?.();
  // Release the viewport claim and restore the pre-search follow state
  // before the box unmounts.
  term?.endSearch?.();
  // flushSync because this runs from plain listeners (the window
  // keydown handler, the native menu item): an ordinary store write
  // lands a microtask later and focusActiveTerm() would run while the
  // box is still visible, which app/focus.ts refuses to act through.
  flushSync(() => patchTileChrome(sessionID, { find: null }));
  deps.focusActiveTerm();
}

export function toggleFindBox(sessionID: string) {
  if (find(sessionID)) closeFindBox(sessionID);
  else openFindBox(sessionID);
}

function focusFindInput(sessionID: string) {
  // Deferred a frame: the input does not exist until React has
  // committed the box.
  requestAnimationFrame(() => {
    const el = document.querySelector<HTMLInputElement>(
      `[data-find-input="${cssEscape(sessionID)}"]`,
    );
    el?.focus();
    el?.select();
  });
}

function cssEscape(s: string): string {
  return typeof CSS !== 'undefined' && CSS.escape ? CSS.escape(s) : s;
}

/** Runs the current query against whichever source is selected. */
export function runQuery(sessionID: string, query: string) {
  const state = find(sessionID);
  if (!state) return;
  // A new query drops the old matches, so applyMatches knows this is a
  // fresh search (start at the newest) rather than a live refresh
  // (stay on the match the user is reading).
  patchFind(sessionID, { query, index: 0, matches: [] });

  if (state.source === 'transcript') {
    void deps.searchTranscript(sessionID, query, MAX_MATCHES);
    return;
  }

  const term = deps.term(sessionID);
  if (!term) return;
  if (!query) {
    term.clearSearch?.();
    patchFind(sessionID, {
      query,
      total: 0,
      index: 0,
      capped: false,
      ready: true,
    });
    return;
  }
  // Guarded, not merely conventional: searching while the alternate
  // buffer is active permanently poisons the addon for the normal
  // buffer for the rest of the session's life.
  if (!mayUseSearchAddon(term.bufferType?.())) return;
  // Bottom to top (spec 431): a new query starts from the newest match.
  applyHit(sessionID, term.searchNewest?.(query) ?? NO_HIT);
}

/** Steps to the next or previous match. */
export function stepMatch(sessionID: string, delta: number) {
  const state = find(sessionID);
  if (!state || state.total <= 0) return;

  // delta > 0 is "next", which in a bottom-to-top search means OLDER —
  // further up the output. The transcript list is newest-first, so older
  // is a higher index.
  if (state.source === 'transcript') {
    const index = stepIndex(state.index, state.total, delta);
    patchFind(sessionID, { index });
    requestWindowFor(sessionID, index);
    return;
  }

  const term = deps.term(sessionID);
  if (!term || !mayUseSearchAddon(term.bufferType?.())) return;
  // In the terminal, older is upward: findPrevious.
  const r =
    delta >= 0
      ? (term.searchPrev?.(state.query) ?? NO_HIT)
      : (term.searchNext?.(state.query) ?? NO_HIT);
  applyHit(sessionID, r);
}

/** Writes an addon result into the box, converted to newest-first. */
function applyHit(sessionID: string, r: SearchHit) {
  patchFind(sessionID, {
    index: newestFirstIndex(r.index, r.total),
    total: r.total,
    capped: r.capped,
    ready: true,
  });
}

/**
 * The addon re-runs the search on its own when output arrives or the
 * terminal resizes, and reports the new result set here. This is what
 * keeps the buffer-mode count live (spec 431 criterion 8); without it the
 * addon's refreshed highlights and the box's number disagree.
 */
export function onFindResults(
  sessionID: string,
  r: { resultIndex: number; resultCount: number },
) {
  const state = find(sessionID);
  if (state?.source !== 'buffer' || !state.query) return;
  applyHit(sessionID, {
    index: r.resultIndex,
    total: r.resultCount,
    capped: r.resultCount >= 1000,
  });
}

/** Asks for the transcript window centered on the active match. */
function requestWindowFor(sessionID: string, index: number) {
  const state = find(sessionID);
  if (!state) return;
  const match = state.matches[index];
  const center = match ? match.line : Math.max(0, state.totalLines - 1);
  const reqId = state.reqId + 1;
  patchFind(sessionID, { reqId });
  void deps.getTranscriptLines(sessionID, reqId, center, WINDOW_LINES);
}

/**
 * Applies a TRANSCRIPT_MATCHES response.
 *
 * Discards a response whose query is no longer the one in the box.
 * Searches run per keystroke and are re-issued on session output, so
 * two can be in flight and land out of order; without this check the
 * later-arriving older response silently paints stale matches.
 */
export function applyMatches(msg: {
  session_id?: string;
  sessionID?: string;
  query?: string;
  available?: boolean;
  reason?: string;
  total?: number;
  truncated?: boolean;
  total_lines?: number;
  totalLines?: number;
  matches?: TranscriptMatch[];
}) {
  const sessionID = msg.session_id ?? msg.sessionID ?? '';
  const state = find(sessionID);
  if (state?.source !== 'transcript') return;
  if ((msg.query ?? '') !== state.query) return;

  const matches = msg.matches ?? [];
  // Newest-first from the daemon. A fresh search starts at the newest
  // (runQuery cleared the old matches); a live refresh stays on the
  // match the user was reading, which new output has pushed down the list.
  const index = reanchorIndex(state.matches[state.index], matches);
  patchFind(sessionID, {
    ready: true,
    reason: msg.available ? '' : (msg.reason ?? 'unavailable'),
    matches,
    total: msg.total ?? matches.length,
    capped: Boolean(msg.truncated),
    totalLines: msg.total_lines ?? msg.totalLines ?? 0,
    index,
  });
  if (msg.available) requestWindowFor(sessionID, index);
}

/**
 * Applies a TRANSCRIPT_LINES response.
 *
 * Keyed on reqId, not on query or revision: a window response carries
 * no query, and two requests differing only in their centre share a
 * revision, so nothing else distinguishes them.
 */
export function applyLines(msg: {
  session_id?: string;
  sessionID?: string;
  req_id?: number;
  reqID?: number;
  start?: number;
  total_lines?: number;
  totalLines?: number;
  available?: boolean;
  reason?: string;
  lines?: TranscriptLine[];
}) {
  const sessionID = msg.session_id ?? msg.sessionID ?? '';
  const state = find(sessionID);
  if (!state) return;
  const reqID = msg.req_id ?? msg.reqID ?? 0;
  if (reqID !== state.reqId) return;

  patchFind(sessionID, {
    ready: true,
    reason: msg.available ? '' : (msg.reason ?? 'unavailable'),
    lines: msg.lines ?? [],
    lineStart: msg.start ?? 0,
    totalLines: msg.total_lines ?? msg.totalLines ?? state.totalLines,
  });
}

/**
 * Re-selects the source when a session's buffer type changes.
 *
 * An agent starting, or a vim opening on a shell, flips the source
 * under an open box. Re-running the query keeps the box honest rather
 * than leaving it showing results from a source that no longer applies.
 */
export function onBufferChange(
  sessionID: string,
  bufferType: string | undefined,
) {
  const state = find(sessionID);
  if (!state) return;
  const source: FindSource = findSourceFor(bufferType);
  if (source === state.source) return;
  patchFind(sessionID, {
    source,
    ready: false,
    reason: '',
    matches: [],
    lines: [],
    total: 0,
    index: 0,
  });
  runQuery(sessionID, state.query);
}

/**
 * Re-runs the current query after a session writes output, debounced.
 *
 * This is how text that arrives while the box is open becomes findable
 * without reopening it. It needs no polling and no new push channel:
 * the GUI is already receiving this session's output.
 */
export function onSessionOutput(sessionID: string) {
  const state = find(sessionID);
  // Buffer mode needs nothing here: the addon refreshes itself on write
  // and reports through onFindResults.
  if (state?.source !== 'transcript' || !state.query) return;
  if (outputTimer) clearTimeout(outputTimer);
  if (settleTimer) clearTimeout(settleTimer);
  // Two trailing refreshes, both reset by every new chunk of output:
  //  - a quick one, so text the agent has already written shows up fast;
  //  - a settle one, because the agent writes its transcript record when
  //    a message FINISHES, which can land after the terminal goes quiet.
  //    Refreshing only on output would then miss the newest message until
  //    the next burst.
  // Each is cheap: the daemon re-parses only the transcript's new tail.
  outputTimer = setTimeout(() => {
    outputTimer = null;
    refreshTranscript(sessionID);
  }, OUTPUT_DEBOUNCE_MS);
  settleTimer = setTimeout(() => {
    settleTimer = null;
    refreshTranscript(sessionID);
  }, SETTLE_REFRESH_MS);
}

// A refresh, not runQuery: the matches are kept so applyMatches can
// re-anchor on the one the user is reading.
function refreshTranscript(sessionID: string) {
  const cur = find(sessionID);
  if (cur?.source === 'transcript' && cur.query) {
    void deps.searchTranscript(sessionID, cur.query, MAX_MATCHES);
  }
}

/** Test seam: drops the pending output debounce. */
export function resetFindBoxForTest() {
  if (outputTimer) clearTimeout(outputTimer);
  outputTimer = null;
  if (settleTimer) clearTimeout(settleTimer);
  settleTimer = null;
}
