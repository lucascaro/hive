import { describe, it, expect } from 'vitest';
import {
  createScrollTrace, SCROLL_TRACE_CAP, classifyViewportMove,
} from '../../src/lib/scroll-debug.js';

describe('createScrollTrace', () => {
  it('records nothing when disabled', () => {
    const { rec, ring } = createScrollTrace({ enabled: false });
    rec('resize', { cols: 80 });
    expect(ring).toEqual([]);
    expect(rec.enabled).toBe(false);
  });

  it('records tag, payload and rounded injected-clock timestamp when enabled', () => {
    let t = 0;
    const { rec, ring } = createScrollTrace({ enabled: true, now: () => t });
    t = 12.6;
    rec('resize', { cols: 80 });
    expect(ring).toEqual([{ t: 13, tag: 'resize', cols: 80 }]);
    expect(rec.enabled).toBe(true);
  });

  it('bounds the ring at cap, dropping the oldest entries in place', () => {
    const { rec, ring } = createScrollTrace({ enabled: true, now: () => 0, cap: 3 });
    for (let i = 0; i < 5; i++) rec('e', { i });
    // Same array object must stay bounded — window.__hive_scrolltrace
    // holds a direct reference to it, so a reassignment would orphan
    // the dump handle.
    expect(ring.length).toBe(3);
    expect(ring.map((e) => e.i)).toEqual([2, 3, 4]);
  });

  it('default cap is SCROLL_TRACE_CAP', () => {
    expect(SCROLL_TRACE_CAP).toBe(2000);
  });
});

describe('classifyViewportMove', () => {
  // The jump-up bug moves the viewport UP (ydisp decreases) with no
  // user gesture behind it. Downward / no-op moves are never the bug.
  it('returns null when the viewport did not move up', () => {
    expect(classifyViewportMove({ from: 10, to: 10, lastUserScrollTs: 0, now: 0 })).toBe(null);
    expect(classifyViewportMove({ from: 10, to: 20, lastUserScrollTs: 0, now: 0 })).toBe(null);
  });

  it('labels an up-move within the user grace window as user-up', () => {
    // User wheeled 100ms ago, then the viewport moved up → that's them.
    expect(classifyViewportMove({
      from: 100, to: 40, lastUserScrollTs: 900, now: 1000, userGraceMs: 250,
    })).toBe('user-up');
  });

  it('treats the grace boundary as inclusive (still user-up)', () => {
    expect(classifyViewportMove({
      from: 100, to: 40, lastUserScrollTs: 750, now: 1000, userGraceMs: 250,
    })).toBe('user-up');
  });

  it('labels an up-move with no recent user gesture as auto-up (the suspicious case)', () => {
    expect(classifyViewportMove({
      from: 100, to: 40, lastUserScrollTs: 100, now: 1000, userGraceMs: 250,
    })).toBe('auto-up');
  });

  it('labels an up-move as auto-up when no user gesture was ever recorded', () => {
    expect(classifyViewportMove({ from: 100, to: 40, lastUserScrollTs: null, now: 1000 })).toBe('auto-up');
    expect(classifyViewportMove({ from: 100, to: 40, now: 1000 })).toBe('auto-up');
  });
});
