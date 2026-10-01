// ---------- command palette: the non-React half ----------
//
// The palette renders from components/modals/CommandPalette.tsx. What
// stays here is the open/close pair the command bus runs, and the row
// list: every registered command with a title (app/command-registry.ts),
// core first and plugins after, so the palette runs exactly what the
// matching key and menu item run.

import { flushSync } from 'react-dom';
import { isMac } from '../../lib/platform.js';
import { paletteShortcuts } from '../../lib/shortcuts.js';
import { closeModal, isModalOpen, openModal } from '../../store/store.js';
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
}

let deps: CommandPaletteDeps = {
  focusActiveTerm: () => {},
};

// Core labels come from lib/shortcuts.ts by command id, so the palette
// and the ⌘/ overlay cannot drift from each other; plugin commands bring
// their own.
const CORE_KEYS = paletteShortcuts({ isMac });

export function paletteCommands(): PaletteCommand[] {
  return listCommands().flatMap((c) =>
    c.title === undefined
      ? []
      : [
          {
            id: c.id,
            name: c.title,
            shortcut: c.shortcut ?? CORE_KEYS[c.id] ?? '',
            run: () => {
              runCommand(c.id);
            },
          },
        ],
  );
}

export function openCommandPalette() {
  if (isModalOpen('command-palette')) return;
  openModal({ id: 'command-palette' });
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
