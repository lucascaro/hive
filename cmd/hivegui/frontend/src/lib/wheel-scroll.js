// Wheel → terminal-line conversion, normalized across platforms.
//
// The GUI takes over wheel handling (capture phase + preventDefault) so
// xterm's own wheel→lines math never runs — see SessionTerm. That means
// EVERY wheel scroll flows through this one function; if it returns 0,
// the terminal cannot scroll at all.
//
// The original math assumed pixel deltas (deltaMode 0) and a populated
// standard `deltaY`, which holds on Chromium and on recent macOS. But
// Wails renders in the *system* WKWebView, whose version tracks the
// macOS version — and some builds deliver wheel events in LINE or PAGE
// mode, or with `deltaY === 0` and the value only in the deprecated
// `wheelDeltaY`. Each of those collapsed the pixel math to 0, leaving
// the terminal completely unscrollable by wheel/trackpad on that machine
// (selection-drag still scrolled, because xterm drives that internally
// and bypasses this handler). We normalize all of those here.
//
// Pure function so the cross-platform cases can be unit-tested without a
// real wheel event or a webview.

const DOM_DELTA_LINE = 1;
const DOM_DELTA_PAGE = 2;

export function wheelToScrollLines(e, { linesPerPixel, maxLinesPerEvent }) {
  let deltaY = e.deltaY;
  let deltaMode = e.deltaMode;

  // Legacy fallback: some WebKit builds leave the standard deltaY at 0
  // (or undefined) but still populate the deprecated wheelDeltaY, which
  // carries the OPPOSITE sign and a pixel-scale magnitude.
  if ((!Number.isFinite(deltaY) || deltaY === 0)
    && Number.isFinite(e.wheelDeltaY) && e.wheelDeltaY !== 0) {
    deltaY = -e.wheelDeltaY;
    deltaMode = 0; // wheelDeltaY is pixel-scale
  }

  if (!Number.isFinite(deltaY) || deltaY === 0) return 0;

  let lines;
  if (deltaMode === DOM_DELTA_LINE) {
    // deltaY is ALREADY a line count (one wheel notch ≈ 1–3 lines).
    lines = Math.round(deltaY);
  } else if (deltaMode === DOM_DELTA_PAGE) {
    // A page per event exceeds our cap; the clamp below bounds it.
    lines = deltaY > 0 ? maxLinesPerEvent : -maxLinesPerEvent;
  } else {
    // Pixel deltas — the common macOS trackpad / Chromium path.
    lines = Math.round(deltaY * linesPerPixel);
  }

  // Sub-unit events still move at least one line in their direction, so
  // a slow scroll never silently does nothing.
  if (lines === 0) lines = deltaY > 0 ? 1 : -1;

  if (lines > maxLinesPerEvent) lines = maxLinesPerEvent;
  if (lines < -maxLinesPerEvent) lines = -maxLinesPerEvent;
  return lines;
}
