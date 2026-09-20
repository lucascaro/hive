// ---------- the failed-build log viewer: the non-React half ----------
//
// Opened from the update banner's "View log" when a latest-channel build
// fails. The banner can only quote one line, and for a Wails build that
// line was its sponsorship trailer — the real error sat somewhere above.
// Same shape as whats-new.ts: open, close, and the two focus callbacks
// main.tsx injects.

import { flushSync } from 'react-dom';
import { UpdateBuildLog } from '../../bridge.js';
import { releaseFocus } from '../../lib/focus-trap.js';
import { closeModal, isModalOpen, openModal } from '../../store/store.js';
import { reportFailure } from '../dom.js';
import { pageEl } from '../el.js';

export interface BuildLogDeps {
  setFocusedTile: (id: string | null) => void;
  focusActiveTerm: () => void;
}

let deps: BuildLogDeps = {
  setFocusedTile: () => {},
  focusActiveTerm: () => {},
};

export async function openBuildLog(): Promise<void> {
  if (isModalOpen('build-log')) return;
  let log: string;
  try {
    log = await UpdateBuildLog();
  } catch (err) {
    reportFailure('load build log')(err);
    return;
  }
  openModal({ id: 'build-log', log });
  deps.setFocusedTile(null);
}

export function closeBuildLog(): void {
  releaseFocus(pageEl('build-log'));
  // flushSync: this also runs from keyboard.ts's window listener, and
  // focusActiveTerm() must not run while the dialog is still visible.
  flushSync(() => closeModal('build-log'));
  deps.focusActiveTerm();
}

export function initBuildLog(injected: BuildLogDeps): void {
  deps = injected;
}
