// Shared top-level DOM handles and the status-bar controller. These
// are app singletons (one #terms, one #status) — modules import them
// rather than re-querying the document.

import { createStatus } from '../lib/status.js';
import { mustEl, pageEl } from './el.js';

// index.html owns these three ids; a missing one means the document and
// this module have drifted, which is a load-time bug, not a runtime
// condition to branch on. mustEl throws rather than using `!` — it names
// the id. See el.ts for why the modals use the non-throwing pageEl().
export const termsHost = mustEl('terms');
termsHost.classList.add('single');

export const projectsUL = mustEl('projects');
export const status = mustEl('status');

const statusCtl = createStatus({
  render: (text: string, isError: boolean) => {
    status.textContent = text;
    status.classList.toggle('error', isError);
  },
  setTimer: (fn: () => void, ms: number) => window.setTimeout(fn, ms),
  clearTimer: (id: number) => window.clearTimeout(id),
  now: () => Date.now(),
});

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

// setBootState drives the full-pane boot overlay declared in
// index.html. Passing null hides it — done once the first session list
// arrives, which is the first moment the pane can tell the truth
// (before that, "No sessions yet" may just mean "the daemon has not
// answered yet").
//
// pageEl, not mustEl: the jsdom tests mount partial markup and must
// not fail on an overlay they never exercise.
export function setBootState(
  text: string | null,
  opts: { retry?: () => void } = {},
): void {
  const el = pageEl('boot-state');
  if (!el) return;
  if (text === null) {
    el.classList.add('hidden');
    return;
  }
  const label = pageEl('boot-state-text');
  if (label) label.textContent = text;
  // A card offering Retry is a card that has stopped waiting, so the
  // spinner goes with it.
  const spinner = el.querySelector('.phase-spinner');
  spinner?.classList.toggle('hidden', Boolean(opts.retry));
  const retry = pageEl<HTMLButtonElement>('boot-state-retry');
  if (retry) {
    retry.classList.toggle('hidden', !opts.retry);
    retry.onclick = opts.retry ? () => opts.retry?.() : null;
  }
  el.classList.remove('hidden');
}
