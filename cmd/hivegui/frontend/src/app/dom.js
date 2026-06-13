// Shared top-level DOM handles and the status-bar controller. These
// are app singletons (one #terms, one #status) — modules import them
// rather than re-querying the document.

import { createStatus } from '../lib/status.js';

export const termsHost = document.getElementById('terms');
termsHost.classList.add('single');

export const projectsUL = document.getElementById('projects');
export const status = document.getElementById('status');

const statusCtl = createStatus({
  render: (text, isError) => {
    status.textContent = text;
    status.classList.toggle('error', isError);
  },
  setTimer: (fn, ms) => window.setTimeout(fn, ms),
  clearTimer: (id) => window.clearTimeout(id),
  now: () => Date.now(),
});

// setStatus owns the persistent slot: connection state, nav feedback.
export function setStatus(text, isError = false) {
  statusCtl.set(text, isError);
}

// flashStatus owns transient per-action feedback; it auto-reverts to
// the persistent slot (errors linger 6s, info 2.5s — see lib/status.js).
export function flashStatus(text, isError = false) {
  statusCtl.flash(text, isError);
}

// reportFailure builds a .catch handler that surfaces a failed user
// action in the status bar. Wails mutation promises reject when the
// daemon connection is down (or the call throws Go-side), which used
// to be swallowed — the button click just silently did nothing.
export const reportFailure = (what) => (err) => flashStatus(`${what} failed: ${err}`, true);
