// ---------- keyboard-shortcuts help overlay (⌘/) ----------
//
// Moved verbatim from main.js; focus callbacks injected via init.

import { isMac } from '../../lib/platform.js';
import { shortcutGroups } from '../../lib/shortcuts.js';
import { registerModal } from './registry.js';

let deps = {
  setFocusedTile: () => {},
  focusActiveTerm: () => {},
};

export const helpEl = document.getElementById('help-overlay');
const helpGroupsEl = document.getElementById('help-overlay-groups');
const helpCloseBtn = document.getElementById('help-overlay-close');
let helpRendered = false;

function renderHelpOverlay() {
  helpGroupsEl.innerHTML = '';
  for (const group of shortcutGroups({ isMac })) {
    const sec = document.createElement('section');
    const h = document.createElement('h4');
    h.textContent = group.title;
    sec.appendChild(h);
    const dl = document.createElement('dl');
    for (const item of group.items) {
      const dt = document.createElement('dt');
      const kbd = document.createElement('kbd');
      kbd.textContent = item.keys;
      dt.appendChild(kbd);
      const dd = document.createElement('dd');
      dd.textContent = item.label;
      dl.append(dt, dd);
    }
    sec.appendChild(dl);
    helpGroupsEl.appendChild(sec);
  }
}

export function openHelpOverlay() {
  if (!helpRendered) {
    renderHelpOverlay(); // static content — render once
    helpRendered = true;
  }
  helpEl.classList.remove('hidden');
  // Same modal-focus discipline as the palette: drop the active tile's
  // visual focus and give the keyboard to the overlay.
  deps.setFocusedTile(null);
  helpCloseBtn.focus();
}

export function closeHelpOverlay() {
  helpEl.classList.add('hidden');
  deps.focusActiveTerm();
}

// toggleHelpOverlay backs the native menu item (menu:keyboard-shortcuts):
// on macOS the menu accelerator owns ⌘/, so open AND close must both be
// reachable through this one entry point.
export function toggleHelpOverlay() {
  if (helpEl.classList.contains('hidden')) openHelpOverlay();
  else closeHelpOverlay();
}

export function initHelpOverlay(injected) {
  deps = injected;
  registerModal(helpEl);
  helpCloseBtn.addEventListener('click', closeHelpOverlay);
  helpEl.addEventListener('mousedown', (e) => {
    // The overlay element is the full-viewport backdrop — clicking it
    // (not the panel) dismisses.
    if (e.target === helpEl) closeHelpOverlay();
  });
}
