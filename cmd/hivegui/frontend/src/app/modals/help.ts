// ---------- the Help modal (sidebar help icon): the non-React half ----------
//
// Modelled on whats-new.ts, which has the same shape: a modal with no daemon
// round-trip, opened from one control and closed on Escape. What it drops is
// the read receipt — Help has nothing to mark as seen.
//
// The modal id is 'help-modal', NOT 'help'. 'help' belongs to the ⌘/
// keyboard-shortcuts overlay (app/modals/help-overlay.ts), and the two are
// separate surfaces: this one shows a shortcuts row that hands off to that
// one. Sharing an id would make them mutually exclusive through isModalOpen
// and break the handoff.

import { flushSync } from 'react-dom';
import { closeModal, isModalOpen, openModal } from '../../store/store.js';
import { releaseFocus } from '../../lib/focus-trap.js';
import { openHelpOverlay } from './help-overlay.js';
import { pageEl } from '../el.js';

export interface HelpDeps {
  setFocusedTile: (id: string | null) => void;
  focusActiveTerm: () => void;
}

let deps: HelpDeps = {
  setFocusedTile: () => {},
  focusActiveTerm: () => {},
};

export function openHelp() {
  if (isModalOpen('help-modal')) return;
  openModal({ id: 'help-modal' });
  // Same modal-focus discipline as What's New and the help overlay: drop the
  // active tile's visual focus and give the keyboard to the dialog.
  deps.setFocusedTile(null);
}

export function closeHelp() {
  releaseFocus(pageEl('help-modal'));
  // flushSync because this also runs from plain listeners (ModalShell's
  // Escape handler): an ordinary store write lands a microtask later and
  // focusActiveTerm() would run while the dialog is still visible.
  flushSync(() => closeModal('help-modal'));
  deps.focusActiveTerm();
}

export function initHelp(injected: HelpDeps) {
  deps = injected;
}

// The Help modal's one outbound action: its shortcuts row and the ⌘/ key
// while it is open both land here. Close first — that releases this dialog's
// focus trap before the overlay acquires its own — then open the overlay.
export function handOffToShortcuts(): void {
  closeHelp();
  openHelpOverlay();
}
