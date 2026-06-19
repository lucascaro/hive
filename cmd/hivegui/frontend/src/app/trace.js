import { createScrollTrace } from '../lib/scroll-debug.js';

// Scroll/replay tracer — gated on localStorage hive.debug = '1' (the
// focus consistency checker's switch). The gate is latched here at
// module init: users hitting scroll-jump bugs set the key, RELOAD,
// reproduce, then dump window.__hive_scrolltrace. The e2e-real scroll
// specs arm it via addInitScript (before main.js runs) and read the
// ring to prove a scenario actually fired replays.
export const scrollTrace = createScrollTrace({
  enabled: (() => {
    try { return localStorage.getItem('hive.debug') === '1'; } catch { return false; }
  })(),
});
// localStorage key holding the trace window snapshotted at the moment a
// suspicious auto-up scroll was detected. The live ring is capped at
// 2000 entries and heavy agent output rotates it fast — by the time a
// user notices the jump and runs the dump, the evidence may already be
// gone. snapshotScrollJump() freezes the tail the instant the detector
// fires, and it survives a reload (so a user can capture, reload, and
// still hand over the record).
const LASTJUMP_KEY = 'hive.scrolltrace.lastjump';

// Freeze the current trace tail to localStorage. Called by the
// scroll-jump detector (SessionTerm.onScroll) when it records an
// unexplained upward viewport move. Best-effort: storage may be full or
// disabled — never throw into the scroll path.
export function snapshotScrollJump() {
  try {
    localStorage.setItem(LASTJUMP_KEY, JSON.stringify({
      at: Date.now(),
      ring: scrollTrace.ring.slice(-400),
    }));
  } catch { /* storage unavailable — the live ring is still dumpable */ }
}

// ---------- main-thread heartbeat watchdog ----------
//
// A freeze has two very different shapes, and the fix differs completely:
//   1. The main thread is BLOCKED (a busy rAF/ResizeObserver storm, or a
//      synchronous loop) — keystrokes, paints and timers all stall.
//   2. The thread is HEALTHY but keyboard focus was lost — the app paints
//      and the menu works, yet typed keys land on <body> and vanish.
// Both look identical to a user ("frozen, but the menu still works"),
// because the native menu runs in the main process either way.
//
// A setInterval callback can only run when the main thread yields, so the
// gap between consecutive ticks measures how long the thread was blocked.
// Under shape (1) the gap balloons far past the interval; under shape (2)
// the heartbeat stays smooth. That split is the first question any dump of
// this trace should answer — so it's recorded before anything else.
const HEARTBEAT_MS = 250;
const STALL_FACTOR = 2; // only record gaps > 2x the nominal interval as stalls
let _maxStallMs = 0;
// Standing window state at this instant. The first freeze capture showed
// zero keydowns and maxStallMs=0 (thread never blocked) while rAF was
// throttled to ~1fps — the signature of an OS-occluded / unfocused window
// rather than a busy loop. So every freeze probe now carries whether the
// page is visible and whether the webview actually holds OS key focus.
function winState() {
  const ae = document.activeElement;
  return {
    vis: document.visibilityState,
    hasFocus: typeof document.hasFocus === 'function' ? document.hasFocus() : null,
    grid: !!document.getElementById('terms')?.classList.contains('grid'),
    ae: ae ? `${ae.tagName}.${ae.className || ''}`.trim() : 'none',
  };
}

if (scrollTrace.rec.enabled && typeof window !== 'undefined') {
  let lastBeat = performance.now();
  // A hidden window throttles (or, on system sleep, pauses) background
  // timers, so the gap across a hide/sleep is NOT a main-thread stall —
  // recording it would inflate maxStallMs and corrupt the evidence this
  // dump exists to provide. Skip ticks while hidden, and re-baseline on
  // the hidden -> visible transition so the wake gap isn't logged.
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') lastBeat = performance.now();
    scrollTrace.rec('visibility', winState());
  });
  // Record the exact moment the webview gains/loses OS key focus. If keys
  // go dead the instant a 'win-blur' fires (and never recover), the freeze
  // is the window losing focus — not anything in the app's JS.
  window.addEventListener('focus', () => scrollTrace.rec('win-focus', winState()));
  window.addEventListener('blur', () => scrollTrace.rec('win-blur', winState()));
  let beat = 0;
  setInterval(() => {
    const t = performance.now();
    const gap = t - lastBeat;
    lastBeat = t;
    beat += 1;
    // Low-rate alive beacon (~every 2s) — records the standing window state
    // even when nothing else fires, so a long freeze shows whether the
    // window sat hidden/unfocused the whole time. Recorded regardless of
    // visibility (a hidden window is exactly what we want to catch here).
    if (beat % 8 === 0) scrollTrace.rec('alive', winState());
    if (document.visibilityState !== 'visible') return; // throttled, not blocked
    if (gap > HEARTBEAT_MS * STALL_FACTOR) {
      if (gap > _maxStallMs) _maxStallMs = gap;
      scrollTrace.count('heartbeatStalls');
      // How long the main thread was unresponsive, plus the window state at
      // the stall (grid? focus on a terminal textarea or stranded on body?
      // did the webview even hold OS focus?).
      scrollTrace.rec('heartbeat-stall', { gap: Math.round(gap), ...winState() });
    }
  }, HEARTBEAT_MS);
}

if (typeof window !== 'undefined') {
  window.__hive_scrolltrace = scrollTrace.ring;
  // Operator-facing dump handle: returns the full live ring plus the
  // frozen last-jump window, the rotation-proof counters, and the worst
  // observed main-thread stall. Best-effort copies the JSON to the
  // clipboard so a user hitting the bug can paste it straight back.
  window.__hive_dumpscroll = () => {
    let lastJump = null;
    try { lastJump = JSON.parse(localStorage.getItem(LASTJUMP_KEY) || 'null'); } catch { /* ignore */ }
    const dump = {
      enabled: scrollTrace.rec.enabled,
      ring: scrollTrace.ring.slice(),
      lastJump,
      counters: { ...scrollTrace.counters },
      maxStallMs: Math.round(_maxStallMs),
    };
    try { navigator.clipboard?.writeText(JSON.stringify(dump)); } catch { /* clipboard may be denied */ }
    return dump;
  };
}
