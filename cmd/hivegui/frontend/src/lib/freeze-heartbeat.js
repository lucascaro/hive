// Always-on freeze classifier (idle vs blocked), logged to disk.
//
// The WebGL-storm freeze pinned a CPU core and left a fingerprint. The
// *other* freeze does the opposite: the webview main thread goes idle,
// 0% CPU, and nothing in the app's instrumented paths fires — so it
// leaves no trace on disk. A user-reported "irresponsive" then has zero
// evidence to work from.
//
// A setInterval callback can only run when the main thread yields, so the
// gap between ticks measures how long the thread was blocked. This helper
// is the pure decision half (what, if anything, to log for a given gap);
// the caller owns the timer and the LogFrontend sink so this stays
// trivially unit-testable.

// classifyBeat returns a log line string, or null to stay silent.
//   - gap  : ms since the previous tick.
//   - nominalMs : the timer's configured interval.
//   - visible   : document.visibilityState === 'visible'.
//   - state     : arbitrary extra fields ({hasFocus, view, ...}) appended.
//   - beat      : monotonically increasing tick counter.
//   - aliveEvery: emit a low-rate "alive" line every N beats (0 = never).
//
// A gap far past nominal means the thread was BLOCKED (busy loop or a
// synchronous stall) — always worth logging. A hidden window legitimately
// throttles timers, so a large gap while hidden is not a stall and is
// suppressed. The periodic "alive" line proves the loop is still running
// (and carries window state) so a totally silent log = process wedged or
// killed, not merely quiet.
export function classifyBeat({
  gap,
  nominalMs,
  visible,
  state,
  beat,
  aliveEvery,
}) {
  const stall = visible && gap > nominalMs * 2;
  if (stall) {
    return `hb STALL gap=${Math.round(gap)}ms ${fmtState(state)}`;
  }
  if (aliveEvery > 0 && beat % aliveEvery === 0) {
    return `hb alive vis=${visible ? 1 : 0} ${fmtState(state)}`;
  }
  return null;
}

function fmtState(state) {
  if (!state) return '';
  return Object.entries(state)
    .map(([k, v]) => `${k}=${v}`)
    .join(' ');
}

// jsHeapMB reads the JS heap size (MB, rounded) from performance.memory
// when the engine exposes it. Chromium/WebView2 do; WebKit/Safari's
// WebContent (what the Mac app uses) generally does NOT — returns null
// there, and the heartbeat line simply omits the field. It measures the
// JS heap only, not the process RSS that ballooned to ~1 GB (that lives
// in the separate WebContent/GPU process), but a climbing JS heap is
// still a useful leak signal where available.
export function jsHeapMB(perf) {
  try {
    const m = perf?.memory;
    if (m && typeof m.usedJSHeapSize === 'number') {
      return Math.round(m.usedJSHeapSize / (1024 * 1024));
    }
  } catch {
    /* not exposed */
  }
  return null;
}
