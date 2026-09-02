// Shared top-level DOM handles and the status-bar controller.
//
// After Phase 2 of the React rewrite this module renders nothing: the
// status bar, boot overlay and banners are components
// (src/components/*), and these functions are the compatibility surface
// the ~40 call sites across app/ already import. Their signatures are
// unchanged on purpose — `setStatus(text, isError)` is called from
// events, modals, keyboard and view, and a rename would be a diff
// through every one of them for no behavioural gain.
//
// What is left here: #terms (an app singleton every terminal path needs
// a handle on) and the lib/status.ts flash engine, whose render callback
// writes the store.

import { createStatus, type ModeHint } from '../lib/status.js';
import * as store from '../store/store.js';
import { mustEl } from './el.js';

// index.html owns this id; a missing one means the document and this
// module have drifted, which is a load-time bug, not a runtime condition
// to branch on. mustEl throws rather than using `!` — it names the id.
//
// The `single` class that used to be added here moved to main.ts: it is
// initial paint state, not a property of holding the handle, and this
// module is imported by ~30 jsdom tests that never render a view.
// showSingle() re-adds it on the first paint either way (view.ts).
export const termsHost = mustEl('terms');

// The flash engine stays here rather than moving into the store: it is
// the timing policy (FLASH_MIN_MS, the persistent slot that survives a
// flash), it is already unit-tested in lib/, and the store's job is to
// hold what is on screen. Its render callback is the only bridge.
const statusCtl = createStatus({
  render: store.setStatusText,
  setTimer: (fn: () => void, ms: number) => window.setTimeout(fn, ms),
  clearTimer: (id: number) => window.clearTimeout(id),
  now: () => Date.now(),
});

// setModeHint owns the right slot: the current mode's top shortcuts.
export function setModeHint(hints: ModeHint[]): void {
  store.setModeHint(hints);
}

// setStatus owns the persistent slot: connection state, nav feedback.
export function setStatus(text: string, isError = false): void {
  statusCtl.set(text, isError);
}

// flashStatus owns transient per-action feedback; it auto-reverts to
// the persistent slot (errors linger 6s, info 2.5s — see lib/status.ts).
export function flashStatus(text: string, isError = false): void {
  statusCtl.flash(text, isError);
}

// reportFailure builds a .catch handler that surfaces a failed user
// action in the status bar. Wails mutation promises reject when the
// daemon connection is down (or the call throws Go-side), which used
// to be swallowed — the button click just silently did nothing.
export const reportFailure = (what: string) => (err: unknown) =>
  flashStatus(`${what} failed: ${err}`, true);

// setBootState drives the full-pane boot overlay. Passing null hides it
// — done once the first session list arrives, which is the first moment
// the pane can tell the truth (before that, "No sessions yet" may just
// mean "the daemon has not answered yet").
//
// The signature keeps its `{ retry }` shape: main.ts's bounded
// retryBoot() is the only caller that passes one, and the 5-attempt
// policy stays there rather than moving into a component.
export function setBootState(
  text: string | null,
  opts: { retry?: () => void } = {},
): void {
  store.setBootState(
    text === null ? null : { text, onRetry: opts.retry ?? null },
  );
}
