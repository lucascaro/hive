// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The GUI's half of the dictation box: the menu bar's on macOS (specs
// 553, 573), each Hive window's on Windows (spec 575).
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
//    differ. A DICTATION whose chord equals the keymap sends nothing, so
//    the daemon's broadcast cannot loop back into another send.
//
// 3. The Handy switch (spec 573). The daemon owns it; Settings shows the
//    last DICTATION's value and writes through SetHandyAutoStart, which
//    sends only the switch, so it can never clobber the chord (and the
//    chord sync above never clobbers the switch).

import { EventsOn, ReportFocusedSession, SetGlobalHotkey } from '../bridge.js';
import { chordsOfIn } from '../lib/bindings.js';
import {
  desiredHotkey,
  sameHotkey,
  type WireHotkey,
} from '../lib/carbon-keys.js';
import { dictationSupported, isMac } from '../lib/platform.js';
import { appStore } from '../store/store.js';
import { subscribeKeymap } from './bindings.js';

export const DICTATE_COMMAND = 'dictate-hud';

/** Tells the daemon this window is showing session id. */
export function reportFocus(id: string | null): void {
  // Only the dictation box reads focus; elsewhere a report would just
  // fan a SESSION_EVENT out to every client.
  if (!id || !dictationSupported) return;
  ReportFocusedSession(id).catch(() => {
    // No control connection yet: the next switch or window focus
    // reports again.
  });
}

// The chord in the daemon's last DICTATION, or null until one arrives. An
// older daemon never sends one and would ignore SET_DICTATION, so nothing
// is sent until it does.
let received: WireHotkey | null = null;
// The Handy switch in the daemon's last DICTATION, or null until one
// arrives; Settings hides the switch until then.
let handyAutoStart: boolean | null = null;
const handyListeners = new Set<() => void>();

/** The daemon's Handy auto-start switch, or null before it has said. */
export function getHandyAutoStart(): boolean | null {
  return handyAutoStart;
}

/** Calls cb whenever the daemon's Handy switch changes. */
export function subscribeHandyAutoStart(cb: () => void): () => void {
  handyListeners.add(cb);
  return () => handyListeners.delete(cb);
}
// The value last sent and not yet echoed back, so a burst of keymap
// changes does not resend the same chord while the broadcast is in
// flight.
let pending: WireHotkey | null = null;

function reconcile(): void {
  if (!dictationSupported || received === null) return;
  const want = desiredHotkey(
    chordsOfIn(appStore.getState().keymap, DICTATE_COMMAND, isMac),
    isMac,
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

type WireDictation = {
  hotkey?: Partial<WireHotkey & { keyCode: number }>;
  handy_auto_start?: boolean;
  handyAutoStart?: boolean;
};

function readDictation(
  raw: unknown,
): { hotkey: WireHotkey; handy: boolean } | null {
  const v = (
    typeof raw === 'string' ? JSON.parse(raw) : raw
  ) as WireDictation | null;
  if (!v || typeof v !== 'object' || !v.hotkey) return null;
  const h = v.hotkey;
  return {
    hotkey: {
      key_code: Number(h.key_code ?? h.keyCode ?? 0),
      modifiers: Number(h.modifiers ?? 0),
      enabled: h.enabled === true,
    },
    handy: (v.handy_auto_start ?? v.handyAutoStart) === true,
  };
}

let wired = false;

/** Wires focus reports and the hotkey reconcile. Call once at boot. */
export function initGlobalHotkey(): void {
  if (wired) return;
  wired = true;
  if (!dictationSupported) return;
  window.addEventListener('focus', () =>
    reportFocus(appStore.getState().activeId),
  );
  EventsOn('dictation:settings', (payload: unknown) => {
    let d: ReturnType<typeof readDictation>;
    try {
      d = readDictation(payload);
    } catch {
      return;
    }
    if (!d) return;
    received = d.hotkey;
    if (handyAutoStart !== d.handy) {
      handyAutoStart = d.handy;
      for (const cb of handyListeners) cb();
    }
    reconcile();
  });
  subscribeKeymap(() => reconcile());
}

/** Test seam: forget what the daemon said and what was sent. */
export function _resetGlobalHotkeyForTest(): void {
  received = null;
  pending = null;
  handyAutoStart = null;
  handyListeners.clear();
  wired = false;
}
