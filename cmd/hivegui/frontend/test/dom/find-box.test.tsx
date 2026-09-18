// @vitest-environment jsdom
//
// Covers the in-session find box (spec 431): src/app/find-box.ts driving
// src/components/FindBox.tsx.
//
// The load-bearing behaviours here are the ones that are invisible when
// they break:
//   - the source is picked from the terminal's BUFFER TYPE, not the
//     agent, and re-picked when the buffer changes under an open box;
//   - the xterm search addon is never asked to search while the
//     alternate buffer is active, because doing so permanently poisons
//     it for the normal buffer for the rest of the session's life;
//   - responses that no longer match the box are discarded, since two
//     searches can be in flight and land out of order.
import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  beforeEach,
  afterEach,
} from 'vitest';
import { act, render } from '@testing-library/react';
import {
  appStore,
  initialTileChrome,
  resetStore,
} from '../../src/store/store.js';

type FindBoxModule = typeof import('../../src/app/find-box.js');
let mod: FindBoxModule;
let FindBox: typeof import('../../src/components/FindBox.js')['FindBox'];

const SID = 's1';

const searchTranscript = vi.fn(async () => {});
const getTranscriptLines = vi.fn(async () => {});
const focusActiveTerm = vi.fn();

// A stand-in SessionTerm. `bufferType` is a let so a test can flip the
// buffer mid-run, which is what an agent starting does.
let bufferType = 'normal';
// The addon reports TOP-DOWN indexes; index 2 of 3 is the bottom-most
// match, which the box shows as 1/3 because search runs bottom to top.
const searchNewest = vi.fn(() => ({ index: 2, total: 3, capped: false }));
const searchNext = vi.fn(() => ({ index: 2, total: 3, capped: false }));
const searchPrev = vi.fn(() => ({ index: 1, total: 3, capped: false }));
const clearSearch = vi.fn();
const beginSearch = vi.fn();
const endSearch = vi.fn();

const term = {
  bufferType: () => bufferType,
  searchNewest,
  searchNext,
  searchPrev,
  clearSearch,
  beginSearch,
  endSearch,
};

beforeAll(async () => {
  mod = await import('../../src/app/find-box.js');
  FindBox = (await import('../../src/components/FindBox.js')).FindBox;
});

beforeEach(() => {
  vi.clearAllMocks();
  bufferType = 'normal';
  resetStore();
  appStore.setState({
    tileChrome: new Map([[SID, initialTileChrome('ready')]]),
  });
  mod.initFindBox({
    searchTranscript,
    getTranscriptLines,
    term: () => term,
    focusActiveTerm,
  });
  // requestAnimationFrame is used to focus the input after React
  // commits; jsdom has it, but run it synchronously so the tests do not
  // have to wait a frame.
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
    cb(0);
    return 0;
  });
});

afterEach(() => {
  mod.resetFindBoxForTest();
  vi.unstubAllGlobals();
});

function find() {
  return appStore.getState().tileChrome.get(SID)?.find ?? null;
}

function renderBox() {
  const f = find();
  if (!f) throw new Error('no find state');
  return render(<FindBox id={SID} find={f} />);
}

describe('source selection', () => {
  it('picks the terminal buffer on a normal-buffer session', () => {
    act(() => mod.openFindBox(SID));
    expect(find()?.source).toBe('buffer');
  });

  it('picks the transcript on an alt-screen session', () => {
    bufferType = 'alternate';
    act(() => mod.openFindBox(SID));
    expect(find()?.source).toBe('transcript');
    // Opening on the tail rather than a blank box: the empty query is
    // issued immediately so the view has something to show.
    expect(searchTranscript).toHaveBeenCalledWith(
      SID,
      '',
      expect.any(Number),
      expect.any(Number),
    );
  });

  it('claims the viewport on open and releases it on close', () => {
    act(() => mod.openFindBox(SID));
    expect(beginSearch).toHaveBeenCalledTimes(1);
    act(() => mod.closeFindBox(SID));
    expect(endSearch).toHaveBeenCalledTimes(1);
    expect(focusActiveTerm).toHaveBeenCalledTimes(1);
    expect(find()).toBeNull();
  });

  // An agent starting, or a vim opening on a shell, flips the source
  // under an open box.
  it('re-picks the source when the buffer changes under an open box', () => {
    vi.useFakeTimers();
    try {
      act(() => mod.openFindBox(SID));
      act(() => mod.runQuery(SID, 'needle'));
      expect(find()?.source).toBe('buffer');

      bufferType = 'alternate';
      act(() => mod.onBufferChange(SID, 'alternate'));

      expect(find()?.source).toBe('transcript');
      // The box stays open and re-runs the query against the new source,
      // once the typing debounce has passed.
      expect(find()).not.toBeNull();
      act(() => {
        vi.advanceTimersByTime(mod.TYPE_DEBOUNCE_MS);
      });
      expect(searchTranscript).toHaveBeenCalledWith(
        SID,
        'needle',
        expect.any(Number),
        expect.any(Number),
      );
    } finally {
      vi.useRealTimers();
    }
  });

  // One daemon search per pause in typing, not per keystroke: each search
  // scans the whole transcript on the GUI's control connection.
  it('coalesces a burst of keystrokes into one search for the final query', () => {
    vi.useFakeTimers();
    try {
      bufferType = 'alternate';
      act(() => mod.openFindBox(SID));
      searchTranscript.mockClear();
      for (const q of ['n', 'ne', 'nee', 'need', 'needl', 'needle']) {
        act(() => mod.runQuery(SID, q));
        act(() => {
          vi.advanceTimersByTime(mod.TYPE_DEBOUNCE_MS / 2);
        });
      }
      expect(searchTranscript).not.toHaveBeenCalled();
      // The box reflects the typing at once, even before the search runs.
      expect(find()?.query).toBe('needle');
      act(() => {
        vi.advanceTimersByTime(mod.TYPE_DEBOUNCE_MS);
      });
      expect(searchTranscript).toHaveBeenCalledTimes(1);
      expect(searchTranscript).toHaveBeenCalledWith(
        SID,
        'needle',
        expect.any(Number),
        expect.any(Number),
      );
    } finally {
      vi.useRealTimers();
    }
  });
  it('does nothing when the buffer change does not change the source', () => {
    act(() => mod.openFindBox(SID));
    searchTranscript.mockClear();
    act(() => mod.onBufferChange(SID, 'normal'));
    expect(searchTranscript).not.toHaveBeenCalled();
  });
});

describe('the addon is never searched on the alternate buffer', () => {
  // This is criterion 11's real test. It pins the invariant that makes
  // the poisoning unreachable, rather than trying to observe the
  // poisoning itself — that only manifests after the app exits, so it
  // cannot be seen in-process.
  it('does not call findNext/findPrevious while the alt buffer is active', () => {
    bufferType = 'alternate';
    act(() => mod.openFindBox(SID));
    act(() => mod.runQuery(SID, 'needle'));
    act(() => mod.stepMatch(SID, 1));
    act(() => mod.stepMatch(SID, -1));

    expect(searchNewest).not.toHaveBeenCalled();
    expect(searchNext).not.toHaveBeenCalled();
    expect(searchPrev).not.toHaveBeenCalled();
  });

  it('does call the addon on a normal buffer', () => {
    act(() => mod.openFindBox(SID));
    act(() => mod.runQuery(SID, 'needle'));
    expect(searchNewest).toHaveBeenCalledWith('needle');
  });
});

describe('buffer-source searching', () => {
  it('updates the count on every keystroke, with no submit', () => {
    act(() => mod.openFindBox(SID));
    act(() => mod.runQuery(SID, 'n'));
    act(() => mod.runQuery(SID, 'ne'));
    // Every keystroke restarts from the bottom, so a new query always
    // lands on its newest match.
    expect(searchNewest).toHaveBeenCalledTimes(2);
    expect(find()?.total).toBe(3);
  });

  it('clears the terminal search when the query empties', () => {
    act(() => mod.openFindBox(SID));
    act(() => mod.runQuery(SID, 'needle'));
    act(() => mod.runQuery(SID, ''));
    expect(clearSearch).toHaveBeenCalled();
    expect(find()?.total).toBe(0);
  });

  // Search runs bottom to top: the first match is the newest, "next"
  // goes UP to an older one (findPrevious), "previous" goes down.
  it('starts at the newest match and steps upward to older ones', () => {
    act(() => mod.openFindBox(SID));
    act(() => mod.runQuery(SID, 'needle'));
    expect(find()?.index).toBe(0); // top-down 2 of 3 -> newest-first 0

    act(() => mod.stepMatch(SID, 1));
    expect(searchPrev).toHaveBeenCalledTimes(1);
    expect(searchNext).not.toHaveBeenCalled();
    expect(find()?.index).toBe(1); // top-down 1 of 3 -> newest-first 1

    act(() => mod.stepMatch(SID, -1));
    expect(searchNext).toHaveBeenCalledTimes(1);
  });

  // The addon re-runs the search itself on new output and reports it;
  // that report is what keeps the count live.
  it('updates the count from the addon’s own refreshes', () => {
    act(() => mod.openFindBox(SID));
    act(() => mod.runQuery(SID, 'needle'));
    act(() => mod.onFindResults(SID, { resultIndex: 4, resultCount: 5 }));
    expect(find()?.total).toBe(5);
    expect(find()?.index).toBe(0); // bottom-most of 5
  });

  it('ignores addon reports when the box shows the transcript', () => {
    bufferType = 'alternate';
    act(() => mod.openFindBox(SID));
    act(() => mod.runQuery(SID, 'needle'));
    act(() => mod.onFindResults(SID, { resultIndex: 0, resultCount: 9 }));
    expect(find()?.total).not.toBe(9);
  });
});

describe('transcript responses', () => {
  function openTranscript() {
    bufferType = 'alternate';
    act(() => mod.openFindBox(SID));
    act(() => mod.runQuery(SID, 'needle'));
  }

  it('applies matches for the current query', () => {
    openTranscript();
    act(() =>
      mod.applyMatches({
        session_id: SID,
        query: 'needle',
        available: true,
        total: 2,
        total_lines: 40,
        matches: [
          { line: 5, col: 0, len: 6 },
          { line: 9, col: 2, len: 6 },
        ],
      }),
    );
    expect(find()?.total).toBe(2);
    expect(find()?.matches).toHaveLength(2);
    // The window around the first match is requested straight away.
    expect(getTranscriptLines).toHaveBeenCalledWith(
      SID,
      expect.any(Number),
      5,
      expect.any(Number),
      expect.any(Number),
      expect.any(Number),
    );
  });

  // Searches run per keystroke AND are re-issued on session output, so
  // two can be in flight; a late older response must not repaint.
  it('discards a matches response for a stale query', () => {
    openTranscript();
    act(() =>
      mod.applyMatches({
        session_id: SID,
        query: 'needle',
        available: true,
        total: 2,
        matches: [{ line: 1, col: 0, len: 6 }],
      }),
    );
    expect(find()?.total).toBe(2);

    act(() =>
      mod.applyMatches({
        session_id: SID,
        query: 'need', // an older keystroke, landing late
        available: true,
        total: 99,
        matches: [],
      }),
    );
    expect(find()?.total).toBe(2);
  });

  it('renders the unavailable state rather than an empty list', () => {
    bufferType = 'alternate';
    act(() => mod.openFindBox(SID));
    act(() =>
      mod.applyMatches({
        session_id: SID,
        query: '',
        available: false,
        reason: 'unsupported_agent',
      }),
    );
    const { container } = renderBox();
    expect(container.querySelector('[data-find-unavailable]')).not.toBeNull();
    expect(container.textContent).toContain('No searchable history');
  });

  // Window responses carry no query, and two differing only in centre
  // share a revision — so reqId is the only thing that distinguishes
  // them.
  it('discards a window response with a stale reqId', () => {
    openTranscript();
    act(() =>
      mod.applyMatches({
        session_id: SID,
        query: 'needle',
        available: true,
        total: 2,
        total_lines: 40,
        matches: [
          { line: 5, col: 0, len: 6 },
          { line: 30, col: 0, len: 6 },
        ],
      }),
    );
    const current = find()?.reqId ?? 0;

    act(() =>
      mod.applyLines({
        session_id: SID,
        req_id: current - 1, // an earlier window, landing late
        start: 900,
        available: true,
        lines: [{ line: 900, text: 'stale' }],
      }),
    );
    expect(find()?.lineStart).not.toBe(900);

    act(() =>
      mod.applyLines({
        session_id: SID,
        req_id: current,
        start: 1,
        available: true,
        lines: [{ line: 1, text: 'fresh' }],
      }),
    );
    expect(find()?.lineStart).toBe(1);
  });

  it('centres the window on the active match when stepping', () => {
    openTranscript();
    act(() =>
      mod.applyMatches({
        session_id: SID,
        query: 'needle',
        available: true,
        total: 2,
        total_lines: 40,
        matches: [
          { line: 5, col: 0, len: 6 },
          { line: 30, col: 0, len: 6 },
        ],
      }),
    );
    getTranscriptLines.mockClear();
    act(() => mod.stepMatch(SID, 1));
    expect(find()?.index).toBe(1);
    expect(getTranscriptLines).toHaveBeenCalledWith(
      SID,
      expect.any(Number),
      30,
      expect.any(Number),
      expect.any(Number),
      expect.any(Number),
    );
  });
});

describe('newest-first transcript navigation', () => {
  function openWith(matches: { line: number; col: number; len: number }[]) {
    bufferType = 'alternate';
    act(() => mod.openFindBox(SID));
    act(() => mod.runQuery(SID, 'needle'));
    act(() =>
      mod.applyMatches({
        session_id: SID,
        query: 'needle',
        available: true,
        total: matches.length,
        total_lines: 100,
        matches,
      }),
    );
  }

  it('starts on the newest match (the daemon sends newest first)', () => {
    openWith([
      { line: 90, col: 0, len: 6 },
      { line: 10, col: 0, len: 6 },
    ]);
    expect(find()?.index).toBe(0);
    expect(getTranscriptLines).toHaveBeenLastCalledWith(
      SID,
      expect.any(Number),
      90,
      expect.any(Number),
      expect.any(Number),
      expect.any(Number),
    );
  });

  // New output prepends newer matches. Keeping the index would silently
  // move the user to a different match; they must stay on theirs.
  it('stays on the match being read when a refresh adds newer ones', () => {
    openWith([
      { line: 50, col: 0, len: 6 },
      { line: 10, col: 2, len: 6 },
    ]);
    act(() => mod.stepMatch(SID, 1)); // older: line 10
    expect(find()?.matches[find()?.index ?? -1]?.line).toBe(10);

    act(() =>
      mod.applyMatches({
        session_id: SID,
        query: 'needle',
        available: true,
        total: 3,
        total_lines: 120,
        matches: [
          { line: 110, col: 0, len: 6 }, // arrived while reading
          { line: 50, col: 0, len: 6 },
          { line: 10, col: 2, len: 6 },
        ],
      }),
    );
    expect(find()?.index).toBe(2);
    expect(find()?.matches[2]?.line).toBe(10);
  });

  it('a new query starts again at the newest', () => {
    openWith([
      { line: 50, col: 0, len: 6 },
      { line: 10, col: 0, len: 6 },
    ]);
    act(() => mod.stepMatch(SID, 1));
    act(() => mod.runQuery(SID, 'other'));
    act(() =>
      mod.applyMatches({
        session_id: SID,
        query: 'other',
        available: true,
        total: 2,
        matches: [
          { line: 70, col: 0, len: 5 },
          { line: 10, col: 0, len: 5 },
        ],
      }),
    );
    expect(find()?.index).toBe(0);
  });
});

describe('scroll-loaded history', () => {
  const ln = (a: number, b: number) =>
    Array.from({ length: b - a }, (_, i) => ({
      line: a + i,
      text: `l${a + i}`,
    }));

  it('merges older lines above, without duplicates', () => {
    const r = mod.mergeLines(
      { lines: ln(100, 200), lineStart: 100 },
      ln(0, 120),
    );
    expect(r.lastLoad).toBe('prepend');
    expect(r.lineStart).toBe(0);
    expect(r.lines).toHaveLength(200);
    expect(r.lines.map((l) => l.line)).toEqual(ln(0, 200).map((l) => l.line));
  });

  it('merges newer lines below, without duplicates', () => {
    const r = mod.mergeLines({ lines: ln(0, 100), lineStart: 0 }, ln(80, 150));
    expect(r.lastLoad).toBe('append');
    expect(r.lines.map((l) => l.line)).toEqual(ln(0, 150).map((l) => l.line));
  });

  // Scrolling far back must not put a whole transcript in the DOM: the
  // end the reader is moving away from is trimmed.
  it('trims the far end past the cap', () => {
    const cap = mod.MAX_LOADED_LINES;
    const up = mod.mergeLines(
      { lines: ln(1000, 1000 + cap), lineStart: 1000 },
      ln(800, 1000),
    );
    expect(up.lines).toHaveLength(cap);
    expect(up.lineStart).toBe(800); // kept the new top, trimmed the bottom
    const down = mod.mergeLines(
      { lines: ln(0, cap), lineStart: 0 },
      ln(cap, cap + 200),
    );
    expect(down.lines).toHaveLength(cap);
    expect(down.lineStart).toBe(200); // kept the new bottom, trimmed the top
  });

  function openLoaded() {
    bufferType = 'alternate';
    act(() => mod.openFindBox(SID));
    act(() =>
      mod.applyMatches({
        session_id: SID,
        query: '',
        available: true,
        total_lines: 1000,
      }),
    );
    act(() =>
      mod.applyLines({
        session_id: SID,
        req_id: find()?.reqId,
        start: 800,
        total_lines: 1000,
        available: true,
        lines: ln(800, 1000),
      }),
    );
  }

  it('requests exactly the block above the loaded range', () => {
    openLoaded();
    getTranscriptLines.mockClear();
    act(() => mod.extendLines(SID, 'up'));
    // centre 700 with count 200 is the window [600, 800).
    expect(getTranscriptLines).toHaveBeenCalledWith(
      SID,
      expect.any(Number),
      700,
      200,
      expect.any(Number),
      expect.any(Number),
    );
  });

  it('keeps at most one extension in flight', () => {
    openLoaded();
    getTranscriptLines.mockClear();
    act(() => mod.extendLines(SID, 'up'));
    act(() => mod.extendLines(SID, 'up'));
    expect(getTranscriptLines).toHaveBeenCalledTimes(1);
  });

  it('does not extend past either end', () => {
    openLoaded();
    getTranscriptLines.mockClear();
    act(() => mod.extendLines(SID, 'down')); // already at the last line
    expect(getTranscriptLines).not.toHaveBeenCalled();
  });

  // A new search replaces the range; an extension still in flight
  // belongs to the old one and must not be merged into the new.
  it('drops an extension orphaned by a replace', () => {
    openLoaded();
    act(() => mod.extendLines(SID, 'up'));
    const staleExtend = find()?.extendReqId ?? 0;
    act(() => mod.runQuery(SID, 'l5'));
    act(() =>
      mod.applyMatches({
        session_id: SID,
        query: 'l5',
        available: true,
        total: 1,
        total_lines: 1000,
        matches: [{ line: 5, col: 0, len: 2 }],
      }),
    );
    act(() =>
      mod.applyLines({
        session_id: SID,
        req_id: staleExtend,
        start: 600,
        available: true,
        lines: ln(600, 800),
      }),
    );
    expect(find()?.lines.some((l) => l.line === 600)).toBe(false);
  });

  // With no query, a live refresh only raises the line count: replacing
  // the window would yank someone reading history back to the end.
  it('a no-query live refresh does not replace the loaded range', () => {
    openLoaded();
    getTranscriptLines.mockClear();
    act(() =>
      mod.applyMatches({
        session_id: SID,
        query: '',
        available: true,
        total_lines: 1010,
      }),
    );
    expect(find()?.totalLines).toBe(1010);
    expect(getTranscriptLines).not.toHaveBeenCalled();
    expect(find()?.lineStart).toBe(800);
  });
});

describe('review round 2', () => {
  function openTx(query = 'needle') {
    bufferType = 'alternate';
    act(() => mod.openFindBox(SID));
    if (query) {
      vi.useFakeTimers();
      try {
        act(() => mod.runQuery(SID, query));
        act(() => {
          vi.advanceTimersByTime(mod.TYPE_DEBOUNCE_MS);
        });
      } finally {
        vi.useRealTimers();
      }
    }
  }

  // A live refresh re-sends the SAME query, so only the request id can
  // tell an older reply from a newer one.
  it('discards a reply to an older request with the same query', () => {
    openTx();
    const latest = find()?.searchReqId ?? 0;
    act(() =>
      mod.applyMatches({
        session_id: SID,
        req_id: latest,
        query: 'needle',
        available: true,
        total: 3,
        matches: [
          { line: 9, col: 0, len: 6 },
          { line: 5, col: 0, len: 6 },
          { line: 1, col: 0, len: 6 },
        ],
      }),
    );
    act(() =>
      mod.applyMatches({
        session_id: SID,
        req_id: latest - 1, // an older search for the same query, landing late
        query: 'needle',
        available: true,
        total: 1,
        matches: [{ line: 1, col: 0, len: 6 }],
      }),
    );
    expect(find()?.total).toBe(3);
  });

  it('clears a stale unavailable state when a refresh succeeds', () => {
    openTx('');
    act(() =>
      mod.applyLines({
        session_id: SID,
        req_id: find()?.reqId,
        start: 0,
        total_lines: 10,
        available: true,
        lines: [{ line: 0, text: 'x' }],
      }),
    );
    // Something reported the transcript unavailable...
    act(() => {
      appStore.setState((s) => {
        const tc = new Map(s.tileChrome);
        const cur = tc.get(SID);
        if (cur?.find)
          tc.set(SID, {
            ...cur,
            find: { ...cur.find, reason: 'no_transcript_file' },
          });
        return { tileChrome: tc };
      });
    });
    // ...then a no-query live refresh finds it available again.
    act(() =>
      mod.applyMatches({
        session_id: SID,
        req_id: find()?.searchReqId,
        query: '',
        available: true,
        total_lines: 12,
      }),
    );
    expect(find()?.reason).toBe('');
  });

  it('stops extending once an extension reports the transcript unavailable', () => {
    openTx('');
    act(() =>
      mod.applyLines({
        session_id: SID,
        req_id: find()?.reqId,
        start: 800,
        total_lines: 1000,
        available: true,
        lines: Array.from({ length: 200 }, (_, i) => ({
          line: 800 + i,
          text: 'x',
        })),
      }),
    );
    act(() => mod.extendLines(SID, 'up'));
    act(() =>
      mod.applyLines({
        session_id: SID,
        req_id: find()?.extendReqId,
        available: false,
        reason: 'no_transcript_file',
      }),
    );
    getTranscriptLines.mockClear();
    act(() => mod.extendLines(SID, 'up'));
    act(() => mod.extendLines(SID, 'down'));
    expect(getTranscriptLines).not.toHaveBeenCalled();
  });

  // The window request names the active match, so a long line holding it
  // comes back sliced around the match rather than cut before it.
  it('passes the active match as the window focus', () => {
    openTx();
    getTranscriptLines.mockClear();
    act(() =>
      mod.applyMatches({
        session_id: SID,
        req_id: find()?.searchReqId,
        query: 'needle',
        available: true,
        total: 1,
        total_lines: 40,
        matches: [{ line: 7, col: 5123, len: 6 }],
      }),
    );
    expect(getTranscriptLines).toHaveBeenLastCalledWith(
      SID,
      expect.any(Number),
      7,
      expect.any(Number),
      7,
      5123,
    );
  });

  it('highlights within a sliced line by rebasing on its offset', () => {
    openTx();
    act(() =>
      mod.applyMatches({
        session_id: SID,
        req_id: find()?.searchReqId,
        query: 'needle',
        available: true,
        total: 1,
        total_lines: 1,
        matches: [{ line: 0, col: 5000, len: 6 }],
      }),
    );
    act(() =>
      mod.applyLines({
        session_id: SID,
        req_id: find()?.reqId,
        start: 0,
        available: true,
        // The slice begins 4990 units into the line.
        lines: [
          {
            line: 0,
            text: 'xxxxxxxxxxneedleyyyy',
            offset: 4990,
            truncated: true,
          },
        ],
      }),
    );
    const { container } = renderBox();
    expect(container.querySelector('.hv-find-hit')?.textContent).toBe('needle');
    // A leading ellipsis marks that the line was cut before the slice.
    expect(
      container
        .querySelector('[data-find-line="0"]')
        ?.textContent?.startsWith('…'),
    ).toBe(true);
  });
});

describe('re-search on session output', () => {
  // Criterion 8's GUI half: text arriving while the box is open becomes
  // findable without reopening it. The Go cache tests cannot reach this.
  it('re-issues the current query after the debounce', async () => {
    vi.useFakeTimers();
    try {
      bufferType = 'alternate';
      act(() => mod.openFindBox(SID));
      act(() => mod.runQuery(SID, 'needle'));
      searchTranscript.mockClear();

      act(() => mod.onSessionOutput(SID));
      expect(searchTranscript).not.toHaveBeenCalled(); // debounced

      await act(async () => {
        vi.advanceTimersByTime(500);
      });
      expect(searchTranscript).toHaveBeenCalledWith(
        SID,
        'needle',
        expect.any(Number),
        expect.any(Number),
      );
    } finally {
      vi.useRealTimers();
    }
  });

  // The agent writes its transcript record when a message FINISHES,
  // which can land after output goes quiet — so there is a second,
  // later refresh.
  it('refreshes again once output settles', async () => {
    vi.useFakeTimers();
    try {
      bufferType = 'alternate';
      act(() => mod.openFindBox(SID));
      act(() => mod.runQuery(SID, 'needle'));
      // Let the typing debounce fire first, so only refreshes are counted.
      act(() => {
        vi.advanceTimersByTime(mod.TYPE_DEBOUNCE_MS);
      });
      searchTranscript.mockClear();

      act(() => mod.onSessionOutput(SID));
      await act(async () => {
        vi.advanceTimersByTime(300);
      });
      expect(searchTranscript).toHaveBeenCalledTimes(1);
      await act(async () => {
        vi.advanceTimersByTime(1500);
      });
      expect(searchTranscript).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  // Buffer mode refreshes through the addon's own re-search; a transcript
  // request there would be wasted work against the wrong source.
  it('does not request the transcript for a buffer-mode box', async () => {
    vi.useFakeTimers();
    try {
      act(() => mod.openFindBox(SID));
      act(() => mod.runQuery(SID, 'needle'));
      searchTranscript.mockClear();
      act(() => mod.onSessionOutput(SID));
      await act(async () => {
        vi.advanceTimersByTime(3000);
      });
      expect(searchTranscript).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it('does not re-search when the box has no query', async () => {
    vi.useFakeTimers();
    try {
      act(() => mod.openFindBox(SID));
      searchNext.mockClear();
      act(() => mod.onSessionOutput(SID));
      await act(async () => {
        vi.advanceTimersByTime(500);
      });
      expect(searchNext).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('the box UI', () => {
  it('focuses its input on mount, with no click', () => {
    act(() => mod.openFindBox(SID));
    const { container } = renderBox();
    const input = container.querySelector(
      '[data-find-input]',
    ) as HTMLInputElement;
    expect(document.activeElement).toBe(input);
  });

  // macOS autocorrect / autocapitalize would rewrite a search term
  // under the user's cursor; a query is not prose.
  it('disables autocorrect, autocapitalize and spellcheck on the input', () => {
    act(() => mod.openFindBox(SID));
    const { container } = renderBox();
    const input = container.querySelector(
      '[data-find-input]',
    ) as HTMLInputElement;
    expect(input.getAttribute('autocorrect')).toBe('off');
    expect(input.getAttribute('autocapitalize')).toBe('off');
    expect(input.getAttribute('autocomplete')).toBe('off');
    expect(input.getAttribute('spellcheck')).toBe('false');
  });

  it('renders the count', () => {
    act(() => mod.openFindBox(SID));
    act(() => mod.runQuery(SID, 'needle'));
    const { container } = renderBox();
    expect(container.querySelector('[data-find-count]')?.textContent).toBe(
      '1/3',
    );
  });

  it('closes on Escape from the input', async () => {
    act(() => mod.openFindBox(SID));
    const { container } = renderBox();
    const input = container.querySelector(
      '[data-find-input]',
    ) as HTMLInputElement;
    act(() => {
      input.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      );
    });
    expect(find()).toBeNull();
    // Focus goes back to the terminal only after the key event is done,
    // so no engine can re-target the Escape at the session.
    expect(focusActiveTerm).not.toHaveBeenCalled();
    await new Promise((r) => setTimeout(r, 0));
    expect(focusActiveTerm).toHaveBeenCalledTimes(1);
  });

  // Escape reaching an agent interrupts it; the box consumes it.
  it('does not let Escape propagate past the box', () => {
    act(() => mod.openFindBox(SID));
    const { container } = renderBox();
    const seen = vi.fn();
    document.addEventListener('keydown', seen);
    try {
      const input = container.querySelector(
        '[data-find-input]',
      ) as HTMLInputElement;
      act(() => {
        input.dispatchEvent(
          new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
        );
      });
      expect(seen).not.toHaveBeenCalled();
    } finally {
      document.removeEventListener('keydown', seen);
    }
  });

  // The find-field convention: the chord pressed in the box selects the
  // query so typing replaces it. It never closes the box.
  it('selects the query when the find chord is pressed in the input', () => {
    act(() => mod.openFindBox(SID));
    act(() => mod.runQuery(SID, 'needle'));
    const { container } = renderBox();
    const input = container.querySelector(
      '[data-find-input]',
    ) as HTMLInputElement;
    input.setSelectionRange(input.value.length, input.value.length);
    // jsdom reports a non-mac platform, so the chord is Ctrl+Shift+F.
    act(() => {
      input.dispatchEvent(
        new KeyboardEvent('keydown', {
          key: 'F',
          code: 'KeyF',
          ctrlKey: true,
          shiftKey: true,
          bubbles: true,
        }),
      );
    });
    expect(find()).not.toBeNull();
    expect(input.selectionStart).toBe(0);
    expect(input.selectionEnd).toBe('needle'.length);
  });

  it('opening an already-open box refocuses it rather than stacking or closing', () => {
    act(() => mod.openFindBox(SID));
    const first = find();
    act(() => mod.openFindBox(SID));
    expect(find()).toBe(first);
  });

  it('closes on the close control', () => {
    act(() => mod.openFindBox(SID));
    const { container } = renderBox();
    const close = container.querySelector(
      '[data-find-close]',
    ) as HTMLButtonElement;
    act(() => close.click());
    expect(find()).toBeNull();
    expect(focusActiveTerm).toHaveBeenCalled();
  });

  it('highlights matches inside a transcript line', () => {
    bufferType = 'alternate';
    act(() => mod.openFindBox(SID));
    act(() => mod.runQuery(SID, 'needle'));
    act(() =>
      mod.applyMatches({
        session_id: SID,
        query: 'needle',
        available: true,
        total: 1,
        total_lines: 1,
        matches: [{ line: 0, col: 2, len: 6 }],
      }),
    );
    act(() =>
      mod.applyLines({
        session_id: SID,
        req_id: find()?.reqId ?? 0,
        start: 0,
        available: true,
        lines: [{ line: 0, role: 'assistant', text: 'a needle here' }],
      }),
    );
    const { container } = renderBox();
    const hit = container.querySelector('.hv-find-hit');
    expect(hit?.textContent).toBe('needle');
  });
});
