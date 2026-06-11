// snapVisibleTermsToBottom snaps each currently-visible session-term
// to the bottom of its scrollback. Used after a mode switch
// (single ↔ grid ↔ grid-project) so the user lands at the latest
// output rather than wherever the xterm buffer happened to be —
// mode toggles are deliberate user actions, so an unconditional snap
// is the expected behavior.
//
// Skips terms that aren't attached or whose body has zero size
// (display:none / not yet laid out). xterm's scrollToBottom is a
// no-op if there's no scrollback, so guarding is cheap.
//
// Pure helper — no xterm.js import — so it can be unit-tested in
// jsdom against plain mocks. Accepts any iterable (array, Map.values()).
//
// Also overrides any pending replay "restore the reader" intent on
// each snapped term — BOTH halves of the intent pair:
//   - `_replayWantsBottom = true`: a same-tick `show()`/`_onBodyResize()`
//     chain (e.g. during setView) may have armed a debounced replay
//     with `_replayWantsBottom = false` (user was scrolled up at
//     resize capture time). The mode switch is the deliberate user
//     action requesting "land at bottom", so the next replay-done
//     must honor that. The flag is consumed-and-cleared by the
//     replay-done handler in handleScrollbackEvent.
//   - `delete _replayPrevFromBottom`: the flag alone is not enough
//     when a replay-done EVENT has already latched wantsBottom=false
//     but its parse-time `finish` has not run yet. `finish` reads the
//     captured distance at parse time, so deleting it here is what
//     actually stops the queued restore from scrollToLine-ing the
//     viewport back into history and reverting this snap.
// The pair must be cleared together at every override site (here and
// in applyRebaseline) — clearing only one leaves a stale half that a
// later replay-done can act on.
export function snapVisibleTermsToBottom(terms) {
  if (!terms) return;
  for (const st of terms) {
    if (!st || !st.attached) continue;
    if (!st.body || st.body.clientHeight === 0) continue;
    if (st.term && typeof st.term.scrollToBottom === 'function') {
      st.term.scrollToBottom();
      st._replayWantsBottom = true;
      delete st._replayPrevFromBottom;
    }
  }
}
