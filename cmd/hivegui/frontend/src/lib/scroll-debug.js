// Scroll/replay tracer. Gated on localStorage `hive.debug` = '1'
// (same switch as the focus consistency checker), so production users
// can flip it on and dump `window.__hive_scrolltrace` when reporting
// scroll-jump bugs; tests read it to prove a scenario actually
// exercised the replay machinery (a passing assertion over zero
// replays proves nothing).
//
// Pure factory — timer source injected for tests.

export const SCROLL_TRACE_CAP = 2000;

export function createScrollTrace({ enabled, now, cap = SCROLL_TRACE_CAP }) {
  const ring = [];
  const clock = now || (() => (typeof performance !== 'undefined' ? performance.now() : 0));
  function rec(tag, data = {}) {
    if (!enabled) return;
    ring.push({ t: Math.round(clock()), tag, ...data });
    if (ring.length > cap) ring.splice(0, ring.length - cap);
  }
  rec.enabled = enabled;
  return { rec, ring };
}
