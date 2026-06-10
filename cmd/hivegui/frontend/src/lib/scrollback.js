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
// rather than landing mid-history — UNLESS `st._replayWantsBottom`
// was explicitly set to `false` by the caller that armed the replay
// (see _onBodyResize: a user actively reading scrollback when a
// resize triggers a replay must not be yanked to the bottom). The
// flag is consumed (deleted) after each Done so it does not leak
// across replays.
// applyRebaseline snaps the replay baseline to the term's current cols
// and clears any pending debounced replay timer. Used when grid
// geometry changes for a non-resize reason (first attach in grid,
// minimize/restore reflowing remaining tiles): the next _onBodyResize
// would otherwise see a >=REPLAY_COL_THRESHOLD delta against a stale
// baseline and trigger a spurious scrollback replay that visibly
// drops or duplicates lines. Pure user window resizes do NOT call
// this — they should continue through shouldRequestReplay.
//
// Mutates `st` in place and returns it. `st.term.cols` is required;
// `st._replayTimer` is cleared via the injected clearTimer (defaults
// to global clearTimeout) when present and truthy.
export function applyRebaseline(st, clearTimer = clearTimeout) {
  if (!st || !st.term) return st;
  st._replayBaselineCols = st.term.cols;
  if (st._replayTimer) {
    clearTimer(st._replayTimer);
    st._replayTimer = 0;
  }
  // Always clear any pending wants-bottom intent on rebaseline. The
  // armed replay (if any) is being canceled here; leaving the flag
  // would let a later unrelated replay-done read a stale `false` and
  // skip its default bottom snap.
  delete st._replayWantsBottom;
  return st;
}

export function handleScrollbackEvent(st, kind) {
  if (!st || !st.term) return false;
  switch (kind) {
    case 'scrollback_replay_begin': {
      // Capture the reader's position as a distance from the bottom
      // BEFORE the wipe. The replay rebuilds the buffer with the
      // viewport tracking the bottom, so "skip the snap on done" alone
      // cannot preserve a scrolled-up reading position — the position
      // is destroyed at reset time, not at done time. (This was the
      // real mechanism behind "scrolling jumps around with codex":
      // any resize/mode-reflow replay dumped a reading user at the
      // bottom despite #213's wants-bottom flag.)
      const buf = st.term.buffer?.active;
      if (buf && typeof buf.baseY === 'number' && typeof buf.viewportY === 'number') {
        st._replayPrevFromBottom = Math.max(0, buf.baseY - buf.viewportY);
      } else {
        delete st._replayPrevFromBottom;
      }
      // Parse-ordered reset: xterm's write() is async-queued, so a
      // synchronous reset() jumps the queue — any not-yet-parsed live
      // bytes would repaint AFTER the wipe and then appear a second
      // time when the replay re-streams them (duplicated lines under
      // codex-rate output). The empty-write callback executes exactly
      // between the pre-begin backlog and the replay bytes.
      if (typeof st.term.write === 'function') {
        st.term.write('', () => st.term.reset());
      } else {
        st.term.reset();
      }
      // The decoder resets immediately: writeData decodes at event
      // time (queueing decoded strings), so decode order == event
      // order and the fresh decoder must be in place for the first
      // replay chunk, not parse time.
      if (st.decoder) st.decoder = new TextDecoder('utf-8');
      return true;
    }
    case 'scrollback_replay_done': {
      const wantsBottom = st._replayWantsBottom !== false;
      delete st._replayWantsBottom;
      const fromBottom = st._replayPrevFromBottom;
      delete st._replayPrevFromBottom;
      const finish = () => {
        if (wantsBottom) {
          if (typeof st.term.scrollToBottom === 'function') st.term.scrollToBottom();
          return;
        }
        // Restore the reader's distance from the bottom. Soft-wrapped
        // line counts can differ after the rewrap at the new width, so
        // this is an approximation — but it keeps the reader in
        // history near where they were instead of at the bottom.
        const buf = st.term.buffer?.active;
        if (typeof fromBottom === 'number' && buf && typeof st.term.scrollToLine === 'function') {
          st.term.scrollToLine(Math.max(0, buf.baseY - fromBottom));
        }
      };
      // Parse-ordered: at done-event time the replay bytes may still
      // be sitting in xterm's write queue — "done" must mean "fully
      // parsed" before the viewport is placed, or the restore target
      // is computed against a half-built buffer.
      if (typeof st.term.write === 'function') st.term.write('', finish);
      else finish();
      return true;
    }
    default:
      return false;
  }
}
