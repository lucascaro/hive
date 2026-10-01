// Status-bar controller with a persistent slot and a transient flash
// layer. Pure (all effects injected) so the semantics are unit-testable
// without a DOM.
//
// The status bar serves two kinds of messages that used to clobber each
// other through a bare textContent write:
//   - persistent state ("connected", "control disconnected", the name
//     of the session you just switched to) — owned by set();
//   - per-action feedback ("copy failed: …", "creating session…") —
//     owned by flash(), which auto-reverts to the persistent slot.
//
// Guarantees:
//   - a flash is visible for at least FLASH_MIN_MS before a set() may
//     replace it (so an error isn't wiped by nav feedback a frame later);
//   - a flash auto-expires (errors linger longer than info);
//   - set() during an active flash is never lost — it lands in the
//     persistent slot and renders when the flash ends.

export const FLASH_ERROR_MS = 6000;
export const FLASH_INFO_MS = 2500;
export const FLASH_MIN_MS = 1500;

export interface StatusDeps {
  render: (text: string, isError: boolean) => void;
  // Timer handles are opaque numbers so a test can inject a counter;
  // the browser's setTimeout satisfies this too.
  setTimer: (fn: () => void, ms: number) => number;
  clearTimer: (id: number) => void;
  now: () => number;
}

export interface Status {
  set(text: string, isError?: boolean): void;
  flash(text: string, isError?: boolean): void;
}

export function createStatus({
  render,
  setTimer,
  clearTimer,
  now,
}: StatusDeps): Status {
  let persistent = { text: '', isError: false };
  let flashActive = false;
  let flashTimer = 0;
  let flashStarted = 0;

  function endFlash(): void {
    flashActive = false;
    flashTimer = 0;
    render(persistent.text, persistent.isError);
  }

  function set(text: string, isError = false): void {
    persistent = { text, isError };
    if (!flashActive) {
      render(text, isError);
      return;
    }
    if (now() - flashStarted >= FLASH_MIN_MS) {
      clearTimer(flashTimer);
      endFlash();
    }
    // Else: the flash keeps the screen; the stored persistent text
    // renders when it expires.
  }

  function flash(text: string, isError = false): void {
    if (flashTimer) clearTimer(flashTimer);
    flashActive = true;
    flashStarted = now();
    render(text, isError);
    flashTimer = setTimer(endFlash, isError ? FLASH_ERROR_MS : FLASH_INFO_MS);
  }

  return { set, flash };
}

// The status bar's right slot. patterns.md > Keyboard hints: "the status
// bar right slot shows the current mode's top 1-2 shortcuts". Kept here,
// beside the controller, and pure so a test can assert the table without
// a DOM. The keys are the user's current ones (lib/bindings.ts, spec
// 477): a hint that names a chord nothing is bound to is worse than no
// hint at all (AGENTS.md > Consistency), so a command with no shortcut
// drops its hint.
import { EMPTY_KEYMAP, isDefaultIn, labelIn, type Keymap } from './bindings.js';
import type { ViewMode } from './view.js';

export interface ModeHint {
  key: string;
  label: string;
}

const ARROWS = ['prev-session', 'next-session', 'grid-left', 'grid-right'];

export function modeHints(
  view: ViewMode,
  mac: boolean,
  keymap: Keymap = EMPTY_KEYMAP,
): ModeHint[] {
  const hint = (command: string, label: string): ModeHint[] => {
    const key = labelIn(keymap, command, mac);
    return key ? [{ key, label }] : [];
  };
  if (view === 'grid-all' || view === 'grid-project') {
    return [
      // Each grid is toggled back to a single pane by the chord that
      // opened it: ⌘G for the project grid, ⇧⌘G for the all-sessions
      // grid. Naming plain ⌘G in grid-all would advertise a chord that
      // switches grids instead of focusing.
      ...hint(
        view === 'grid-all' ? 'toggle-all-grid' : 'toggle-project-grid',
        'focus',
      ),
      // Four commands in one hint. Off macOS the arrows spelled out would
      // be longer than the slot; the word carries the same meaning. Once
      // any of them is rebound there is no short form, so it is dropped.
      ...(ARROWS.every((c) => isDefaultIn(keymap, c, mac))
        ? [{ key: mac ? '⌘↑↓←→' : 'Ctrl+Arrows', label: 'move' }]
        : []),
    ];
  }
  return [
    ...hint('toggle-project-grid', 'grid'),
    ...hint('command-palette', 'actions'),
  ];
}
