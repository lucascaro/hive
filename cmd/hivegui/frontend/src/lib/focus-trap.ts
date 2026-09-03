// ---------- focus trap ----------
//
// `aria-modal` promises that focus stays inside the dialog. Nothing in
// the platform enforces it, so every modal has to implement Tab itself
// — and the cost of not doing so is specific and bad: the next tab stop
// behind a modal is a hidden terminal's textarea, so keystrokes leak
// into a session the user cannot see.
//
// This was written three separate times (the help overlay, the settings
// form, the choice dialog) with three slightly different rules, and the
// worktree browser was simply missing it. One implementation, one set
// of tests, four call sites.

// Elements Tab can land on. Deliberately a query rather than a
// registry: modal contents are built dynamically, so anything that
// tries to track focusables as they are created goes stale.
const FOCUSABLE =
  'a[href], button, input, select, textarea, [tabindex]:not([tabindex="-1"])';

// focusableWithin returns the visible, enabled focusables of
// container, in document order.
//
// Visibility is judged by this app's own convention — the `.hidden`
// class and the `hidden` attribute — rather than by offsetParent or
// getClientRects. Those need a layout engine, which jsdom does not
// have, so a layout-based rule would report zero focusables in the DOM
// tests and quietly make every trap test vacuous.
export function focusableWithin(container: HTMLElement): HTMLElement[] {
  return [...container.querySelectorAll<HTMLElement>(FOCUSABLE)].filter(
    (el) =>
      !(el as HTMLElement & { disabled?: boolean }).disabled &&
      !el.hidden &&
      !el.closest('.hidden'),
  );
}

// trapFocus keeps Tab inside container. Call it from the modal's own
// keydown handler; it acts only on Tab and returns whether it consumed
// the event.
//
// Movement *within* the container is left to the browser — a form's
// fields should walk naturally. Only the two boundaries wrap, plus the
// case where focus has escaped the container entirely (a click
// elsewhere, or a modal opened while the terminal held focus).
// `container` is nullable because every caller reaches it through
// pageEl(), which casts rather than throws (app/el.ts): a jsdom test that
// mounts only part of index.html hands this a null, and the ladder in
// keyboard.ts would then throw on a key the modal was not even open for.
export function trapFocus(
  container: HTMLElement | null,
  e: KeyboardEvent,
): boolean {
  if (e.key !== 'Tab') return false;
  if (!container) return false;
  const focusable = focusableWithin(container);
  if (focusable.length === 0) {
    // Nothing to focus, but Tab must still not walk out of the modal.
    e.preventDefault();
    return true;
  }
  const first = focusable[0];
  const last = focusable[focusable.length - 1];
  const active = document.activeElement as HTMLElement | null;
  const inside = !!active && container.contains(active);

  if (!inside) {
    e.preventDefault();
    (e.shiftKey ? last : first).focus();
    return true;
  }
  if (e.shiftKey && active === first) {
    e.preventDefault();
    last.focus();
    return true;
  }
  if (!e.shiftKey && active === last) {
    e.preventDefault();
    first.focus();
    return true;
  }
  // Interior move: let the browser do it.
  return false;
}

// releaseFocus drops focus if it currently sits inside container.
//
// Hiding a modal with `display: none` usually blurs its focused child,
// but not reliably, and not before the close handler's own refocus
// runs — so focus can end up on <body> (stranded: the next Tab starts
// from the top of the page) or, worse, still inside the now-invisible
// modal, where keystrokes go somewhere the user cannot see.
//
// Call it BEFORE hiding, then send focus wherever it belongs.
export function releaseFocus(container: HTMLElement | null): void {
  if (!container) return;
  const focused = document.activeElement;
  if (focused instanceof HTMLElement && container.contains(focused)) {
    focused.blur();
  }
}
