// Loads the user's keymap (spec 477) and keeps the surfaces that are not
// React components in step with it. Dispatch, the palette, the help
// overlay and every component already derive from the store's keymap;
// what is left here is the boot load and the imperative surfaces: the
// native macOS menu (Go builds it, so it must be told), the status bar's
// hint slot, and index.html's new-project button title.
//
// The keyboard listener is live before GetKeymap answers, so until then
// the shipped defaults fire — keydown and native menu alike. That gap is
// a few milliseconds at boot and accepted: Go reading keymap.json itself
// would mean a second copy of the chord grammar.

import { GetKeymap, SetMenuAccelerators } from '../bridge.js';
import { menuAcceleratorOverrides, type Keymap } from '../lib/bindings.js';
import { isMac } from '../lib/platform.js';
import { appStore, setKeymap } from '../store/store.js';
import { shortcutLabel, subscribeKeymap } from './bindings.js';

export interface KeymapSyncDeps {
  refreshModeHint: () => void;
  /** The commands with a native menu item, by id (main.tsx derives them
   * from MENU_COMMANDS, plus the two items Go handles itself). */
  menuCommands: readonly string[];
}

// The last overrides sent to Go, as JSON. Null until the first send: with
// no overrides there is nothing to tell a menu built from the defaults.
let sentMenu: string | null = null;

function pushMenu(keymap: Keymap, ids: readonly string[]): void {
  if (!isMac) return; // no native menu elsewhere (menu_other.go)
  const overrides = menuAcceleratorOverrides(ids, keymap);
  const json = JSON.stringify(overrides);
  if (json === (sentMenu ?? '{}')) return;
  sentMenu = json;
  SetMenuAccelerators(overrides).catch((e: unknown) =>
    console.warn('updating the menu shortcuts failed', e),
  );
}

function titleNewProjectButton(): void {
  const btn = document.getElementById('new-project-btn');
  if (!btn) return;
  const key = shortcutLabel('new-project');
  btn.title = key ? `New project (${key})` : 'New project';
}

let unsubscribe: (() => void) | null = null;

/** Wires the imperative surfaces to the keymap and applies the current
 * one to them. Call once at boot, before loadKeymap. */
export function initKeymapSync(deps: KeymapSyncDeps): void {
  unsubscribe?.();
  titleNewProjectButton();
  unsubscribe = subscribeKeymap((keymap) => {
    pushMenu(keymap, deps.menuCommands);
    deps.refreshModeHint();
    titleNewProjectButton();
  });
}

/** Reads keymap.json into the store. A missing file is the empty keymap;
 * a failure leaves the defaults in place — the app must start. The store
 * is only written when the keymap actually differs, so a re-read of an
 * unchanged file rebuilds nothing. */
export async function loadKeymap(): Promise<void> {
  let next: Keymap;
  try {
    next = ((await GetKeymap()) ?? {}) as Keymap;
  } catch (e) {
    console.warn('loading keymap.json failed; using the default shortcuts', e);
    return;
  }
  if (JSON.stringify(next) === JSON.stringify(appStore.getState().keymap))
    return;
  setKeymap(next);
}

/** Test-only. */
export function resetKeymapSyncForTest(): void {
  unsubscribe?.();
  unsubscribe = null;
  sentMenu = null;
}
