// Status-bar controller with a persistent slot and a transient flash
// layer. Pure (all effects injected) so the semantics are unit-testable
// without a DOM.
//
// The status bar serves two kinds of messages that used to clobber each
// other through a bare textContent write:
//   - persistent state ("connected", "control disconnected", the name
//     of the session you just switched to) — owned by set();
//   - per-action feedback ("copy failed: …", "creating session…") —
//     owned by flash(), which auto-reverts to the persistent slot.
//
// Guarantees:
//   - a flash is visible for at least FLASH_MIN_MS before a set() may
//     replace it (so an error isn't wiped by nav feedback a frame later);
//   - a flash auto-expires (errors linger longer than info);
//   - set() during an active flash is never lost — it lands in the
//     persistent slot and renders when the flash ends.

export const FLASH_ERROR_MS = 6000;
export const FLASH_INFO_MS = 2500;
export const FLASH_MIN_MS = 1500;

export function createStatus({ render, setTimer, clearTimer, now }) {
  let persistent = { text: '', isError: false };
  let flashActive = false;
  let flashTimer = 0;
  let flashStarted = 0;

  function endFlash() {
    flashActive = false;
    flashTimer = 0;
    render(persistent.text, persistent.isError);
  }

  function set(text, isError = false) {
    persistent = { text, isError };
    if (!flashActive) {
      render(text, isError);
      return;
    }
    if (now() - flashStarted >= FLASH_MIN_MS) {
      clearTimer(flashTimer);
      endFlash();
    }
    // Else: the flash keeps the screen; the stored persistent text
    // renders when it expires.
  }

  function flash(text, isError = false) {
    if (flashTimer) clearTimer(flashTimer);
    flashActive = true;
    flashStarted = now();
    render(text, isError);
    flashTimer = setTimer(endFlash, isError ? FLASH_ERROR_MS : FLASH_INFO_MS);
  }

  return { set, flash };
}
