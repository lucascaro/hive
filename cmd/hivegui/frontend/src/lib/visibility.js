// shouldFitTerminal decides whether a tile's xterm.fit() call should
// proceed. The recent regression (canvas-resize-on-session-switch)
// hit because fit() was called against a 0×0 box while the tile was
// hidden. Rule: only fit when the tile is both visible (display
// non-none) and has positive width and height.
//
// Pure: takes a snapshot of layout-relevant flags, returns bool.
export function shouldFitTerminal({ visible, width, height }) {
  if (!visible) return false;
  if (!Number.isFinite(width) || !Number.isFinite(height)) return false;
  return width > 0 && height > 0;
}
