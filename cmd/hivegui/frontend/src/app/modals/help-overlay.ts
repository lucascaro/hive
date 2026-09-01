// ---------- keyboard-shortcuts help overlay (⌘/) ----------
//
// Built on the dialog primitive; focus callbacks injected via init.

import { isMac } from '../../lib/platform.js';
import { shortcutGroups } from '../../lib/shortcuts.js';
import { dialog } from '../../ui/dialog.js';
import { releaseFocus } from './focus-trap.js';
import { kbd } from '../../ui/kbd.js';

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

const helpGroupsEl = document.createElement('div');
helpGroupsEl.id = 'help-overlay-groups';

const dlg = dialog({
  id: 'help-overlay',
  title: 'Keyboard shortcuts',
  size: 'lg',
  body: [helpGroupsEl],
  hints: [kbd('[esc]'), document.createTextNode(' close')],
  onClose: () => closeHelpOverlay(),
});
dlg.panel.id = 'help-overlay-panel';
dlg.el
  .querySelector('.hv-dialog__close')
  ?.setAttribute('id', 'help-overlay-close');
export const helpEl = dlg.el;

let helpRendered = false;

function renderHelpOverlay() {
  helpGroupsEl.replaceChildren();
  for (const group of shortcutGroups({ isMac })) {
    const sec = document.createElement('section');
    const h = document.createElement('h4');
    h.textContent = group.title;
    sec.appendChild(h);
    const dl = document.createElement('dl');
    for (const item of group.items) {
      const dt = document.createElement('dt');
      // kbd() is the only way a hint is formatted, so this overlay and
      // the dialog footers can never drift apart.
      dt.appendChild(kbd(item.keys));
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
  dlg.show();
  // Same modal-focus discipline as the palette: drop the active tile's
  // visual focus and give the keyboard to the overlay.
  deps.setFocusedTile(null);
  document.getElementById('help-overlay-close')?.focus();
}

export function closeHelpOverlay() {
  // Before hide(), same as the other modals: focus left on a
  // display:none element resolves to <body> and strands the keyboard.
  releaseFocus(helpEl);
  dlg.hide();
  deps.focusActiveTerm();
}

// toggleHelpOverlay backs the native menu item (menu:keyboard-shortcuts):
// on macOS the menu accelerator owns ⌘/, so open AND close must both be
// reachable through this one entry point.
export function toggleHelpOverlay() {
  if (dlg.isOpen()) closeHelpOverlay();
  else openHelpOverlay();
}

export function initHelpOverlay(injected: HelpOverlayDeps) {
  deps = injected;
  document.getElementById('app')?.append(helpEl);
}
