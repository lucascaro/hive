// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Platform detection + cross-platform modifier-key helper.
//
// Extracted from main.js so it can be unit-tested with a fake
// navigator. detectMac(nav) is a pure function; isMac is the
// production value computed once at import time.

// `userAgentData` is not in TS's DOM lib yet; declaring it optional
// here keeps the real `navigator` assignable and lets tests pass a
// bare object literal with only the field they care about.
export function detectMac(
  nav:
    | { userAgentData?: { platform?: string }; platform?: string }
    | null
    | undefined,
): boolean {
  const p = (nav && (nav.userAgentData?.platform || nav.platform)) || '';
  return /mac|iphone|ipad/i.test(p);
}

export const isMac = detectMac(
  typeof navigator !== 'undefined' ? navigator : null,
);

/** Windows, from the same navigator fields as detectMac. */
export function detectWindows(
  nav:
    | { userAgentData?: { platform?: string }; platform?: string }
    | null
    | undefined,
): boolean {
  const p = (nav && (nav.userAgentData?.platform || nav.platform)) || '';
  return /^win/i.test(p);
}

export const isWindows = detectWindows(
  typeof navigator !== 'undefined' ? navigator : null,
);

/** Whether this platform has the dictation box: the menu bar's on macOS
 * (spec 553), each Hive window's on Windows (spec 575). */
export const dictationSupported = isMac || isWindows;

// cmdOrCtrl returns true when the user pressed the platform's
// "primary" modifier: Cmd on macOS, Ctrl elsewhere. We deliberately
// reject the cross-platform combo (Ctrl+Cmd on macOS, Cmd+Ctrl on
// PC) so chorded shortcuts don't fire on weird key states.
export function cmdOrCtrl(
  e: Pick<KeyboardEvent, 'metaKey' | 'ctrlKey'>,
  mac = isMac,
): boolean {
  return mac ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey;
}
