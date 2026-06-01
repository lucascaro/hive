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
// jsdom against plain mocks.
export function snapVisibleTermsToBottom(terms) {
  if (!terms) return;
  for (const st of terms) {
    if (!st || !st.attached) continue;
    if (!st.body || st.body.clientHeight === 0) continue;
    if (st.term && typeof st.term.scrollToBottom === 'function') {
      st.term.scrollToBottom();
    }
  }
}
