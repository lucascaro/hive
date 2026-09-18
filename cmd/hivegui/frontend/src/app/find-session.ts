// The ⌘F entry point: resolves "the focused session" and applies the
// view gate, so keyboard.ts and the native menu share one path.
//
// Separate from find-box.ts because that module is about one session's
// box and is unit-testable with a plain deps object; this one reaches
// into app state to decide *which* session the chord applies to.

import { appStore } from '../store/store.js';
import { VIEW_SINGLE } from '../lib/view.js';
import { closeFindBox, openFindBox, toggleFindBox } from './find-box.js';

/**
 * The session ⌘F applies to: the active one, in single view only.
 *
 * Gated to single view per spec 431's non-goals. Not merely cosmetic:
 * the mode snap force-sets _followBottom (view-scroll.ts), so a ⌘G
 * mid-search would revoke the box's viewport claim and leave the
 * restore path never firing.
 */
function targetSession(): string | null {
  const s = appStore.getState();
  if (s.view !== VIEW_SINGLE) return null;
  return s.activeId || null;
}

/** True while a find box has keyboard focus. */
export function findBoxActive(): boolean {
  const el = document.activeElement as HTMLElement | null;
  return Boolean(el?.hasAttribute?.('data-find-input'));
}

/**
 * Toggles the find box for the focused session.
 *
 * Toggle rather than open because on macOS the native menu accelerator
 * is the entry point and fires on every press — the same reason the
 * help overlay exposes a toggle.
 */
export function toggleFindInSession() {
  const id = targetSession();
  if (id) toggleFindBox(id);
}

/** Opens (or refocuses) the box. The menu item's target. */
export function openFindInSession() {
  const id = targetSession();
  if (id) openFindBox(id);
}

/**
 * Closes any open find box.
 *
 * Called on view change: the box is single-view only, and a grid flip
 * would otherwise strand its viewport claim.
 */
export function closeFindForAllSessions() {
  for (const [id, chrome] of appStore.getState().tileChrome) {
    if (chrome.find) closeFindBox(id);
  }
}
