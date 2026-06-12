// Modal registry — the seam that lets the focus pipeline ask "does a
// modal own the keyboard?" without main.js hard-coding every modal
// element. Each modal module registers its root element at init;
// anyModalOpen() is consumed by focusSnapshot (and is the ONLY
// intentional behavior-adjacent edit of the modularization — it
// replaces an explicit four-element classList check with the same
// check over the registered set).

const modals = [];

export function registerModal(el) {
  if (el) modals.push(el);
}

export function anyModalOpen() {
  return modals.some((el) => !el.classList.contains('hidden'));
}
