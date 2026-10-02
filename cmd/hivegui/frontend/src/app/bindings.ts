// The live keymap (spec 477): lib/bindings.ts's resolver bound to the
// store, so callers ask about "the user's shortcuts" without threading
// the keymap through. Every function reads the store when called.

import {
  chordsOfIn,
  effectiveFor,
  isDefaultIn,
  labelIn,
  shortcutsIn,
  type Effective,
  type Keymap,
} from '../lib/bindings.js';
import { isMac as platformIsMac } from '../lib/platform.js';
import { appStore, useAppStore } from '../store/store.js';

export {
  DEFAULT_APP_BINDINGS,
  HELP_CHORD,
  keymapHalf,
  RESERVED_CHORDS,
  type Binding,
} from '../lib/bindings.js';

const keymap = () => appStore.getState().keymap;

/** The app's bindings under the user's keymap. Keydown reads this on
 * every key; it is cached on the keymap object. */
export function effectiveAppBindings(
  isMac: boolean = platformIsMac,
): Effective {
  return effectiveFor(keymap(), isMac);
}

/** Every chord that runs a command, layout spellings included. Modal
 * scopes use it so the key that opened a modal also closes it. */
export function chordsOf(id: string, isMac: boolean = platformIsMac): string[] {
  return chordsOfIn(keymap(), id, isMac);
}

/** A command's shortcuts, one chord each (the first spelling). */
export function shortcutsFor(
  id: string,
  isMac: boolean = platformIsMac,
): string[] {
  return shortcutsIn(keymap(), id, isMac);
}

/** Whether a command has its shipped shortcuts. */
export function hasDefaultShortcut(
  id: string,
  isMac: boolean = platformIsMac,
): boolean {
  return isDefaultIn(keymap(), id, isMac);
}

/** What to show for a command's shortcut, or '' when it has none. */
export function shortcutLabel(
  id: string,
  isMac: boolean = platformIsMac,
): string {
  return labelIn(keymap(), id, isMac);
}

/** Calls fn whenever the keymap object changes (not on other store
 * updates). Returns the unsubscribe. */
export function subscribeKeymap(fn: (keymap: Keymap) => void): () => void {
  let prev = keymap();
  return appStore.subscribe((s) => {
    if (s.keymap === prev) return;
    prev = s.keymap;
    fn(prev);
  });
}

/** shortcutLabel for a component: re-renders when the keymap changes. */
export function useShortcutLabel(
  id: string,
  isMac: boolean = platformIsMac,
): string {
  const km = useAppStore((s) => s.keymap);
  return labelIn(km, id, isMac);
}
