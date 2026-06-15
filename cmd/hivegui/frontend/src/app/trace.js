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

if (typeof window !== 'undefined') {
  window.__hive_scrolltrace = scrollTrace.ring;
  // Operator-facing dump handle: returns the full live ring plus the
  // frozen last-jump window, and best-effort copies the JSON to the
  // clipboard so a user hitting the bug can paste it straight back.
  window.__hive_dumpscroll = () => {
    let lastJump = null;
    try { lastJump = JSON.parse(localStorage.getItem(LASTJUMP_KEY) || 'null'); } catch { /* ignore */ }
    const dump = { enabled: scrollTrace.rec.enabled, ring: scrollTrace.ring.slice(), lastJump };
    try { navigator.clipboard?.writeText(JSON.stringify(dump)); } catch { /* clipboard may be denied */ }
    return dump;
  };
}
