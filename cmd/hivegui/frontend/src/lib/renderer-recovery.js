// Pure helpers for the xterm WebGL renderer recovery flow (#190).
//
// The renderer can land in a state where the glyph atlas / GPU
// backbuffer are stale — context loss (browsers cap simultaneous WebGL
// contexts), DPR change, or post-visibility GPU sleep — and the canvas
// keeps displaying corrupted glyphs until something forces a repaint.
// SessionTerm wires these helpers up; the helpers themselves take no
// terminal or DOM dependencies so they're trivially unit-testable.

// Decide whether a visibilitychange should trigger a renderer refresh.
// We only repaint when the document is becoming visible (the stale
// backbuffer is irrelevant while we're hidden, and we don't want to
// thrash GPU on the way down).
export function shouldRefreshOnVisibility(visibilityState) {
  return visibilityState === 'visible';
}

// Drive a single context-loss recovery cycle. `deps` is an injectable
// surface for the three side-effects so the cycle can be tested in
// isolation:
//   - dispose():  best-effort teardown of the dead WebGL addon.
//   - reattach(): try to bring up a fresh WebGL addon. Returns true on
//                 success, false when the GL context can't be acquired
//                 (e.g. the per-process cap is still saturated).
//   - refresh():  force a full terminal repaint so stale pixels don't
//                 survive into the next user-visible frame.
//
// Returns { reattached } so callers can null out their addon handle
// when reattach failed.
export function recoverFromContextLoss(deps) {
  try { deps.dispose(); } catch { /* dispose is best-effort */ }
  let reattached = false;
  try { reattached = deps.reattach() === true; } catch { reattached = false; }
  if (!reattached) {
    // Reattach failed → we're on the DOM renderer now (or no renderer
    // at all). Force a refresh so the canvas's stale pixels are
    // overwritten without waiting for the next resize.
    try { deps.refresh(); } catch { /* nothing else to do */ }
  }
  return { reattached };
}
