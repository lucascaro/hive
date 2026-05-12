// Platform detection + cross-platform modifier-key helper.
//
// Extracted from main.js so it can be unit-tested with a fake
// navigator. detectMac(nav) is a pure function; isMac is the
// production value computed once at import time.

export function detectMac(nav) {
  const p = (nav && (nav.userAgentData?.platform || nav.platform)) || '';
  return /mac|iphone|ipad/i.test(p);
}

export const isMac = detectMac(typeof navigator !== 'undefined' ? navigator : null);

// cmdOrCtrl returns true when the user pressed the platform's
// "primary" modifier: Cmd on macOS, Ctrl elsewhere. We deliberately
// reject the cross-platform combo (Ctrl+Cmd on macOS, Cmd+Ctrl on
// PC) so chorded shortcuts don't fire on weird key states.
export function cmdOrCtrl(e, mac = isMac) {
  return mac ? (e.metaKey && !e.ctrlKey) : (e.ctrlKey && !e.metaKey);
}
