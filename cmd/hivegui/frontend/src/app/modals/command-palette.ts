// ---------- command palette: the non-React half ----------
//
// The palette renders from components/modals/CommandPalette.tsx
// (Phase 4). What stays here is the open/close pair keyboard.ts imports,
// and the command table itself: main.ts builds it (the actions live
// there) and hands it over at init, so it has to be reachable from
// outside React.

import { flushSync } from 'react-dom';
import { closeModal, isModalOpen, openModal } from '../../store/store.js';

// One row of the command table main.ts builds and hands over.
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
let commandTable: PaletteCommand[] = [];

// paletteCommands is what the component renders. A getter rather than an
// export of the array itself: initCommandPalette runs after the module
// graph is evaluated, so a bound reference would be the empty seed.
export function paletteCommands(): PaletteCommand[] {
  return commandTable;
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

export function initCommandPalette({
  commands,
  ...injected
}: CommandPaletteDeps & { commands: PaletteCommand[] }) {
  deps = injected;
  commandTable = commands;
}
