import { describe, it, expect, vi } from 'vitest';
import {
  shouldRequestReplay,
  REPLAY_COL_THRESHOLD,
  REPLAY_DEBOUNCE_MS,
  handleScrollbackEvent,
  applyRebaseline,
} from '../../src/lib/scrollback.js';

describe('shouldRequestReplay', () => {
  it('returns true on grid → single (large widen)', () => {
    expect(shouldRequestReplay(40, 200)).toBe(true);
  });

  it('returns true on single → grid (large shrink)', () => {
    expect(shouldRequestReplay(200, 40)).toBe(true);
  });

  it('returns true exactly at the threshold', () => {
    expect(shouldRequestReplay(80, 80 + REPLAY_COL_THRESHOLD)).toBe(true);
    expect(shouldRequestReplay(80, 80 - REPLAY_COL_THRESHOLD)).toBe(true);
  });

  it('returns false below the threshold (kerning jitter)', () => {
    expect(shouldRequestReplay(80, 81)).toBe(false);
    expect(shouldRequestReplay(80, 83)).toBe(false);
    expect(shouldRequestReplay(80, 80)).toBe(false);
  });

  it('returns false when prevCols is missing (first measurement)', () => {
    expect(shouldRequestReplay(undefined, 80)).toBe(false);
    expect(shouldRequestReplay(0, 80)).toBe(false);
  });

  it('returns false when nextCols is zero (hidden tile)', () => {
    expect(shouldRequestReplay(80, 0)).toBe(false);
  });

  it('accepts a custom threshold', () => {
    expect(shouldRequestReplay(80, 82, 2)).toBe(true);
    expect(shouldRequestReplay(80, 81, 2)).toBe(false);
  });
});

describe('debounce timing constant', () => {
  it('is small enough for live use', () => {
    expect(REPLAY_DEBOUNCE_MS).toBeGreaterThan(0);
    expect(REPLAY_DEBOUNCE_MS).toBeLessThan(1000);
  });
});

describe('handleScrollbackEvent', () => {
  // Mock term with an xterm-like async write queue: write(data, cb)
  // enqueues; flush() "parses" entries in order, firing callbacks.
  // This models the property the handler now depends on — reset and
  // viewport placement are parse-ordered, not event-ordered.
  function makeSt({ baseY = 0, viewportY = 0 } = {}) {
    const queue = [];
    const order = [];
    const term = {
      buffer: { active: { baseY, viewportY } },
      reset: vi.fn(() => order.push('reset')),
      scrollToBottom: vi.fn(() => order.push('scrollToBottom')),
      scrollToLine: vi.fn((n) => order.push(`scrollToLine:${n}`)),
      write: vi.fn((data, cb) => {
        queue.push({ data, cb });
        if (data) order.push(`parse:${data}`);
      }),
    };
    const flush = () => {
      while (queue.length) {
        const entry = queue.shift();
        // A real parser would consume entry.data here; our `order`
        // log records data entries at enqueue time which is fine for
        // relative ordering because flush preserves queue order.
        entry.cb?.();
      }
    };
    return { st: { term, decoder: new TextDecoder('utf-8') }, flush, order, queue };
  }

  it('begin refreshes the decoder immediately (decode order is event order)', () => {
    const { st } = makeSt();
    const beforeDecoder = st.decoder;
    expect(handleScrollbackEvent(st, 'scrollback_replay_begin')).toBe(true);
    expect(st.decoder).not.toBe(beforeDecoder);
  });

  it('begin resets parse-ordered, not synchronously — backlog cannot repaint after the wipe', () => {
    const { st, flush } = makeSt();
    // Simulate codex-rate backlog already sitting in the queue.
    st.term.write('backlog-bytes');
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    // Replay bytes arrive after begin.
    st.term.write('replay-bytes');
    // Event time: nothing reset yet (queue not parsed).
    expect(st.term.reset).not.toHaveBeenCalled();
    flush();
    expect(st.term.reset).toHaveBeenCalledTimes(1);
    // The reset callback was enqueued after the backlog and before the
    // replay bytes: backlog parses, THEN reset, THEN replay paints.
    const calls = st.term.write.mock.calls.map((c) => c[0]);
    expect(calls).toEqual(['backlog-bytes', '', 'replay-bytes']);
  });

  it('begin captures the reader distance from bottom — at parse time, not event time', () => {
    const { st, flush } = makeSt({ baseY: 100, viewportY: 60 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    expect(st._replayPrevFromBottom).toBeUndefined(); // parse-ordered like the reset
    flush();
    expect(st._replayPrevFromBottom).toBe(40);
  });

  it('capture is parse-ordered: backlog parsed before the wipe counts toward the distance', () => {
    const { st, flush } = makeSt({ baseY: 100, viewportY: 60 });
    // Codex-rate backlog already queued at begin time; parsing it adds
    // 10 lines while the scrolled-up reader's viewportY stays put.
    st.term.write('backlog-bytes', () => { st.term.buffer.active.baseY = 110; });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    st._replayWantsBottom = false;
    // The replay re-streams everything, backlog included.
    st.term.write('replay-bytes', () => { st.term.buffer.active.baseY = 115; });
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    // d = 110 - 60 = 50 (backlog included); restore = 115 - 50 = 65.
    // An event-time capture would have measured 40 and restored to 75
    // — a backlog's-worth of lines below the reader's content.
    expect(st.term.scrollToLine).toHaveBeenCalledWith(65);
  });

  it('done snaps to bottom by default — after the queue is parsed', () => {
    const { st, flush } = makeSt();
    expect(handleScrollbackEvent(st, 'scrollback_replay_done')).toBe(true);
    expect(st.term.scrollToBottom).not.toHaveBeenCalled(); // not at event time
    flush();
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);
    expect(st.term.reset).not.toHaveBeenCalled();
  });

  it('done snaps when _replayWantsBottom === true and clears the flag', () => {
    const { st, flush } = makeSt();
    st._replayWantsBottom = true;
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);
    expect(st._replayWantsBottom).toBeUndefined();
  });

  it('done restores the reading position when _replayWantsBottom === false', () => {
    const { st, flush } = makeSt({ baseY: 100, viewportY: 60 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    st._replayWantsBottom = false;
    // The replay bytes parse after the reset and rebuild the buffer
    // with a new baseY — modeled as a queued write whose "parse"
    // bumps baseY, so the capture (queued at begin) still sees 100.
    st.term.write('replay-bytes', () => { st.term.buffer.active.baseY = 120; });
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToBottom).not.toHaveBeenCalled();
    expect(st.term.scrollToLine).toHaveBeenCalledWith(80); // 120 - 40
    expect(st._replayWantsBottom).toBeUndefined();
    expect(st._replayPrevFromBottom).toBeUndefined();
  });

  it('restore target clamps at 0 when history shrank', () => {
    const { st, flush } = makeSt({ baseY: 50, viewportY: 0 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    st._replayWantsBottom = false;
    // Rebuilt buffer is much shorter than the captured distance (50).
    st.term.write('replay-bytes', () => { st.term.buffer.active.baseY = 10; });
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToLine).toHaveBeenCalledWith(0);
  });

  it('falls back to synchronous behavior when term has no write()', () => {
    const st = {
      term: { reset: vi.fn(), scrollToBottom: vi.fn() },
      decoder: new TextDecoder('utf-8'),
    };
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    expect(st.term.reset).toHaveBeenCalledTimes(1);
    handleScrollbackEvent(st, 'scrollback_replay_done');
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);
  });

  it('unknown event kinds are no-ops', () => {
    const { st } = makeSt();
    expect(handleScrollbackEvent(st, 'something_else')).toBe(false);
    expect(st.term.reset).not.toHaveBeenCalled();
    expect(st.term.scrollToBottom).not.toHaveBeenCalled();
  });

  it('null / undefined st is a no-op (no throw)', () => {
    expect(handleScrollbackEvent(null, 'scrollback_replay_begin')).toBe(false);
    expect(handleScrollbackEvent(undefined, 'scrollback_replay_begin')).toBe(false);
    expect(handleScrollbackEvent({}, 'scrollback_replay_begin')).toBe(false);
  });
});

describe('applyRebaseline clears stale wants-bottom intent', () => {
  it('deletes _replayWantsBottom so a later replay-done does not read stale false', () => {
    const cleared = vi.fn();
    const st = {
      term: { cols: 120 },
      _replayBaselineCols: 80,
      _replayTimer: 42,
      _replayWantsBottom: false,
    };
    applyRebaseline(st, cleared);
    expect(cleared).toHaveBeenCalledWith(42);
    expect(st._replayBaselineCols).toBe(120);
    expect(st._replayTimer).toBe(0);
    expect(st._replayWantsBottom).toBeUndefined();
  });

  it('is a no-op on _replayWantsBottom when flag was unset', () => {
    const st = { term: { cols: 100 }, _replayBaselineCols: 80 };
    applyRebaseline(st, () => {});
    expect(st._replayWantsBottom).toBeUndefined();
  });
});

describe('shouldRequestReplay against baseline (debounce edge case)', () => {
  // Simulates main.js's baseline-relative debounce: compare *current*
  // cols against baseline-at-last-replay (not just-previous
  // measurement). The reviewer flagged r614 — 80→84→83 should NOT
  // trigger (final delta 3 < threshold 4). Conversely 80→90→89 SHOULD
  // trigger (final delta 9), even though the single 90→89 step is
  // sub-threshold.
  it('baseline-relative threshold catches multi-step crossings', () => {
    const baseline = 80;
    expect(shouldRequestReplay(baseline, 90)).toBe(true);
    expect(shouldRequestReplay(baseline, 89)).toBe(true);
    expect(shouldRequestReplay(baseline, 83)).toBe(false);
    expect(shouldRequestReplay(baseline, 84)).toBe(true);
  });
});
