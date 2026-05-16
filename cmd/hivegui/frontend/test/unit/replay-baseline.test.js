import { describe, it, expect, vi } from 'vitest';
import {
  applyRebaseline,
  shouldRequestReplay,
  REPLAY_COL_THRESHOLD,
} from '../../src/lib/scrollback.js';

// Regression coverage for #208:
//   R1 — first grid-mode entry after restart triggers a spurious replay
//        because the baseline was captured against an xterm default (80)
//        while the actual fitted cols differ by >= REPLAY_COL_THRESHOLD.
//   R2 — minimizing a tile in a 3-tile grid reflows the remaining tiles;
//        ResizeObserver fires _onBodyResize with new cols against the
//        stale baseline, again crossing the threshold and firing replay.
// applyRebaseline is the pure helper backing SessionTerm.rebaselineReplayCols.
// These tests pin the contract so the fix can't quietly regress.

function makeSt({ cols = 80, baseline, timer } = {}) {
  return {
    term: { cols },
    _replayBaselineCols: baseline,
    _replayTimer: timer ?? 0,
  };
}

describe('applyRebaseline', () => {
  it('R1: anchors baseline to current term.cols on first-attach (no spurious replay against default 80)', () => {
    // Simulates: tile attached in grid mode; fitted to 84 cols, but the
    // baseline init in _onBodyResize would have captured prevCols=80.
    const st = makeSt({ cols: 84, baseline: 80 });
    applyRebaseline(st);
    expect(st._replayBaselineCols).toBe(84);
    // After rebaseline, the very next resize at the same width must not
    // cross the >=4 threshold.
    expect(shouldRequestReplay(st._replayBaselineCols, st.term.cols)).toBe(false);
  });

  it('R2: after a >=4 col delta from minimize/restore, rebaseline prevents the next _onBodyResize from firing a replay', () => {
    // Pre-minimize: tile rendered at 60 cols → baseline = 60.
    // Minimize one tile → remaining tiles reflow to 80 cols.
    // Without rebaseline, shouldRequestReplay(60, 80) === true.
    expect(shouldRequestReplay(60, 80)).toBe(true);
    // With rebaseline applied after layout settles, baseline tracks new cols
    // so the inert reflow doesn't trip the threshold.
    const st = makeSt({ cols: 80, baseline: 60 });
    applyRebaseline(st);
    expect(shouldRequestReplay(st._replayBaselineCols, st.term.cols)).toBe(false);
  });

  it('clears a pending debounced replay timer so a queued spurious replay does not fire', () => {
    const clearTimer = vi.fn();
    const st = makeSt({ cols: 100, baseline: 60, timer: 42 });
    applyRebaseline(st, clearTimer);
    expect(clearTimer).toHaveBeenCalledWith(42);
    expect(st._replayTimer).toBe(0);
  });

  it('is a no-op on the timer when none is pending', () => {
    const clearTimer = vi.fn();
    const st = makeSt({ cols: 100, baseline: 60, timer: 0 });
    applyRebaseline(st, clearTimer);
    expect(clearTimer).not.toHaveBeenCalled();
    expect(st._replayBaselineCols).toBe(100);
  });

  it('does NOT swallow a legitimate pure-resize signal — only the baseline is moved, threshold logic continues to work for future resizes', () => {
    // After rebaseline, a subsequent real window resize that changes cols
    // by >= REPLAY_COL_THRESHOLD must still flag a replay. This is the
    // R-control case: the fix must not kill genuine resize replays.
    const st = makeSt({ cols: 80, baseline: 60 });
    applyRebaseline(st);
    // Now the user actually resizes the window: tile widens to 90.
    expect(shouldRequestReplay(st._replayBaselineCols, 90)).toBe(true);
    expect(REPLAY_COL_THRESHOLD).toBeLessThanOrEqual(10);
  });

  it('tolerates a missing term gracefully', () => {
    const st = { _replayBaselineCols: 60, _replayTimer: 0 };
    const out = applyRebaseline(st);
    expect(out).toBe(st);
    expect(st._replayBaselineCols).toBe(60);
  });
});
