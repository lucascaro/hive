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

// The last overrides sent to Go, as JSON. Null until the first send.
let sentMenu: string | null = null;
// The menu commands initKeymapSync was given.
let menuIds: readonly string[] = [];
// Sends go one at a time, in order: Go installs whatever arrives last, so
// two in flight must not land newest-first.
let queue: Promise<unknown> = Promise.resolve();

function pushMenu(keymap: Keymap, ids: readonly string[], force = false): void {
  if (!isMac) return; // no native menu elsewhere (menu_other.go)
  const overrides = menuAcceleratorOverrides(ids, keymap);
  const json = JSON.stringify(overrides);
  if (!force && json === (sentMenu ?? '{}')) return;
  const before = sentMenu;
  sentMenu = json;
  queue = queue
    .then(() => SetMenuAccelerators(overrides))
    .catch((e: unknown) => {
      console.warn('updating the menu shortcuts failed', e);
      // Go never took it, so the next keymap change must send again. Only
      // if nothing newer was sent meanwhile: that one supersedes this.
      if (sentMenu === json) sentMenu = before;
    });
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
  menuIds = deps.menuCommands;
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
 * unchanged file rebuilds nothing.
 *
 * The first load of a page also tells Go the menu, whatever it read: Go
 * outlives a webview reload (Debug › trace toggle), so the menu may still
 * carry overrides an earlier page sent while this one starts from the
 * defaults. One rebuild per page load buys a menu that always matches the
 * keys. */
export async function loadKeymap(): Promise<void> {
  try {
    const next = ((await GetKeymap()) ?? {}) as Keymap;
    if (JSON.stringify(next) !== JSON.stringify(appStore.getState().keymap))
      setKeymap(next);
  } catch (e) {
    console.warn('loading keymap.json failed; using the default shortcuts', e);
  }
  if (sentMenu === null) pushMenu(appStore.getState().keymap, menuIds, true);
}

/** Test-only. */
export function resetKeymapSyncForTest(): void {
  unsubscribe?.();
  unsubscribe = null;
  sentMenu = null;
  menuIds = [];
  queue = Promise.resolve();
}

/** Test-only: resolves once every queued menu update has settled. */
export function menuQueueSettledForTest(): Promise<unknown> {
  return queue;
}
