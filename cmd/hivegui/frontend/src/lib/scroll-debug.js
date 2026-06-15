// Scroll/replay tracer. Gated on localStorage `hive.debug` = '1'
// (same switch as the focus consistency checker). `enabled` is
// latched once at startup, so to diagnose a scroll-jump bug: set
// `localStorage.setItem('hive.debug', '1')`, RELOAD the app,
// reproduce, then dump `window.__hive_scrolltrace`. Tests read the
// ring to prove a scenario actually exercised the replay machinery
// (a passing assertion over zero replays proves nothing).
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

// Classify a single viewport move for the scroll-jump auto-detector.
// The reported bug moves the viewport UP into history (xterm's ydisp
// decreases) with no user gesture behind it. `from`/`to` are the
// previous and new viewportY; `lastUserScrollTs` is when the user last
// drove a scroll (wheel / scroll key), `now` the move's timestamp, both
// on the same monotonic clock. Returns:
//   - null       the move wasn't upward (down-scroll or no-op) — never the bug
//   - 'user-up'  upward, but a user gesture fired within `userGraceMs` — expected
//   - 'auto-up'  upward with NO recent user gesture — the suspicious case the
//                detector records (a resize/replay/renderer event moved it)
// Pure so the detector's decision can be unit-tested without xterm.
export function classifyViewportMove({ from, to, lastUserScrollTs, now, userGraceMs = 250 }) {
  if (!(to < from)) return null;
  const userDriven =
    typeof lastUserScrollTs === 'number' &&
    typeof now === 'number' &&
    (now - lastUserScrollTs) <= userGraceMs;
  return userDriven ? 'user-up' : 'auto-up';
}
