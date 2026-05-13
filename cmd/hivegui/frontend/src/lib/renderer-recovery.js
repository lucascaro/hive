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

// Build a self-rebinding DPR change watcher. A `(resolution: Xdppx)`
// MediaQueryList only fires `change` on the single transition away from
// X — after that, `matches` stays false and further DPR shifts are
// silent. We work around this by re-creating the MQL against the
// *current* `window.devicePixelRatio` inside each `change` handler.
//
// `deps`:
//   - matchMedia(query):  factory for MediaQueryList instances.
//   - getDpr():           returns the current device pixel ratio.
//   - onChange():         called after each successful DPR change.
//
// Returns `{ teardown() }` so callers can remove the currently-bound
// listener at destruction. Returns `null` when the platform doesn't
// support matchMedia or the initial bind throws — callers can skip the
// DPR path silently.
export function bindDprWatcher(deps) {
  let mql = null;
  let handler = null;
  const bind = () => {
    try {
      const dpr = deps.getDpr();
      mql = deps.matchMedia(`(resolution: ${dpr}dppx)`);
      handler = () => {
        // Tear down the now-stale MQL and rebind against the new DPR
        // before notifying, so further transitions keep firing.
        try { mql.removeEventListener('change', handler); } catch { /* ignore */ }
        bind();
        try { deps.onChange(); } catch { /* host handles its own errors */ }
      };
      mql.addEventListener('change', handler);
      return true;
    } catch {
      mql = null;
      handler = null;
      return false;
    }
  };
  if (!bind()) return null;
  return {
    teardown() {
      if (mql && handler) {
        try { mql.removeEventListener('change', handler); } catch { /* ignore */ }
      }
      mql = null;
      handler = null;
    },
  };
}
