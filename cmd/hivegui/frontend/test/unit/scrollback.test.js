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
  function makeSt() {
    return {
      term: {
        reset: vi.fn(),
        scrollToBottom: vi.fn(),
      },
      decoder: new TextDecoder('utf-8'),
    };
  }

  it('begin event calls term.reset() and refreshes decoder', () => {
    const st = makeSt();
    const beforeDecoder = st.decoder;
    const ok = handleScrollbackEvent(st, 'scrollback_replay_begin');
    expect(ok).toBe(true);
    expect(st.term.reset).toHaveBeenCalledTimes(1);
    // decoder should be a fresh instance (not the same object)
    expect(st.decoder).not.toBe(beforeDecoder);
  });

  it('done event scrolls to bottom by default (no _replayWantsBottom)', () => {
    const st = makeSt();
    const ok = handleScrollbackEvent(st, 'scrollback_replay_done');
    expect(ok).toBe(true);
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);
    expect(st.term.reset).not.toHaveBeenCalled();
  });

  it('done event preserves position when _replayWantsBottom === false and clears the flag', () => {
    const st = makeSt();
    st._replayWantsBottom = false;
    const ok = handleScrollbackEvent(st, 'scrollback_replay_done');
    expect(ok).toBe(true);
    expect(st.term.scrollToBottom).not.toHaveBeenCalled();
    expect(st._replayWantsBottom).toBeUndefined();
  });

  it('done event snaps when _replayWantsBottom === true and clears the flag', () => {
    const st = makeSt();
    st._replayWantsBottom = true;
    const ok = handleScrollbackEvent(st, 'scrollback_replay_done');
    expect(ok).toBe(true);
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);
    expect(st._replayWantsBottom).toBeUndefined();
  });

  it('unknown event kinds are no-ops', () => {
    const st = makeSt();
    const ok = handleScrollbackEvent(st, 'something_else');
    expect(ok).toBe(false);
    expect(st.term.reset).not.toHaveBeenCalled();
    expect(st.term.scrollToBottom).not.toHaveBeenCalled();
  });

  it('null / undefined st is a no-op (no throw)', () => {
    expect(handleScrollbackEvent(null, 'scrollback_replay_begin')).toBe(false);
    expect(handleScrollbackEvent(undefined, 'scrollback_replay_begin')).toBe(false);
    expect(handleScrollbackEvent({}, 'scrollback_replay_begin')).toBe(false);
  });

  it('begin runs before any subsequent write would matter', () => {
    // Document the ordering invariant: a caller doing
    //   handleScrollbackEvent(st, 'scrollback_replay_begin')
    //   st.term.write(replayBytes)
    // observes reset() in call-1 before write() in call-2. Trivially
    // true given JS is single-threaded; this test exists to lock the
    // semantic in code so a future refactor that makes the handler
    // async would have to update this test.
    const st = makeSt();
    st.term.write = vi.fn();
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    st.term.write('hello');
    expect(st.term.reset.mock.invocationCallOrder[0]).toBeLessThan(
      st.term.write.mock.invocationCallOrder[0]
    );
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
