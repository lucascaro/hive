import { describe, it, expect } from 'vitest';
import { createScrollTrace, SCROLL_TRACE_CAP } from '../../src/lib/scroll-debug.js';

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
