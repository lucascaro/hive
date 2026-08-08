// Shared top-level DOM handles and the status-bar controller. These
// are app singletons (one #terms, one #status) — modules import them
// rather than re-querying the document.

import { createStatus } from '../lib/status.js';
import { mustEl } from './el.js';

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
// the persistent slot (errors linger 6s, info 2.5s — see lib/status.js).
export function flashStatus(text: string, isError = false): void {
  statusCtl.flash(text, isError);
}

// reportFailure builds a .catch handler that surfaces a failed user
// action in the status bar. Wails mutation promises reject when the
// daemon connection is down (or the call throws Go-side), which used
// to be swallowed — the button click just silently did nothing.
export const reportFailure = (what: string) => (err: unknown) =>
  flashStatus(`${what} failed: ${err}`, true);
