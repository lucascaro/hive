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
if (typeof window !== 'undefined') {
  window.__hive_scrolltrace = scrollTrace.ring;
}
