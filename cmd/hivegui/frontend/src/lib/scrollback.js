// Scrollback-replay decision helpers. Pure functions so the resize-
// driven replay logic in main.js can be unit-tested without dragging
// xterm.js or the Wails bridge into jsdom.

// Column-count change that warrants a fresh scrollback replay. Picked
// to ignore minor font-kerning jitter while still firing on single ↔
// grid transitions (which typically change cols by tens of columns).
export const REPLAY_COL_THRESHOLD = 4;

// Debounce window so dragging a divider doesn't spam replays — only
// the final settled width sends a request.
export const REPLAY_DEBOUNCE_MS = 100;

// Returns true when a resize from prevCols → nextCols should trigger
// a scrollback replay request. Encapsulates "first measurement"
// (prevCols falsy) as well: an undefined / 0 prev means we haven't
// measured yet, so suppress — the initial attach already sent a
// replay.
export function shouldRequestReplay(prevCols, nextCols, threshold = REPLAY_COL_THRESHOLD) {
  if (!prevCols || !nextCols) return false;
  return Math.abs(nextCols - prevCols) >= threshold;
}
