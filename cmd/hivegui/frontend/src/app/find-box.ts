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
const WINDOW_LINES = 200;

/** Lines fetched per scroll-driven extension of the loaded range. */
const EXTEND_LINES = 200;

/**
 * The most lines kept rendered at once. Scrolling far back extends the
 * range upward and trims the far end, so a 20 MB transcript never ends up
 * in the DOM; there is no virtualization in this app to lean on.
 */
export const MAX_LOADED_LINES = 1200;

/** One counter for every window request, replace or extend alike. */
let reqCounter = 0;

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
 * Opens the box for a session, or refocuses it with the query selected
 * when it is already open — never a second box, and never a close.
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

export function closeFindBox(
  sessionID: string,
  opts: { deferFocus?: boolean } = {},
) {
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
  // Closing from a key press (Escape, the find chord) hands focus back
  // only AFTER the event has finished. Moving focus to the terminal
  // mid-keydown lets an engine deliver the rest of that key to the
  // terminal — and Escape reaching an agent interrupts it. Chromium does
  // not do this, but the app runs on WebKit, where the same key was
  // reported to reach the session.
  if (opts.deferFocus) setTimeout(() => deps.focusActiveTerm(), 0);
  else deps.focusActiveTerm();
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
    // Emptying the query returns to the most recent output: dropping the
    // loaded range makes the answer reload the tail, rather than leaving
    // the reader on the last match's window.
    if (!query) patchFind(sessionID, { lines: [], extendReqId: 0 });
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
  const reqId = ++reqCounter;
  // A replace orphans any extension still in flight: its lines belong to
  // the range being thrown away.
  patchFind(sessionID, { reqId, extendReqId: 0 });
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

  // No query and a range already loaded: this is a live refresh of the
  // plain transcript view. Only the line count changes; the pane appends
  // new lines itself if the reader is at the bottom. Replacing the window
  // here would yank someone reading history back to the end.
  if (!state.query && state.lines.length > 0 && msg.available) {
    patchFind(sessionID, {
      totalLines: msg.total_lines ?? msg.totalLines ?? state.totalLines,
    });
    return;
  }

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
  const totalLines = msg.total_lines ?? msg.totalLines ?? state.totalLines;

  if (reqID !== 0 && reqID === state.extendReqId) {
    patchFind(sessionID, {
      ...mergeLines(state, msg.lines ?? []),
      extendReqId: 0,
      totalLines,
      loadSeq: state.loadSeq + 1,
    });
    return;
  }
  if (reqID !== state.reqId) return;

  patchFind(sessionID, {
    ready: true,
    reason: msg.available ? '' : (msg.reason ?? 'unavailable'),
    lines: msg.lines ?? [],
    lineStart: msg.start ?? 0,
    totalLines,
    lastLoad: 'replace',
    loadSeq: state.loadSeq + 1,
  });
}

/**
 * Folds an extension into the loaded range: older lines go above, newer
 * below. Only lines outside the current range are taken, so an extension
 * overlapping it cannot duplicate a line. Past MAX_LOADED_LINES the far
 * end is trimmed — the end the reader is moving away from.
 */
export function mergeLines(
  state: Pick<FindState, 'lines' | 'lineStart'>,
  incoming: TranscriptLine[],
): Pick<FindState, 'lines' | 'lineStart' | 'lastLoad'> {
  const start = state.lineStart;
  const end = start + state.lines.length;
  const older = incoming.filter((l) => l.line < start);
  if (older.length > 0) {
    const merged = older.concat(state.lines).slice(0, MAX_LOADED_LINES);
    return { lines: merged, lineStart: merged[0].line, lastLoad: 'prepend' };
  }
  const newer = incoming.filter((l) => l.line >= end);
  let merged = state.lines.concat(newer);
  if (merged.length > MAX_LOADED_LINES) {
    merged = merged.slice(merged.length - MAX_LOADED_LINES);
  }
  return {
    lines: merged,
    lineStart: merged.length > 0 ? merged[0].line : start,
    lastLoad: 'append',
  };
}

/**
 * Loads the block of lines above ('up') or below ('down') the loaded
 * range. Called by the pane as the reader nears either edge, and to fill
 * a pane that is not yet full. At most one extension is in flight.
 */
export function extendLines(sessionID: string, dir: 'up' | 'down') {
  const s = find(sessionID);
  if (s?.source !== 'transcript' || s.extendReqId !== 0 || !s.ready) return;
  // Nothing loaded yet: the replace that loads the first window is in
  // flight, and extending from an empty range would fetch the wrong end.
  if (s.lines.length === 0) return;
  const end = s.lineStart + s.lines.length;
  let start: number;
  let count: number;
  if (dir === 'up') {
    if (s.lineStart <= 0) return;
    count = Math.min(EXTEND_LINES, s.lineStart);
    start = s.lineStart - count;
  } else {
    if (end >= s.totalLines) return;
    count = Math.min(EXTEND_LINES, s.totalLines - end);
    start = end;
  }
  const reqId = ++reqCounter;
  patchFind(sessionID, { extendReqId: reqId });
  // The daemon centres a window on `center` and starts it count/2 above,
  // so this centre yields exactly [start, start + count).
  void deps.getTranscriptLines(
    sessionID,
    reqId,
    start + Math.floor(count / 2),
    count,
  );
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
  // With no query too: the plain transcript view follows new output.
  if (state?.source !== 'transcript') return;
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
  if (cur?.source === 'transcript') {
    void deps.searchTranscript(sessionID, cur.query, MAX_MATCHES);
  }
}

/** Test seam: drops the pending output debounce. */
export function resetFindBoxForTest() {
  reqCounter = 0;
  if (outputTimer) clearTimeout(outputTimer);
  outputTimer = null;
  if (settleTimer) clearTimeout(settleTimer);
  settleTimer = null;
}
