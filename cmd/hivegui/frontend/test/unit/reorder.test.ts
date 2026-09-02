import { describe, it, expect } from 'vitest';
import { dropTargetIndex, reorderTarget } from '../../src/lib/reorder.js';

// `order` is the session's index in the daemon's r.order — the value
// reorderTarget must return. The fixtures below deliberately separate
// that from array position, because the two coincide only when the
// daemon's flat list happens to be grouped by project.
//
// Grouped case: display position == .order.
const ordered = [
  { id: 'a0', projectId: 'A', order: 0 },
  { id: 'a1', projectId: 'A', order: 1 },
  { id: 'b0', projectId: 'B', order: 2 },
  { id: 'b1', projectId: 'B', order: 3 },
  { id: 'b2', projectId: 'B', order: 4 },
];

// moveInOrder is a faithful copy of internal/registry's delete-then-
// clamp-then-insert. The interleaved test below runs the returned index
// through it, so the assertion is about the resulting session order and
// not about a number whose meaning could quietly change.
function moveInOrder(order: string[], id: string, newOrder: number): string[] {
  const cur = order.indexOf(id);
  if (cur < 0) return order;
  const out = order.slice();
  out.splice(cur, 1);
  const at = Math.min(Math.max(newOrder, 0), out.length);
  out.splice(at, 0, id);
  return out;
}

describe('reorderTarget', () => {
  it('moves down within the project', () => {
    // b1 swaps with b2, whose index in r.order is 4.
    expect(reorderTarget(ordered, 'b1', +1)).toBe(4);
  });

  it('moves up within the project', () => {
    expect(reorderTarget(ordered, 'b1', -1)).toBe(2);
  });

  it('wraps from the last session in a project to the first', () => {
    expect(reorderTarget(ordered, 'b2', +1)).toBe(2);
  });

  it('wraps from the first session in a project to the last', () => {
    expect(reorderTarget(ordered, 'b0', -1)).toBe(4);
  });

  it('returns null for a project with a single session', () => {
    const solo = [{ id: 'a0', projectId: 'A', order: 0 }, ...ordered.slice(2)];
    expect(reorderTarget(solo, 'a0', +1)).toBeNull();
    expect(reorderTarget(solo, 'a0', -1)).toBeNull();
  });

  it('returns null for an unknown or null active session', () => {
    expect(reorderTarget(ordered, 'nope', +1)).toBeNull();
    expect(reorderTarget(ordered, null, +1)).toBeNull();
    expect(reorderTarget(ordered, 'b1', 0)).toBeNull();
  });

  it('reads both projectId and project_id spellings', () => {
    const snake = [
      { id: 'b0', project_id: 'B', order: 0 },
      { id: 'a0', projectId: 'A', order: 1 },
      { id: 'b1', project_id: 'B', order: 2 },
    ];
    expect(reorderTarget(snake, 'b0', +1)).toBe(2);
  });

  // The regression this file exists for. The daemon's r.order is one
  // flat creation-order list; alternating creates across two projects
  // interleave them, so every session's DISPLAY position (project-major)
  // differs from its .order. Returning a display position here produced
  // a silent no-op — the move was accepted and changed nothing.
  describe('when projects interleave in the daemon order', () => {
    // r.order: s1(A) t1(B) s2(A) t2(B) s3(A)  →  .order 0..4
    // display: s1 s2 s3 t1 t2                 →  positions 0..4
    const raw = ['s1', 't1', 's2', 't2', 's3'];
    const proj: Record<string, string> = {
      s1: 'A',
      t1: 'B',
      s2: 'A',
      t2: 'B',
      s3: 'A',
    };
    // Project-major, then by .order — what orderedSessions() produces.
    const display = raw
      .map((id, i) => ({ id, projectId: proj[id], order: i }))
      .sort((a, b) =>
        a.projectId === b.projectId
          ? a.order - b.order
          : a.projectId.localeCompare(b.projectId),
      );
    const inProject = (order: string[], pid: string) =>
      order.filter((id) => proj[id] === pid);

    it('targets the sibling by its daemon index, not its display position', () => {
      // s3 sits at display position 2 but at r.order index 4.
      expect(reorderTarget(display, 's2', +1)).toBe(4);
    });

    it('actually swaps the pair when the daemon applies it', () => {
      const target = reorderTarget(display, 's2', +1);
      expect(target).not.toBeNull();
      const after = moveInOrder(raw, 's2', target as number);
      expect(inProject(after, 'A')).toEqual(['s1', 's3', 's2']);
      // The other project is left exactly as it was.
      expect(inProject(after, 'B')).toEqual(['t1', 't2']);
    });

    it('swaps back on the reverse move', () => {
      const down = reorderTarget(display, 's2', +1) as number;
      const moved = moveInOrder(raw, 's2', down);
      // Re-derive the display list from the new r.order.
      const display2 = moved
        .map((id, i) => ({ id, projectId: proj[id], order: i }))
        .sort((a, b) =>
          a.projectId === b.projectId
            ? a.order - b.order
            : a.projectId.localeCompare(b.projectId),
        );
      const up = reorderTarget(display2, 's2', -1) as number;
      const back = moveInOrder(moved, 's2', up);
      expect(inProject(back, 'A')).toEqual(['s1', 's2', 's3']);
      expect(inProject(back, 'B')).toEqual(['t1', 't2']);
    });

    it('wraps within the project without touching the other one', () => {
      // s3 is last in A; moving it down wraps to A's first slot.
      const target = reorderTarget(display, 's3', +1) as number;
      const after = moveInOrder(raw, 's3', target);
      expect(inProject(after, 'A')).toEqual(['s3', 's1', 's2']);
      expect(inProject(after, 'B')).toEqual(['t1', 't2']);
    });
  });
});

// dropTargetIndex — the drag-and-drop half of the same conversion.
//
// Every case runs the returned index through moveInOrder and asserts the
// RESULTING order. Asserting the bare index would just re-encode whatever the
// implementation does; the bug this suite exists for (spec 305) produced a
// perfectly plausible-looking index that landed the row one slot too low.
describe('dropTargetIndex', () => {
  // One project, so r.order index == display position.
  const flat = ['a', 'b', 'c', 'd'];
  const sessions = flat.map((id, i) => ({ id, projectId: 'P', order: i }));
  const drop = (dragged: string, target: string, above: boolean) => {
    const idx = dropTargetIndex(sessions, dragged, target, above);
    return idx === null ? flat : moveInOrder(flat, dragged, idx);
  };

  it('drops above a target further down the list', () => {
    // The regression: this used to land [b, c, a, d] — one slot too low.
    expect(drop('a', 'c', true)).toEqual(['b', 'a', 'c', 'd']);
  });

  it('drops below a target further down the list', () => {
    expect(drop('a', 'c', false)).toEqual(['b', 'c', 'a', 'd']);
  });

  it('drops above a target further up the list', () => {
    expect(drop('d', 'b', true)).toEqual(['a', 'd', 'b', 'c']);
  });

  it('drops below a target further up the list', () => {
    expect(drop('d', 'b', false)).toEqual(['a', 'b', 'd', 'c']);
  });

  it('drops at the head of the list', () => {
    expect(drop('c', 'a', true)).toEqual(['c', 'a', 'b', 'd']);
  });

  it('drops past the last row', () => {
    expect(drop('a', 'd', false)).toEqual(['b', 'c', 'd', 'a']);
  });

  it('is a no-op when the slot is the one the session already holds', () => {
    expect(dropTargetIndex(sessions, 'a', 'a', true)).toBeNull();
    expect(dropTargetIndex(sessions, 'a', 'b', true)).toBeNull();
    expect(dropTargetIndex(sessions, 'b', 'a', false)).toBeNull();
  });

  it('returns null for an unknown dragged or target session', () => {
    expect(dropTargetIndex(sessions, 'nope', 'b', true)).toBeNull();
    expect(dropTargetIndex(sessions, 'a', 'nope', true)).toBeNull();
  });

  it('reads both projectId and project_id spellings', () => {
    const snake = [
      { id: 'a', project_id: 'P', order: 0 },
      { id: 'b', project_id: 'P', order: 1 },
      { id: 'c', project_id: 'P', order: 2 },
    ];
    const idx = dropTargetIndex(snake, 'c', 'b', true) as number;
    expect(moveInOrder(['a', 'b', 'c'], 'c', idx)).toEqual(['a', 'c', 'b']);
  });

  // Same trap as reorderTarget's: r.order is one flat list across projects,
  // so a project-relative slot must be translated through a sibling's global
  // index, and the other project must come out untouched.
  describe('when projects interleave in the daemon order', () => {
    const raw = ['s1', 't1', 's2', 't2', 's3'];
    const proj: Record<string, string> = {
      s1: 'A',
      t1: 'B',
      s2: 'A',
      t2: 'B',
      s3: 'A',
    };
    const all = raw.map((id, i) => ({ id, projectId: proj[id], order: i }));
    const inProject = (order: string[], pid: string) =>
      order.filter((id) => proj[id] === pid);

    it('drops above a sibling that is not adjacent in r.order', () => {
      const idx = dropTargetIndex(all, 's3', 's2', true) as number;
      const after = moveInOrder(raw, 's3', idx);
      expect(inProject(after, 'A')).toEqual(['s1', 's3', 's2']);
      expect(inProject(after, 'B')).toEqual(['t1', 't2']);
    });

    it('drops below a sibling further down the project', () => {
      const idx = dropTargetIndex(all, 's1', 's2', false) as number;
      const after = moveInOrder(raw, 's1', idx);
      expect(inProject(after, 'A')).toEqual(['s2', 's1', 's3']);
      expect(inProject(after, 'B')).toEqual(['t1', 't2']);
    });

    it('drops past the last sibling without swallowing the other project', () => {
      const idx = dropTargetIndex(all, 's1', 's3', false) as number;
      const after = moveInOrder(raw, 's1', idx);
      expect(inProject(after, 'A')).toEqual(['s2', 's3', 's1']);
      expect(inProject(after, 'B')).toEqual(['t1', 't2']);
    });
  });
});
