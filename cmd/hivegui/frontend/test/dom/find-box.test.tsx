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
    expect(searchTranscript).toHaveBeenCalledWith(SID, '', expect.any(Number));
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
    act(() => mod.openFindBox(SID));
    act(() => mod.runQuery(SID, 'needle'));
    expect(find()?.source).toBe('buffer');

    bufferType = 'alternate';
    act(() => mod.onBufferChange(SID, 'alternate'));

    expect(find()?.source).toBe('transcript');
    // The box stays open and re-runs the query against the new source.
    expect(find()).not.toBeNull();
    expect(searchTranscript).toHaveBeenCalledWith(
      SID,
      'needle',
      expect.any(Number),
    );
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

  it('closes on Escape from the input', () => {
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
    expect(focusActiveTerm).toHaveBeenCalled();
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
