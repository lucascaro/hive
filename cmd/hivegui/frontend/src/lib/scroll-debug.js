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

// Tags teed to the persistent log (hivegui.log) when a sink is wired. The
// in-memory ring holds everything (heartbeat, wheel, focus, …) for a live
// window.__hive_dumpscroll dump; only these low-rate, scroll-diagnostic tags
// go to the log file so the exact sequence behind a jump survives a reload
// without flooding the log. High-rate tags (wheel, focus-apply, heartbeat,
// alive) are deliberately excluded.
export const TEE_TAGS = new Set([
  'viewport-jump',
  'replay-restore',
  'mode-snap',
  'resize',
]);

export function createScrollTrace({
  enabled,
  now,
  cap = SCROLL_TRACE_CAP,
  sink,
  teeTags = TEE_TAGS,
}) {
  const ring = [];
  const clock =
    now || (() => (typeof performance !== 'undefined' ? performance.now() : 0));
  function rec(tag, data = {}) {
    // Tee whitelisted low-rate tags to the persistent log ALWAYS — even when
    // the in-memory tracer is disabled. The localStorage-gated ring dump kept
    // coming back stale (the flag clears on relaunch, so window.__hive_dumpscroll
    // returned a frozen old snapshot); the append-only log tee can't be fooled
    // that way. Only the 4 diagnostic tags are teed, so this is a handful of
    // lines per mode switch, not a flood. Best-effort — never throw into a
    // scroll/resize path.
    if (sink && teeTags.has(tag)) {
      try {
        sink(`scroll ${tag} ${JSON.stringify(data)}`);
      } catch {
        /* sink unavailable */
      }
    }
    // The rich in-memory ring (all tags, for window.__hive_dumpscroll) stays
    // gated on the debug flag.
    if (!enabled) return;
    ring.push({ t: Math.round(clock()), tag, ...data });
    if (ring.length > cap) ring.splice(0, ring.length - cap);
  }
  // Call sites gate their (cheap) payload build on rec.enabled. Keep them
  // firing whenever EITHER the ring is on (debug flag) OR a log sink is
  // wired, so the always-on tee above actually receives the 4 diagnostic
  // tags in production. The ring/counters/heartbeat still gate on the real
  // `enabled` internally, so this adds only the teed lines, nothing else.
  rec.enabled = enabled || !!sink;
  // Monotonic counters that survive ring rotation. Under a render/focus
  // storm the 2000-entry ring fills in ~1s and the early evidence scrolls
  // away; the counters keep the totals (how many renderGrid calls, focus
  // re-applies, heartbeat stalls, …) legible in any later dump.
  const counters = Object.create(null);
  function count(name, by = 1) {
    if (!enabled) return;
    counters[name] = (counters[name] || 0) + by;
  }
  return { rec, ring, count, counters };
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
export function classifyViewportMove({
  from,
  to,
  lastUserScrollTs,
  now,
  userGraceMs = 250,
}) {
  if (!(to < from)) return null;
  const userDriven =
    typeof lastUserScrollTs === 'number' &&
    typeof now === 'number' &&
    now - lastUserScrollTs <= userGraceMs;
  return userDriven ? 'user-up' : 'auto-up';
}
