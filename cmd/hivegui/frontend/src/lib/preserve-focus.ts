// Re-rendering steals the keyboard unless something puts focus back.
//
// Two render paths in this app move or destroy live DOM nodes:
// app/sidebar.ts's renderSidebar does `projectsUL.innerHTML = ''` and
// rebuilds every row, and app/view.ts's renderGrid re-parents tiles with
// appendChild — which, on an already-attached node, is a remove+insert
// the browser treats as a blur. Either way focus lands on <body> and the
// next keystroke goes nowhere.
//
// That matters beyond the terminal: the daemon emits `session:event`
// `updated` on every phase step, on every surviving session after a kill,
// and whenever the agent-session-id capture poll lands — up to 30s after a
// spawn (internal/registry/create.go). Any of those arriving while a
// sidebar button holds focus drops it.
//
// preserveFocus captures what was focused inside `root`, runs the render,
// and restores focus to the same node when it survived (the reorder case)
// or to its rebuilt equivalent when it did not (the rebuild case), located
// by the owning row's data-sid plus a within-row selector.
//
// It only ever reclaims focus that was DROPPED — if the render moved focus
// deliberately (switchTo focusing a terminal, a modal opening), the new
// owner keeps it. Same rule as app/focus.ts's blur guard.

const FOCUS_OPTS: FocusOptions = { preventScroll: true };

// How to find the same control again inside a rebuilt row. data-action is
// the stable handle the action buttons already carry; everything else is
// identified by its component class. Matching is done by predicate rather
// than by building a selector string, so an id or class carrying a quote
// or a colon can't produce a malformed selector (and CSS.escape, which
// jsdom does not implement, isn't needed).
type Match = (el: HTMLElement) => boolean;

function matcherFor(el: HTMLElement): Match | null {
  const action = el.dataset.action;
  if (action) return (c) => c.dataset.action === action;
  const cls = Array.from(el.classList).find((c) =>
    c.startsWith('hv-session-row__'),
  );
  if (cls) return (c) => c.classList.contains(cls);
  if (el instanceof HTMLInputElement && el.type === 'color')
    return (c) => c instanceof HTMLInputElement && c.type === 'color';
  return null;
}

export function preserveFocus(root: HTMLElement, render: () => void): void {
  const active = document.activeElement;
  const keep =
    active instanceof HTMLElement && active !== root && root.contains(active)
      ? active
      : null;
  const owner = keep?.closest<HTMLElement>('[data-sid]') ?? null;
  const sid = owner?.dataset.sid ?? null;
  const within = keep && keep !== owner ? matcherFor(keep) : null;

  render();

  if (!keep) return;
  // Something claimed the keyboard on purpose during the render — a modal,
  // a terminal, an inline rename. Never yank it back.
  const now = document.activeElement;
  if (now && now !== document.body && now !== root) return;

  // Survived: the node was moved, not replaced.
  if (root.contains(keep)) {
    keep.focus(FOCUS_OPTS);
    return;
  }
  if (!sid) return;
  const row = Array.from(root.querySelectorAll<HTMLElement>('[data-sid]')).find(
    (el) => el.dataset.sid === sid,
  );
  if (!row) return; // the session is gone; nothing to restore to
  if (!within) {
    row.focus(FOCUS_OPTS);
    return;
  }
  Array.from(row.querySelectorAll<HTMLElement>('*'))
    .find(within)
    ?.focus(FOCUS_OPTS);
}
