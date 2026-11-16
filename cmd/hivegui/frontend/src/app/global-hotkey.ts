// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The GUI's half of the menu bar's dictation capture HUD (spec 553).
//
// 1. Focus reports. Whenever this window shows the user a session —
//    a switch, or the window coming back to the front — the daemon is
//    told, and the HUD offers the newest report as its default target.
//    The daemon ignores a repeat for the same session within a second,
//    so reporting on every window focus costs nothing.
//
// 2. The hotkey. keymap.json is the source of truth for the HUD's chord,
//    like every other binding; the daemon keeps a copy only so the menu
//    bar, which reads nothing from disk, can learn it with no GUI open.
//    This keeps the copy in step: whenever the keymap or the daemon's
//    value changes, the converted chord is sent if, and only if, the two
//    differ. A GLOBAL_HOTKEY equal to the keymap sends nothing, so the
//    daemon's broadcast cannot loop back into another send.

import { EventsOn, ReportFocusedSession, SetGlobalHotkey } from '../bridge.js';
import { chordsOfIn } from '../lib/bindings.js';
import {
  desiredHotkey,
  sameHotkey,
  type WireHotkey,
} from '../lib/carbon-keys.js';
import { isMac } from '../lib/platform.js';
import { appStore } from '../store/store.js';
import { subscribeKeymap } from './bindings.js';

export const DICTATE_COMMAND = 'dictate-hud';

/** Tells the daemon this window is showing session id. */
export function reportFocus(id: string | null): void {
  // Only the macOS menu-bar HUD reads focus; elsewhere a report would
  // just fan a SESSION_EVENT out to every client.
  if (!id || !isMac) return;
  ReportFocusedSession(id).catch(() => {
    // No control connection yet: the next switch or window focus
    // reports again.
  });
}

// The daemon's last GLOBAL_HOTKEY, or null until one arrives. An older
// daemon never sends one and would refuse SET_GLOBAL_HOTKEY, so nothing
// is sent until it does.
let received: WireHotkey | null = null;
// The value last sent and not yet echoed back, so a burst of keymap
// changes does not resend the same chord while the broadcast is in
// flight.
let pending: WireHotkey | null = null;

function reconcile(): void {
  if (!isMac || received === null) return;
  const want = desiredHotkey(
    chordsOfIn(appStore.getState().keymap, DICTATE_COMMAND, true),
  );
  if (sameHotkey(want, received)) {
    pending = null;
    return;
  }
  if (pending && sameHotkey(want, pending)) return;
  pending = want;
  SetGlobalHotkey(want.key_code, want.modifiers, want.enabled).catch(
    (e: unknown) => {
      pending = null;
      console.warn('syncing the dictation hotkey failed', e);
    },
  );
}

function readHotkey(raw: unknown): WireHotkey | null {
  const v = (typeof raw === 'string' ? JSON.parse(raw) : raw) as Partial<
    WireHotkey & { keyCode: number }
  > | null;
  if (!v || typeof v !== 'object') return null;
  return {
    key_code: Number(v.key_code ?? v.keyCode ?? 0),
    modifiers: Number(v.modifiers ?? 0),
    enabled: v.enabled === true,
  };
}

let wired = false;

/** Wires focus reports and the hotkey reconcile. Call once at boot. */
export function initGlobalHotkey(): void {
  if (wired) return;
  wired = true;
  if (!isMac) return;
  window.addEventListener('focus', () =>
    reportFocus(appStore.getState().activeId),
  );
  EventsOn('hotkey:global', (payload: unknown) => {
    try {
      received = readHotkey(payload);
    } catch {
      return;
    }
    reconcile();
  });
  subscribeKeymap(() => reconcile());
}

/** Test seam: forget what the daemon said and what was sent. */
export function _resetGlobalHotkeyForTest(): void {
  received = null;
  pending = null;
  wired = false;
}
