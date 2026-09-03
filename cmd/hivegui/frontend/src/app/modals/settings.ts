// ---------- settings: the non-React half ----------
//
// The panel renders from components/modals/Settings.tsx (Phase 3). What
// stays here is the open/close pair every caller already imports from
// this path (keyboard.ts, events.ts, main.ts, the command palette), the
// argv splitter Go's validator mirrors, and the OS theme watch — which
// is not part of the modal at all: it runs whether or not Settings has
// ever been opened.
//
// Focus-pipeline callbacks are injected via initSettings(deps) — this
// module must never import the focus pipeline directly (main.ts owns
// that wiring).

import { flushSync } from 'react-dom';
import { applyTheme, readTheme } from '../../theme/theme.js';
import { applyXtermTheme } from '../session-term.js';
import { closeModal, isModalOpen, openModal } from '../../store/store.js';
import { pageEl } from '../el.js';
import { registerModal } from './registry.js';
import { releaseFocus } from './focus-trap.js';

// Narrow on purpose: this modal needs exactly two callbacks off the
// focus pipeline, so it names those two rather than the whole module.
export interface SettingsDeps {
  setFocusedTile: (id: string | null) => void;
  refocusActiveTerm: () => void;
}

let deps: SettingsDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
};

// keyboard.ts and the tests key off this element; it is the dialog root
// declared in index.html.
export const settingsEl = pageEl('settings');

/** Splits a command line into argv on whitespace.
 *
 * ponytail: whitespace split, no quote handling. There is no
 * shell-word splitter in this repo and `claude --model haiku` is the
 * shape people actually type. agents.json stores a real array, so an
 * argument containing spaces stays hand-editable in the file. Upgrade
 * path if this bites: a real tokenizer here and in Go's validator.
 *
 * Takes `string | null` because a test asserts splitCommand(null) is [];
 * the String(line || '') guard is part of the contract, not defensive
 * padding around a narrower one.
 */
export function splitCommand(line: string | null): string[] {
  return String(line || '')
    .trim()
    .split(/\s+/)
    .filter(Boolean);
}

export function openSettings() {
  // Re-entry must not discard an in-progress draft. This is reachable on
  // macOS: the native File ▸ Settings… accelerator consumes ⌘, before the
  // webview's keydown listener sees it (same precedence that makes the
  // '?' branch in keyboard.ts dead on darwin, per menu_darwin.go), so
  // pressing ⌘, with the modal already open arrives here as
  // menu:settings rather than as the toggle-to-close in the keydown
  // gate. Without this guard the modal would remount and silently wipe
  // the user's unsaved edits.
  if (isModalOpen('settings')) return;
  // Drop the active tile's visual focus — the dialog owns the keyboard,
  // and the component pulls focus onto its close button on mount.
  deps.setFocusedTile(null);
  openModal({ id: 'settings' });
}

export function closeSettings() {
  // Blur before hiding, then hand focus back: refocusActiveTerm() bails
  // when activeElement is an INPUT (lib/focus.ts), and unmounting the
  // dialog does not synchronously move focus out of it in every engine.
  // The order is why this lives here and not in a component effect.
  releaseFocus(settingsEl);
  // flushSync for the same reason closeLauncher uses it: this is called
  // from plain listeners (keyboard.ts's window handler, the shell's
  // Escape), so an ordinary store write lands a microtask later and
  // refocusActiveTerm() would run while #settings is still visible —
  // app/focus.ts refuses to touch the terminal while a modal is open.
  flushSync(() => closeModal('settings'));
  deps.refocusActiveTerm();
}

// 'system' resolves prefers-color-scheme at the moment it is applied,
// and since phase 6 it is the default for every new install — so without
// a listener, flipping the OS to dark leaves Hive light until it is
// restarted. Re-applies only when the stored choice is still 'system':
// an explicit preset is a decision the OS does not get to override.
// Same trio as the component's theme picker minus the store write,
// because the stored value ('system') is exactly what has not changed.
export function initThemeWatch(): void {
  const mq = window.matchMedia?.('(prefers-color-scheme: dark)');
  // jsdom has no matchMedia, and older webviews expose the legacy
  // addListener only; neither is worth a shim for a live-preview nicety.
  if (!mq?.addEventListener) return;
  mq.addEventListener('change', () => {
    if (readTheme() !== 'system') return;
    applyTheme('system');
    applyXtermTheme();
  });
}

export function initSettings(injected: SettingsDeps) {
  deps = injected;
  // Still registered: anyModalOpen() answers "does a modal own the
  // keyboard?" off the `.hidden` class of every registered root, and
  // #settings keeps that class (toggled by the island's layout effect).
  registerModal(settingsEl);
}
