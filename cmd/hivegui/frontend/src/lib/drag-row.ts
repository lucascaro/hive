// Drag-to-reorder wiring for one row of a list, shared by every list that
// reorders by drag: sidebar session rows, sidebar project cards and the
// pinned agents in Settings. The placeholder mechanics live in
// drag-placeholder.ts; this is only the four React handlers each row used
// to spell out by hand.
//
// A row opts in by spreading the returned props onto an <li> that also
// carries `data-drag-row` — the attribute drag-placeholder.ts resolves
// drop slots against. The list must be a <ul>/<ol>: the spacer is an <li>.

import type { DragEvent as ReactDragEvent } from 'react';
import { beginDrag, endDrag, moveTo } from './drag-placeholder.js';

export interface DragRowOpts {
  // DataTransfer type carrying the dragged row's id. It doubles as the
  // accept filter: a row only takes drops of its own kind.
  mime: string;
  id: string;
  // Called with the dragged id and the row the drop resolved to — either
  // this row or, when the release lands on the spacer, its neighbour.
  onCommit: (draggedId: string, target: HTMLElement, above: boolean) => void;
  // Drags starting inside a match are cancelled (preventDefault), for
  // chrome inside the row — buttons, rename inputs — where a pointer drag
  // means text selection, not a reorder.
  cancelFrom?: string;
  // Upper/lower half test. Defaults to the row's own midpoint; project
  // cards measure against their header instead.
  aboveOf?: (clientY: number, row: HTMLElement) => boolean;
}

function midpointAbove(clientY: number, row: HTMLElement): boolean {
  const r = row.getBoundingClientRect();
  return clientY - r.top < r.height / 2;
}

// bubbledFromNested is true when the event started on a different drag row
// nested inside this one (a session row inside its project card). Such an
// event belongs to the inner row: the outer one must neither start its own
// drag nor preventDefault, which would cancel the inner drag. A Text-node
// target (text-selection drag) has no closest(), and is this row's own.
function bubbledFromNested(e: ReactDragEvent<HTMLElement>): boolean {
  const t = e.target;
  return (
    t instanceof Element && t.closest('[data-drag-row]') !== e.currentTarget
  );
}

export function dragRowProps(o: DragRowOpts) {
  const aboveOf = o.aboveOf ?? midpointAbove;
  const commit = (target: HTMLElement, above: boolean, e: DragEvent) => {
    const dragged = e.dataTransfer?.getData(o.mime);
    if (dragged) o.onCommit(dragged, target, above);
  };
  return {
    draggable: true,
    onDragStart: (e: ReactDragEvent<HTMLLIElement>) => {
      if (bubbledFromNested(e)) return;
      const t = e.target;
      if (o.cancelFrom && t instanceof Element && t.closest(o.cancelFrom)) {
        e.preventDefault();
        return;
      }
      const dt = e.dataTransfer;
      if (!dt) return;
      dt.effectAllowed = 'move';
      dt.setData(o.mime, o.id);
      beginDrag(e.currentTarget, commit);
    },
    onDragEnd: (e: ReactDragEvent<HTMLLIElement>) => {
      // The nested row owns its own teardown.
      if (bubbledFromNested(e)) return;
      endDrag();
    },
    onDragOver: (e: ReactDragEvent<HTMLLIElement>) => {
      const dt = e.dataTransfer;
      if (!dt?.types.includes(o.mime)) return;
      e.preventDefault();
      dt.dropEffect = 'move';
      moveTo(e.currentTarget, aboveOf(e.clientY, e.currentTarget));
    },
    // Drops of another kind fall through to whichever ancestor row accepts
    // them — a project drop released over a session row reaches its card.
    onDrop: (e: ReactDragEvent<HTMLLIElement>) => {
      const dt = e.dataTransfer;
      if (!dt?.types.includes(o.mime)) return;
      e.preventDefault();
      const row = e.currentTarget;
      const above = aboveOf(e.clientY, row);
      endDrag();
      commit(row, above, e.nativeEvent);
    },
  };
}

export type DragRowProps = ReturnType<typeof dragRowProps>;
