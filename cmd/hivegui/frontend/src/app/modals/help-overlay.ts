// ---------- keyboard-shortcuts help overlay (⌘/): the non-React half ----------
//
// The overlay renders from components/modals/HelpOverlay.tsx (Phase 4).
// What stays here is the open/close/toggle trio every caller already
// imports from this path (keyboard.ts, main.ts, the command palette) and
// the focus-pipeline callbacks main.ts injects.

import { flushSync } from 'react-dom';
import { closeModal, isModalOpen, openModal } from '../../store/store.js';
import { pageEl } from '../el.js';
import { releaseFocus } from '../../lib/focus-trap.js';

// Narrow on purpose: this overlay needs exactly two callbacks off the
// focus pipeline, so it names those two rather than the whole module.
export interface HelpOverlayDeps {
  setFocusedTile: (id: string | null) => void;
  focusActiveTerm: () => void;
}

let deps: HelpOverlayDeps = {
  setFocusedTile: () => {},
  focusActiveTerm: () => {},
};

export function openHelpOverlay() {
  if (isModalOpen('help')) return;
  openModal({ id: 'help' });
  // Same modal-focus discipline as the palette: drop the active tile's
  // visual focus and give the keyboard to the overlay, which pulls it
  // onto its close button on mount.
  deps.setFocusedTile(null);
}

export function closeHelpOverlay() {
  // Before the unmount, same as the other modals: focus left on a
  // removed element resolves to <body> and strands the keyboard.
  releaseFocus(pageEl('help-overlay'));
  // flushSync because this runs from plain listeners (keyboard.ts's
  // window handler, the native menu item): an ordinary store write lands
  // a microtask later and focusActiveTerm() would run while the overlay
  // is still visible, which app/focus.ts refuses to act through.
  flushSync(() => closeModal('help'));
  deps.focusActiveTerm();
}

// toggleHelpOverlay backs the native menu item (menu:keyboard-shortcuts):
// on macOS the menu accelerator owns ⌘/, so open AND close must both be
// reachable through this one entry point.
export function toggleHelpOverlay() {
  if (isModalOpen('help')) closeHelpOverlay();
  else openHelpOverlay();
}

export function initHelpOverlay(injected: HelpOverlayDeps) {
  deps = injected;
}
