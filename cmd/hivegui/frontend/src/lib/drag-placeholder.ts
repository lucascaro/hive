// Drop placeholder for the sidebar's drag-to-reorder (sessions and project
// cards both). The dragged element leaves the layout flow and a spacer of its
// exact margin box takes its place at the drop slot, so the list's total
// height never changes mid-drag and nothing below the cursor shifts until the
// drop actually commits.
//
// One module-level drag at a time: HTML5 drag-and-drop only ever has one.
//
// The spacer deliberately carries NEITHER `hv-session-row` nor
// `hv-project-card`. app/sidebar.ts's domShape() reads the sidebar back with
// that selector pair to decide whether an in-place update is safe; a spacer
// wearing either class would read as a phantom row.

const CLASS = 'hv-drop-placeholder';

let dragged: HTMLElement | null = null;
let spacer: HTMLElement | null = null;
// Captured at dragstart, BEFORE the element leaves the flow.
let box = { height: 0, marginTop: '0px', marginBottom: '0px' };

// hide is what takes the element out of the flow. It must not run inside the
// dragstart handler: the browser snapshots the drag image after that handler
// returns, and a source element already at `display: none` cancels the drag.
function hide(el: HTMLElement) {
  el.classList.add('dragging');
}

export function beginDrag(el: HTMLElement) {
  endDrag();
  dragged = el;
  const cs = getComputedStyle(el);
  box = {
    height: el.getBoundingClientRect().height,
    marginTop: cs.marginTop,
    marginBottom: cs.marginBottom,
  };
  setTimeout(() => {
    if (dragged === el) hide(el);
  }, 0);
}

// moveTo places the spacer immediately above or below `target`. Safe to call
// on every dragover: it moves the one spacer rather than adding another, and
// re-asserts the hidden state so a renderSidebar() rebuild landing mid-drag
// (it clears projectsUL.innerHTML) only costs a frame of drag chrome.
export function moveTo(target: Element, above: boolean) {
  if (!dragged) return;
  hide(dragged);
  if (!spacer) {
    spacer = document.createElement('li');
    spacer.className = CLASS;
    spacer.setAttribute('aria-hidden', 'true');
  }
  // Height plus the dragged element's own vertical margins: project cards
  // carry margins, and a zero-margin spacer would not occupy the same space
  // once adjacent-sibling margins collapse.
  spacer.style.height = `${box.height}px`;
  spacer.style.marginTop = box.marginTop;
  spacer.style.marginBottom = box.marginBottom;
  const before = above ? target : target.nextSibling;
  if (before === spacer) return;
  target.parentNode?.insertBefore(spacer, before);
}

// endDrag is idempotent — drop and dragend both fire, and a drag can be
// cancelled without either landing on a target.
export function endDrag() {
  spacer?.remove();
  spacer = null;
  dragged?.classList.remove('dragging');
  dragged = null;
}
