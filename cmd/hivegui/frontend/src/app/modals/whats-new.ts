// ---------- the What's New modal (sidebar gift): the non-React half ----------
//
// Modelled on help-overlay.ts, which has the same shape: a modal with no
// daemon round-trip, opened from one control and closed on Escape.
//
// The one thing it adds is the read receipt. Opening — not closing — writes
// the seen version: opening IS the act of reading, and someone who opens the
// modal and then quits the app has still read it. Writing on close would make
// a quit-while-open look like it never happened.

import { flushSync } from 'react-dom';
import { closeModal, isModalOpen, openModal } from '../../store/store.js';
import { releaseFocus } from '../../lib/focus-trap.js';
import { latestVersion, SEEN_KEY } from '../../lib/whats-new.js';
import { pageEl } from '../el.js';

export interface WhatsNewDeps {
  setFocusedTile: (id: string | null) => void;
  focusActiveTerm: () => void;
}

let deps: WhatsNewDeps = {
  setFocusedTile: () => {},
  focusActiveTerm: () => {},
};

/** The version the user has read up to, or null if they never have. */
export function readSeenVersion(): string | null {
  try {
    return localStorage.getItem(SEEN_KEY);
  } catch {
    // Private-mode / disabled storage: treat as never-read rather than
    // throwing on a sidebar render.
    return null;
  }
}

function markSeen(): void {
  const latest = latestVersion();
  if (!latest) return;
  try {
    localStorage.setItem(SEEN_KEY, latest);
  } catch {
    // Nothing to do — the dot comes back next launch, which is a better
    // failure than a crash on click.
  }
}

export function openWhatsNew() {
  if (isModalOpen('whats-new')) return;
  markSeen();
  openModal({ id: 'whats-new' });
  // Same modal-focus discipline as the help overlay: drop the active tile's
  // visual focus and give the keyboard to the dialog.
  deps.setFocusedTile(null);
}

export function closeWhatsNew() {
  releaseFocus(pageEl('whats-new'));
  // flushSync because this also runs from plain listeners (ModalShell's
  // Escape handler): an ordinary store write lands a microtask later and
  // focusActiveTerm() would run while the dialog is still visible.
  flushSync(() => closeModal('whats-new'));
  deps.focusActiveTerm();
}

export function initWhatsNew(injected: WhatsNewDeps) {
  deps = injected;
}
