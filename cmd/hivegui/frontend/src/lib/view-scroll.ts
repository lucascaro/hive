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
// The snap is asserted twice:
//   - synchronously, so the viewport lands at bottom immediately on
//     machines where the replay has already parsed; and
//   - via a parse-ordered empty write, because xterm's write() is
//     async-queued — on a slow machine the mode-switch replay's
//     multi-MB re-parse may still be queued when the snap fires, and
//     xterm's bottom-follow is lost during that heavy parse (cap-trim
//     keeps baseY pinned at the scrollback cap while the viewport
//     drifts off-bottom). The empty-write callback re-asserts bottom
//     only after the write queue drains, mirroring the parse-ordered
//     discipline in scrollback.js.
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
// Structural, not the real SessionTerm: this module is deliberately
// xterm-free so it can be unit-tested against plain mocks, and the
// mocks only carry these fields.
export interface SnapTarget {
  attached?: boolean;
  body?: { clientHeight: number } | null;
  term?: {
    scrollToBottom?: () => void;
    write?: (data: string, callback?: () => void) => void;
  } | null;
  _replayWantsBottom?: boolean;
  _followBottom?: boolean;
  _replayPrevFromBottom?: number;
}

export function snapVisibleTermsToBottom(
  terms: Iterable<SnapTarget | null | undefined> | null | undefined,
): void {
  if (!terms) return;
  for (const st of terms) {
    if (!st?.attached) continue;
    if (!st.body || st.body.clientHeight === 0) continue;
    // Hoisted to a const so the callback below closes over a non-null
    // `term`. This does change one thing from the original
    // `st.term.scrollToBottom()`: the terminal OBJECT is now captured
    // at snap time rather than re-read when the callback fires. Safe —
    // `SessionTerm.term` is assigned once (app/session-term.js) and
    // never reassigned or nulled. The `?.` still re-reads the method
    // off that object at call time.
    const term = st.term;
    if (term && typeof term.scrollToBottom === 'function') {
      term.scrollToBottom();
      // Parse-ordered re-snap: re-assert bottom after any in-flight
      // replay bytes finish parsing (see header comment).
      if (typeof term.write === 'function') {
        term.write('', () => term.scrollToBottom?.());
      }
      st._replayWantsBottom = true;
      // A mode switch is a deliberate "land at the bottom" — resume
      // following so the next resize keeps the user pinned there.
      st._followBottom = true;
      delete st._replayPrevFromBottom;
    }
  }
}
