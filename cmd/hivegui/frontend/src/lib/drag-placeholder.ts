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
//
// The spacer is a REAL drop target, not decoration. An "insert above"
// placeholder is inserted where the cursor already is, which pushes the target
// row down and leaves the cursor hovering the spacer itself. If the spacer
// were `pointer-events: none`, the hit-test would fall through to the
// container — which has no dragover handler, so nothing calls preventDefault,
// the browser disallows the drop, and releasing there silently does nothing.
// So the spacer keeps the drag alive itself and resolves its own drop from
// the neighbours it sits between.

const CLASS = 'hv-drop-placeholder';
const ROW = '.hv-session-row, .hv-project-card';

// The drop event is handed back so the caller can read its own payload key
// out of the DataTransfer — this module stays agnostic about what is being
// dragged.
type DropHandler = (target: HTMLElement, above: boolean, e: DragEvent) => void;

let dragged: HTMLElement | null = null;
// A selector for the dragged element, so a renderSidebar() rebuild landing
// mid-drag (it clears projectsUL.innerHTML) can be recovered from: `dragged`
// would otherwise point at a detached node for the rest of the gesture.
let draggedSelector = '';
let spacer: HTMLElement | null = null;
let onDrop: DropHandler | null = null;
// Captured at dragstart, BEFORE the element leaves the flow.
let box = { height: 0, marginTop: '0px', marginBottom: '0px' };

function selectorFor(el: HTMLElement): string {
  const sid = el.dataset.sid;
  if (sid) return `.hv-session-row[data-sid="${CSS.escape(sid)}"]`;
  const pid = el.dataset.pid;
  if (pid) return `.hv-project-card[data-pid="${CSS.escape(pid)}"]`;
  return '';
}

// resolve re-reads the dragged element from the DOM when the node we are
// holding has been replaced by a rebuild.
function resolve(): HTMLElement | null {
  if (dragged?.isConnected) return dragged;
  if (!draggedSelector) return dragged;
  const fresh = document.querySelector<HTMLElement>(draggedSelector);
  if (fresh) dragged = fresh;
  return dragged;
}

// The slot the spacer currently represents, expressed the way the row drop
// handlers express it: the row it would push down, or — when it sits at the
// end of a list — the row it follows.
function slot(): { target: HTMLElement; above: boolean } | null {
  if (!spacer) return null;
  let next = spacer.nextElementSibling;
  while (next && !next.matches(ROW)) next = next.nextElementSibling;
  if (next instanceof HTMLElement) return { target: next, above: true };
  let prev = spacer.previousElementSibling;
  while (prev && !prev.matches(ROW)) prev = prev.previousElementSibling;
  if (prev instanceof HTMLElement) return { target: prev, above: false };
  return null;
}

function makeSpacer(): HTMLElement {
  const el = document.createElement('li');
  el.className = CLASS;
  el.setAttribute('aria-hidden', 'true');
  el.addEventListener('dragover', (e) => {
    // Keeps the drag alive over the gap. Without the preventDefault the
    // browser treats the spacer as a non-target and refuses the drop.
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
  });
  el.addEventListener('drop', (e) => {
    e.preventDefault();
    e.stopPropagation();
    const at = slot();
    const handler = onDrop;
    endDrag();
    if (at && handler) handler(at.target, at.above, e as DragEvent);
  });
  return el;
}

function place(before: Node | null, parent: Node | null | undefined) {
  if (!parent) return;
  if (!spacer) spacer = makeSpacer();
  // Height plus the dragged element's own vertical margins: project cards
  // carry margins, and a zero-margin spacer would not occupy the same space
  // once adjacent-sibling margins collapse.
  spacer.style.height = `${box.height}px`;
  spacer.style.marginTop = box.marginTop;
  spacer.style.marginBottom = box.marginBottom;
  if (before === spacer) return;
  parent.insertBefore(spacer, before);
}

// beginDrag must not take the element out of the flow synchronously: the
// browser snapshots the drag image after the dragstart handler returns, and a
// source element already at `display: none` cancels the drag. The swap —
// spacer in, element out — happens together one tick later, so the list never
// spends a frame a row shorter than it started.
export function beginDrag(el: HTMLElement, handler: DropHandler) {
  endDrag();
  dragged = el;
  draggedSelector = selectorFor(el);
  onDrop = handler;
  const cs = getComputedStyle(el);
  box = {
    height: el.getBoundingClientRect().height,
    marginTop: cs.marginTop,
    marginBottom: cs.marginBottom,
  };
  setTimeout(() => {
    if (dragged !== el) return;
    place(el, el.parentNode);
    el.classList.add('dragging');
  }, 0);
}

// moveTo places the spacer immediately above or below `target`. Safe to call
// on every dragover: it moves the one spacer rather than adding another, and
// re-asserts the hidden state on whichever node currently represents the
// dragged element.
export function moveTo(target: Element, above: boolean) {
  if (!dragged) return;
  resolve()?.classList.add('dragging');
  place(above ? target : target.nextSibling, target.parentNode);
}

// endDrag is idempotent — drop and dragend both fire, and a drag can be
// cancelled without either landing on a target.
export function endDrag() {
  spacer?.remove();
  spacer = null;
  onDrop = null;
  resolve()?.classList.remove('dragging');
  dragged = null;
  draggedSelector = '';
}
