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

// Tags teed to the persistent log (hivegui.log) when a sink is wired AND the
// debug flag is armed. The in-memory ring holds everything (heartbeat, wheel,
// focus, …) for a live window.__hive_dumpscroll dump; only these tags go to
// the log file, so the exact sequence behind a jump survives a reload without
// burying it.
//
// The whitelist is per-EVENT, not per-frame. `resize` is deliberately absent:
// _onBodyResize fires continuously for every tile during a window or sidebar
// drag (see its own comment in session-term.ts), so teeing it writes hundreds
// of lines per second and drowns the records that matter. Its diagnostic
// payload is redundant anyway — the debounced outcome it leads to is captured
// by `replay-request` / `replay-skip`, which fire at most once per drag.
// Other high-rate tags (wheel, focus-apply, heartbeat, alive) are excluded
// for the same reason.
export const TEE_TAGS = new Set([
  'viewport-jump',
  'replay-restore',
  'replay-request',
  'replay-skip',
  'mode-snap',
]);

export interface ScrollTraceEntry {
  t: number;
  tag: string;
  [key: string]: unknown;
}

// `rec` is an expando: call sites gate payload construction on
// `rec.enabled` so a normal run builds nothing.
export interface ScrollTraceRec {
  (tag: string, data?: Record<string, unknown>): void;
  enabled: boolean;
}

export interface ScrollTrace {
  rec: ScrollTraceRec;
  ring: ScrollTraceEntry[];
  count(name: string, by?: number): void;
  counters: Record<string, number>;
}

export function createScrollTrace({
  enabled,
  now,
  cap = SCROLL_TRACE_CAP,
  sink,
  teeTags = TEE_TAGS,
}: {
  enabled?: boolean;
  now?: (() => number) | null;
  cap?: number;
  sink?: ((line: string) => void) | null;
  teeTags?: { has(tag: string): boolean };
}): ScrollTrace {
  const ring: ScrollTraceEntry[] = [];
  const clock =
    now || (() => (typeof performance !== 'undefined' ? performance.now() : 0));
  function rec(tag: string, data: Record<string, unknown> = {}): void {
    // Everything here — ring AND log tee — is gated on the debug flag. The
    // tracer's call sites live in the scroll and resize hot paths, so nothing
    // it does may cost anything in a normal run; a user hitting a scroll bug
    // arms `hive.debug`, relaunches, and reproduces.
    if (!enabled) return;
    // Tee whitelisted tags to hivegui.log. The in-memory ring rotates fast and
    // needs a live window.__hive_dumpscroll to read (which kept coming back
    // stale after a relaunch); the append-only log can't be fooled that way.
    // Best-effort — never throw into a scroll/resize path.
    if (sink && teeTags.has(tag)) {
      try {
        sink(`scroll ${tag} ${JSON.stringify(data)}`);
      } catch {
        /* sink unavailable */
      }
    }
    ring.push({ t: Math.round(clock()), tag, ...data });
    if (ring.length > cap) ring.splice(0, ring.length - cap);
  }
  // Call sites gate their payload build on rec.enabled — it is exactly the
  // debug flag, so a normal run builds nothing and records nothing.
  rec.enabled = !!enabled;
  // Monotonic counters that survive ring rotation. Under a render/focus
  // storm the 2000-entry ring fills in ~1s and the early evidence scrolls
  // away; the counters keep the totals (how many renderGrid calls, focus
  // re-applies, heartbeat stalls, …) legible in any later dump.
  const counters: Record<string, number> = Object.create(null);
  function count(name: string, by = 1): void {
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
}: {
  from: number;
  to: number;
  lastUserScrollTs?: number | null;
  now?: number | null;
  userGraceMs?: number;
}): 'user-up' | 'auto-up' | null {
  if (!(to < from)) return null;
  const userDriven =
    typeof lastUserScrollTs === 'number' &&
    typeof now === 'number' &&
    now - lastUserScrollTs <= userGraceMs;
  return userDriven ? 'user-up' : 'auto-up';
}
