// Modal registry — the seam that lets the focus pipeline ask "does a
// modal own the keyboard?" without main.ts hard-coding every modal
// element. Each modal module registers its root element at init;
// anyModalOpen() is consumed by focusSnapshot (and is the ONLY
// intentional behavior-adjacent edit of the modularization — it
// replaces an explicit four-element classList check with the same
// check over the registered set).

const modals: Element[] = [];

export function registerModal(el: Element | null | undefined): void {
  if (el) modals.push(el);
}

// unregisterModal removes a modal that no longer exists. The static
// modals in index.html never need it — they are hidden, not removed —
// but a dialog built per question does: a detached element has no
// `hidden` class, so leaving it registered would make anyModalOpen()
// answer true forever and permanently strand the keyboard.
export function unregisterModal(el: Element | null | undefined): void {
  if (!el) return;
  const i = modals.indexOf(el);
  if (i >= 0) modals.splice(i, 1);
}

export function anyModalOpen(): boolean {
  return modals.some((el) => !el.classList.contains('hidden'));
}
