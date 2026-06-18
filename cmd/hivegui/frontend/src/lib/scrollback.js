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

// decideResizeReplay runs at replay-debounce fire time, once a resize has
// already passed shouldRequestReplay. It answers two coupled questions:
// whether to actually send the replay, and what the new replay baseline is.
//
//   - alternate screen (claude/codex/pi and other full-screen TUIs): SKIP.
//     The alt buffer has no user-facing scrollback, and the program repaints
//     itself from the SIGWINCH ResizeSession already sent — so replaying the
//     daemon's whole raw byte ring (multi-MB on a long-lived session, toward
//     the 8 MB cap) would freeze the renderer for seconds to repaint a screen
//     the TUI already redrew. Crucially the baseline is LEFT UNCHANGED: the
//     normal-buffer scrollback may be wrapped at the old width, and there is
//     no alt→normal re-sync handler, so keeping the old baseline lets the next
//     normal-buffer resize still cross the threshold and send the corrective
//     replay.
//   - normal screen: replay, and advance the baseline to the new width.
export function decideResizeReplay({ bufferType, cols, baselineCols }) {
  if (bufferType === 'alternate') return { replay: false, baseline: baselineCols };
  return { replay: true, baseline: cols };
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
  // Always clear the pending restore-intent PAIR on rebaseline. The
  // armed replay (if any) is being canceled here; leaving the flag
  // would let a later unrelated replay-done read a stale `false` and
  // skip its default bottom snap, and leaving the captured distance
  // would let an already-latched done's parse-time finish scrollToLine
  // back into history. The pair must be cleared together at every
  // override site (here and in snapVisibleTermsToBottom).
  delete st._replayWantsBottom;
  delete st._replayPrevFromBottom;
  return st;
}

// Abandon any in-flight scrollback replays for this term. The in-flight
// counter keeps a FOLLOWING viewport pinned to the bottom during a restream
// (see SessionTerm.onScroll); it is incremented on replay-begin and only
// decremented on the matching replay-done. A dropped connection or a buffer
// wipe for a non-replay reason (disconnect, reattach, revival) means those
// done events will never arrive — so the count must be cleared here, or it
// leaks >0 and pins the viewport to the bottom forever, re-correcting the
// parse-driven cap-trim drift that #228 deliberately leaves alone.
export function abandonReplays(st) {
  if (st) st._replaysInFlight = 0;
}

// `trace` (optional) is scrollTrace.rec — when supplied, the replay
// restore decision is recorded at parse time (wantsBottom, the captured
// fromBottom distance, the computed scrollToLine target, and the
// resulting viewport). This is the smoking gun for the jump-up bug: a
// `wantsBottom:false` restore to a target far above baseY while the user
// was actually at the bottom shows the heuristic misfired.
export function handleScrollbackEvent(st, kind, trace) {
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
      const capture = () => {
        const buf = st.term.buffer?.active;
        if (buf && typeof buf.baseY === 'number' && typeof buf.viewportY === 'number') {
          st._replayPrevFromBottom = Math.max(0, buf.baseY - buf.viewportY);
        } else {
          delete st._replayPrevFromBottom;
        }
      };
      // Parse-ordered reset: xterm's write() is async-queued, so a
      // synchronous reset() jumps the queue — any not-yet-parsed live
      // bytes would repaint AFTER the wipe and then appear a second
      // time when the replay re-streams them (duplicated lines under
      // codex-rate output). The empty-write callback executes exactly
      // between the pre-begin backlog and the replay bytes.
      //
      // The capture is parse-ordered too, for the same reason: the
      // backlog parsing between the begin event and the wipe grows
      // baseY while a scrolled-up reader's viewportY stays put, and
      // the replay re-streams those backlog bytes into the rebuilt
      // buffer. Measuring at reset time includes them, so the done-
      // restore lands on the content the reader was actually on
      // instead of a backlog's-worth of lines below it.
      if (typeof st.term.write === 'function') {
        st.term.write('', () => { capture(); st.term.reset(); });
      } else {
        capture();
        st.term.reset();
      }
      // The decoder resets immediately: writeData decodes at event
      // time (queueing decoded strings), so decode order == event
      // order and the fresh decoder must be in place for the first
      // replay chunk, not parse time.
      if (st.decoder) st.decoder = new TextDecoder('utf-8');
      // Mark a replay as in flight so the term's onScroll can keep a
      // FOLLOWING viewport pinned to the bottom for the whole restream
      // (a full-buffer replay spans many frames; the reset above wipes the
      // viewport to the top and cap-trim then strands it in history). A
      // counter, not a flag: resizes can overlap, so several replays are
      // in flight at once and the last done must not clear an earlier
      // replay's pin. Decremented in the done handler's parse-ordered finish.
      st._replaysInFlight = (st._replaysInFlight || 0) + 1;
      return true;
    }
    case 'scrollback_replay_done': {
      const wantsBottom = st._replayWantsBottom !== false;
      delete st._replayWantsBottom;
      const finish = () => {
        st._replaysInFlight = Math.max(0, (st._replaysInFlight || 0) - 1);
        // Consume the captured distance here, at parse time — the
        // begin handler sets it from its own parse-ordered callback,
        // which at done-EVENT time has usually not flushed yet (the
        // replay bytes are still in the queue). Reading it at event
        // time would see undefined and silently skip the restore.
        // Queue order pairs each begin's capture with its done's
        // finish even when events interleave.
        const fromBottom = st._replayPrevFromBottom;
        delete st._replayPrevFromBottom;
        if (wantsBottom) {
          if (typeof st.term.scrollToBottom === 'function') st.term.scrollToBottom();
          // Replay landed at the bottom — keep following. (Without this,
          // a stale _followBottom=false could survive a snap-to-bottom.)
          st._followBottom = true;
          if (trace) {
            const b = st.term.buffer?.active;
            trace('replay-restore', {
              id: st.info?.id, wants: true, fromBottom,
              baseY: b?.baseY, viewportY: b?.viewportY,
            });
          }
          return;
        }
        // Restore the reader's distance from the bottom. Soft-wrapped
        // line counts can differ after the rewrap at the new width, so
        // this is an approximation — but it keeps the reader in
        // history near where they were instead of at the bottom.
        const buf = st.term.buffer?.active;
        let target;
        if (typeof fromBottom === 'number' && buf && typeof st.term.scrollToLine === 'function') {
          target = Math.max(0, buf.baseY - fromBottom);
          st.term.scrollToLine(target);
          // Only un-follow when a restore ACTUALLY moved the viewport into
          // history. Latching false unconditionally here would strand a
          // user who has since scrolled back to the bottom while a stale
          // wants=false replay was still in flight (no scrollToLine ran).
          st._followBottom = false;
        }
        if (trace) {
          trace('replay-restore', {
            id: st.info?.id, wants: false, fromBottom, target,
            baseY: buf?.baseY, viewportY: st.term.buffer?.active?.viewportY,
          });
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
