// Re-rendering steals the keyboard unless something puts focus back.
//
// app/grid-layout.ts's applyGridLayout re-parents tiles with appendChild — which, on
// an already-attached node, is a remove+insert the browser treats as a
// blur. Focus lands on <body> and the next keystroke goes nowhere.
//
// That is the only path left. The sidebar used to be the other, with an
// `innerHTML = ''` rebuild that replaced every row; the React sidebar
// reconciles in place and handles its own move-blur inline (a keyed row
// is moved, never rebuilt, so there is nothing to relocate focus TO).
// The half of this helper that found a REBUILT equivalent — by the
// owning row's data-sid plus a within-row matcher — went with that
// rebuild rather than staying as unreachable, untestable code.
//
// preserveFocus captures what was focused inside `root`, runs the render,
// and puts focus back on the same node, which a reparent always leaves
// connected.
//
// It only ever reclaims focus that was DROPPED — if the render moved focus
// deliberately (switchTo focusing a terminal, a modal opening), the new
// owner keeps it. Same rule as app/focus.ts's blur guard.

const FOCUS_OPTS: FocusOptions = { preventScroll: true };

export function preserveFocus(root: HTMLElement, render: () => void): void {
  const active = document.activeElement;
  const keep =
    active instanceof HTMLElement && active !== root && root.contains(active)
      ? active
      : null;

  render();

  if (!keep) return;
  // Something claimed the keyboard on purpose during the render — a modal,
  // a terminal, an inline rename. Never yank it back.
  const now = document.activeElement;
  if (now && now !== document.body && now !== root) return;
  // Moved, not replaced: the node is still here.
  if (root.contains(keep)) keep.focus(FOCUS_OPTS);
}
