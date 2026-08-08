import { describe, it, expect, vi } from 'vitest';
import {
  createScrollTrace,
  SCROLL_TRACE_CAP,
  TEE_TAGS,
  classifyViewportMove,
} from '../../src/lib/scroll-debug.js';

describe('createScrollTrace', () => {
  it('records nothing to the ring when disabled and no sink', () => {
    const { rec, ring } = createScrollTrace({ enabled: false });
    rec('resize', { cols: 80 });
    expect(ring).toEqual([]);
    expect(rec.enabled).toBe(false);
  });

  it('stays completely silent when disabled, even with a sink wired', () => {
    // The tracer's call sites sit in the scroll/resize hot paths, so a
    // normal (non-debug) run must cost nothing: no ring, no log tee, and
    // rec.enabled false so call sites don't even build their payloads.
    const sink = vi.fn();
    const { rec, ring } = createScrollTrace({ enabled: false, sink });
    expect(rec.enabled).toBe(false);
    rec('mode-snap', { view: 'grid-all' });
    expect(sink).not.toHaveBeenCalled();
    expect(ring).toEqual([]);
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
    const { rec, ring } = createScrollTrace({
      enabled: true,
      now: () => 0,
      cap: 3,
    });
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

  it('count() is a no-op when disabled', () => {
    const { count, counters } = createScrollTrace({ enabled: false });
    count('renderGrid');
    count('renderGrid');
    expect({ ...counters }).toEqual({});
  });

  it('count() accumulates per-name totals when enabled', () => {
    const { count, counters } = createScrollTrace({
      enabled: true,
      now: () => 0,
    });
    count('renderGrid');
    count('renderGrid');
    count('focusApply', 3);
    expect(counters.renderGrid).toBe(2);
    expect(counters.focusApply).toBe(3);
  });

  it('counters survive ring rotation (totals outlive the capped ring)', () => {
    const { rec, count, ring, counters } = createScrollTrace({
      enabled: true,
      now: () => 0,
      cap: 3,
    });
    for (let i = 0; i < 10; i++) {
      rec('render-grid', { i });
      count('renderGrid');
    }
    // Ring dropped the oldest, but the counter kept every increment.
    expect(ring.length).toBe(3);
    expect(counters.renderGrid).toBe(10);
  });

  it('tees whitelisted tags to the sink with the tag + JSON payload', () => {
    const sink = vi.fn();
    const { rec } = createScrollTrace({ enabled: true, now: () => 0, sink });
    rec('mode-snap', { view: 'grid-all' });
    expect(sink).toHaveBeenCalledTimes(1);
    expect(sink).toHaveBeenCalledWith('scroll mode-snap {"view":"grid-all"}');
  });

  it('does not tee non-whitelisted (high-rate) tags to the sink', () => {
    // `resize` is the important one: _onBodyResize fires per tile per frame
    // during a window/sidebar drag, so teeing it would bury the log.
    const sink = vi.fn();
    const { rec } = createScrollTrace({ enabled: true, now: () => 0, sink });
    rec('resize', { cols: 80 });
    rec('wheel', { deltaY: 3 });
    rec('heartbeat-stall', { gap: 900 });
    expect(sink).not.toHaveBeenCalled();
  });

  it('swallows a throwing sink so the trace path never throws', () => {
    const sink = vi.fn(() => {
      throw new Error('bridge down');
    });
    const { rec, ring } = createScrollTrace({
      enabled: true,
      now: () => 0,
      sink,
    });
    expect(() => rec('mode-snap', { view: 'grid-all' })).not.toThrow();
    // Ring still recorded it even though the tee threw.
    expect(ring).toHaveLength(1);
  });

  it('TEE_TAGS holds only per-event tags — never per-frame `resize`', () => {
    expect([...TEE_TAGS].sort()).toEqual([
      'mode-snap',
      'replay-request',
      'replay-restore',
      'replay-skip',
      'viewport-jump',
    ]);
    expect(TEE_TAGS.has('resize')).toBe(false);
  });
});

describe('classifyViewportMove', () => {
  // The jump-up bug moves the viewport UP (ydisp decreases) with no
  // user gesture behind it. Downward / no-op moves are never the bug.
  it('returns null when the viewport did not move up', () => {
    expect(
      classifyViewportMove({ from: 10, to: 10, lastUserScrollTs: 0, now: 0 }),
    ).toBe(null);
    expect(
      classifyViewportMove({ from: 10, to: 20, lastUserScrollTs: 0, now: 0 }),
    ).toBe(null);
  });

  it('labels an up-move within the user grace window as user-up', () => {
    // User wheeled 100ms ago, then the viewport moved up → that's them.
    expect(
      classifyViewportMove({
        from: 100,
        to: 40,
        lastUserScrollTs: 900,
        now: 1000,
        userGraceMs: 250,
      }),
    ).toBe('user-up');
  });

  it('treats the grace boundary as inclusive (still user-up)', () => {
    expect(
      classifyViewportMove({
        from: 100,
        to: 40,
        lastUserScrollTs: 750,
        now: 1000,
        userGraceMs: 250,
      }),
    ).toBe('user-up');
  });

  it('labels an up-move with no recent user gesture as auto-up (the suspicious case)', () => {
    expect(
      classifyViewportMove({
        from: 100,
        to: 40,
        lastUserScrollTs: 100,
        now: 1000,
        userGraceMs: 250,
      }),
    ).toBe('auto-up');
  });

  it('labels an up-move as auto-up when no user gesture was ever recorded', () => {
    expect(
      classifyViewportMove({
        from: 100,
        to: 40,
        lastUserScrollTs: null,
        now: 1000,
      }),
    ).toBe('auto-up');
    expect(classifyViewportMove({ from: 100, to: 40, now: 1000 })).toBe(
      'auto-up',
    );
  });
});
