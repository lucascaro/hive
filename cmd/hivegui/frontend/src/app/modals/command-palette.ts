// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// ---------- command palette: the non-React half ----------
//
// The palette renders from components/modals/CommandPalette.tsx. What
// stays here is the open/close pair the command bus runs, and the row
// list: every registered command with a title (app/command-registry.ts),
// core first and plugins after, so the palette runs exactly what the
// matching key and menu item run.

import { flushSync } from 'react-dom';
import {
  closeModal,
  openModal,
  appStore,
  type ModalEntry,
} from '../../store/store.js';
import { shortcutLabel } from '../bindings.js';
import { listCommands, runCommand } from '../command-registry.js';

// One row the palette renders.
export interface PaletteCommand {
  id: string;
  name: string;
  shortcut: string;
  run: () => void;
}

export interface CommandPaletteDeps {
  focusActiveTerm: () => void;
  setFocusedTile: (id: string | null) => void;
  goToSession: (id: string) => void;
}

let deps: CommandPaletteDeps = {
  focusActiveTerm: () => {},
  setFocusedTile: () => {},
  goToSession: () => {},
};

// One box, two lists (spec 544): ⇧⌘P lists commands, ⌘P lists sessions,
// and each chord swaps the other's list in place — VS Code's file-vs-command
// quick open. The mode rides on the modal entry.
export type PaletteMode = 'commands' | 'sessions';

function paletteEntry() {
  return appStore
    .getState()
    .modals.find(
      (m): m is Extract<ModalEntry, { id: 'command-palette' }> =>
        m.id === 'command-palette',
    );
}

// Core labels are derived from the bindings under the user's keymap, per
// call, so a rebind shows on the next opening; plugin commands bring
// their own (resolved against the same keymap by the plugin host).

export function paletteCommands(): PaletteCommand[] {
  return listCommands().flatMap((c) =>
    c.title === undefined
      ? []
      : [
          {
            id: c.id,
            name: c.title,
            shortcut: c.shortcut ?? shortcutLabel(c.id),
            run: () => {
              runCommand(c.id);
            },
          },
        ],
  );
}

// The opener for either chord. Closed: open in `mode`. Open in `mode`:
// close (the chord toggles). Open in the other mode: swap in place, keeping
// the box on screen and focus in its input (a new entry remounts the body,
// which clears the query).
function openPaletteMode(mode: PaletteMode) {
  const cur = paletteEntry();
  if (!cur) {
    openModal({ id: 'command-palette', mode });
    deps.setFocusedTile(null);
  } else if ((cur.mode ?? 'commands') === mode) {
    closeCommandPalette();
  } else {
    openModal({ id: 'command-palette', mode });
  }
}

export const openCommandPalette = () => openPaletteMode('commands');
export const openSessionFinder = () => openPaletteMode('sessions');

// Enter on a session row: close first (focus goes home), then jump.
// Deferred so the close finishes before the jump moves focus to the target
// terminal, the same order a palette command uses.
export function activatePaletteSession(id: string): void {
  closeCommandPalette();
  setTimeout(() => deps.goToSession(id), 0);
}

export function closeCommandPalette() {
  // Blur first: focusActiveTerm() bails when activeElement is an INPUT
  // (lib/focus.ts), and unmounting the palette does not synchronously
  // move focus off its search box in every engine.
  const input = document.getElementById('command-palette-input');
  if (input instanceof HTMLElement) input.blur();
  // flushSync for the same reason closeSettings does it: this is called
  // from plain listeners, and a store write a microtask later would let
  // focusActiveTerm() run while the palette is still visible.
  flushSync(() => closeModal('command-palette'));
  deps.focusActiveTerm();
}

export function initCommandPalette(injected: CommandPaletteDeps) {
  deps = injected;
}
