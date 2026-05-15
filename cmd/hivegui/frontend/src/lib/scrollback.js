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

// Handle one EventScrollback{Begin,Done} payload by mutating the
// session-term object accordingly. Extracted from main.js so the
// state machine can be unit-tested against a mock term object
// without dragging xterm.js + jsdom into the suite.
//
// Returns true if `kind` matched a known event, false otherwise.
// On Begin we call `st.term.reset()` to wipe xterm's buffer so the
// incoming replay bytes paint onto a clean slate, AND reset the
// UTF-8 decoder so a partial multi-byte rune sitting across the
// boundary doesn't corrupt the first replay character. On Done we
// scroll to bottom so the user sees the cursor / newest output
// rather than landing mid-history.
export function handleScrollbackEvent(st, kind) {
  if (!st || !st.term) return false;
  switch (kind) {
    case 'scrollback_replay_begin':
      st.term.reset();
      if (st.decoder) st.decoder = new TextDecoder('utf-8');
      return true;
    case 'scrollback_replay_done':
      if (typeof st.term.scrollToBottom === 'function') {
        st.term.scrollToBottom();
      }
      return true;
    default:
      return false;
  }
}
