// The ⌘F entry point: resolves "the focused session" and applies the
// view gate, so keyboard.ts and the native menu share one path.
//
// Separate from find-box.ts because that module is about one session's
// box and is unit-testable with a plain deps object; this one reaches
// into app state to decide *which* session the chord applies to.

import { appStore } from '../store/store.js';
import { VIEW_SINGLE } from '../lib/view.js';
import { closeFindBox, openFindBox } from './find-box.js';

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
 * Opens the find box, or — when it is already open — refocuses it with
 * the query selected, so typing replaces it. That is the find-field
 * convention in browsers and editors; ⌘F never closes the box. Escape
 * and the close control do.
 *
 * The one entry point for the chord, the macOS menu item (which fires on
 * every press, since the native accelerator intercepts before the
 * webview) and the command palette.
 */
export function openFindInSession() {
  const id = targetSession();
  if (id) openFindBox(id);
}

/**
 * Closes every open find box except the one on `keep`.
 *
 * Called on view change (the box is single-view only, and a grid flip
 * would otherwise strand its viewport claim) and when another session
 * takes focus: search belongs to the session it was opened on, so
 * leaving that session ends it.
 */
export function closeFindForAllSessions(keep: string | null = null) {
  for (const [id, chrome] of appStore.getState().tileChrome) {
    if (chrome.find && id !== keep) closeFindBox(id);
  }
}
