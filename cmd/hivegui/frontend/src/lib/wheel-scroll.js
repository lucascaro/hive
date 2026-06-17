// Wheel → terminal-line conversion, normalized across platforms.
//
// The GUI takes over wheel handling (capture phase + preventDefault) so
// xterm's own wheel→lines math never runs — see SessionTerm. This function
// runs only when shouldScrollViewport (below) says the gesture is ours to
// interpret: the normal buffer with mouse reporting off. In that case every
// wheel scroll flows through it, and if it returns 0 the terminal cannot
// scroll at all. (Alt-buffer / mouse-tracking events bail before reaching
// here and fall through to xterm.)
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

// Whether the GUI should take over the wheel and turn it into local
// scrollback (preventDefault + term.scrollLines). ONLY in the normal buffer
// with mouse reporting OFF:
//   - In the alternate buffer (full-screen TUIs — Claude, vim, htop) there is
//     no scrollback, so term.scrollLines is a no-op.
//   - When the running program has enabled mouse tracking it expects the
//     wheel forwarded to it as mouse events so it can scroll its OWN content.
// Taking over in either case swallows the gesture (capture-phase
// preventDefault + stopPropagation) and the app can never scroll — which is
// exactly why the terminal scrolled in a plain shell / pi but not in Claude.
// When this returns false we let the event fall through to xterm's native
// handling (forward-to-app, or Shift+wheel local scroll). Pure so the
// buffer/mode matrix is unit-testable without xterm.
export function shouldScrollViewport({ bufferType, mouseTrackingMode }) {
  if (bufferType !== 'normal') return false;
  if (mouseTrackingMode && mouseTrackingMode !== 'none') return false;
  return true;
}

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
