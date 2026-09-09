import { describe, expect, it } from 'vitest';
import {
  clusterDropOps,
  clusterReorderOps,
  clusterSessions,
  worktreeGroups,
  worktreeKey,
} from '../../src/lib/worktree-groups.js';

// Same shape as reorder.test.ts: `order` is the session's index in the
// daemon's flat r.order, deliberately not the array position.
const S = (
  id: string,
  projectId: string,
  order: number,
  worktree_path = '',
) => ({ id, projectId, order, worktree_path });

// moveInOrder is a faithful copy of internal/registry's delete-then-clamp-
// then-insert. Every clusterDropOps assertion below is played back through
// it, so the test is about the resulting session order and not about numbers
// whose meaning could quietly change.
function moveInOrder(order: string[], id: string, newOrder: number): string[] {
  const cur = order.indexOf(id);
  if (cur < 0) return order;
  const out = order.slice();
  out.splice(cur, 1);
  const at = Math.min(Math.max(newOrder, 0), out.length);
  out.splice(at, 0, id);
  return out;
}

function replay(
  sessions: { id: string; order: number }[],
  ops: { id: string; order: number }[],
): string[] {
  let ids = sessions
    .slice()
    .sort((a, b) => a.order - b.order)
    .map((s) => s.id);
  for (const op of ops) ids = moveInOrder(ids, op.id, op.order);
  return ids;
}

describe('worktreeKey', () => {
  it('reads snake_case and camelCase, and empty for no worktree', () => {
    expect(worktreeKey({ worktree_path: '/wt/a' })).toBe('/wt/a');
    expect(worktreeKey({ worktreePath: '/wt/b' })).toBe('/wt/b');
    expect(worktreeKey({})).toBe('');
  });
});

describe('worktreeGroups', () => {
  it('groups two or more occupants of one worktree, in .order', () => {
    const g = worktreeGroups([
      S('b', 'A', 3, '/wt/x'),
      S('a', 'A', 1, '/wt/x'),
      S('c', 'A', 2, '/wt/y'),
    ]);
    expect([...g.keys()]).toEqual(['/wt/x']);
    expect(g.get('/wt/x')).toEqual(['a', 'b']);
  });

  it('ignores solo occupants and sessions with no worktree', () => {
    const g = worktreeGroups([
      S('a', 'A', 0, '/wt/x'),
      S('b', 'A', 1),
      S('c', 'A', 2),
    ]);
    expect(g.size).toBe(0);
  });
});

describe('clusterSessions', () => {
  it('pulls a group together at its lowest-order member', () => {
    const out = clusterSessions([
      S('a', 'A', 0, '/wt/x'),
      S('b', 'A', 1),
      S('c', 'A', 2, '/wt/x'),
      S('d', 'A', 3),
    ]);
    expect(out.map((s) => s.id)).toEqual(['a', 'c', 'b', 'd']);
  });

  it('is idempotent — a re-render never reshuffles rows', () => {
    const once = clusterSessions([
      S('a', 'A', 0, '/wt/x'),
      S('b', 'A', 1),
      S('c', 'A', 2, '/wt/x'),
    ]);
    expect(clusterSessions(once).map((s) => s.id)).toEqual(
      once.map((s) => s.id),
    );
  });

  it('leaves an ungrouped list in plain .order', () => {
    const out = clusterSessions([S('b', 'A', 1), S('a', 'A', 0)]);
    expect(out.map((s) => s.id)).toEqual(['a', 'b']);
  });
});

// Re-cluster a replayed order the way the next paint would, so assertions
// read as "what the user ends up seeing".
const paintedAfter = <T extends { id: string; order: number }>(
  base: T[],
  ops: { id: string; order: number }[],
  replayFn: (b: T[], o: { id: string; order: number }[]) => string[],
) =>
  clusterSessions(
    replayFn(base, ops).map((id, i) => ({
      ...(base.find((s) => s.id === id) as T),
      order: i,
    })),
  ).map((s) => s.id);

describe('clusterDropOps', () => {
  // a and c share a worktree; b and d do not. r.order is a…d = 0…3.
  const sessions = [
    S('a', 'A', 0, '/wt/x'),
    S('b', 'A', 1),
    S('c', 'A', 2, '/wt/x'),
    S('d', 'A', 3),
  ];

  it('moves a single unshared session to the painted slot', () => {
    // Painted order is a,c,b,d. Dropping d above the painted b row puts it
    // between c and b. The ops may not name `d` — opsToReach emits the
    // shortest prefix-fixing sequence, and it also normalises the stored
    // order onto the painted one — so the assertion is on the resulting
    // order, which is the thing the user sees.
    const ops = clusterDropOps(sessions, 'd', 'b', true);
    expect(replay(sessions, ops)).toEqual(['a', 'c', 'd', 'b']);
  });

  it('leaves the stored order equal to the painted order', () => {
    // The invariant that lets clusterSessions be idempotent on what comes
    // back: after any drop, r.order IS the paint order, so the next
    // broadcast cannot re-shuffle the rows.
    const ops = clusterDropOps(sessions, 'd', 'b', true);
    const after = replay(sessions, ops);
    expect(
      clusterSessions(
        after.map((id, i) => ({
          ...(sessions.find((s) => s.id === id) as (typeof sessions)[number]),
          order: i,
        })),
      ).map((s) => s.id),
    ).toEqual(after);
  });

  it('moves the whole group below a target, contiguously', () => {
    const ops = clusterDropOps(sessions, 'a', 'd', false);
    expect(replay(sessions, ops)).toEqual(['b', 'd', 'a', 'c']);
  });

  it('moves the whole group above a target, contiguously', () => {
    // The group already paints above b, so the rows do not move — but the
    // stored order is still normalised onto the painted one, which is the
    // point of having a single order. A drop that changes nothing VISIBLE
    // may still cost one write; a drop that changes nothing at all costs
    // none (see the no-round-trip case below).
    const ops = clusterDropOps(sessions, 'c', 'b', true);
    expect(replay(sessions, ops)).toEqual(['a', 'c', 'b', 'd']);
  });

  it('drags by any member, not just the group head', () => {
    const byHead = clusterDropOps(sessions, 'a', 'd', false);
    const byTail = clusterDropOps(sessions, 'c', 'd', false);
    expect(replay(sessions, byTail)).toEqual(replay(sessions, byHead));
  });

  it('lands at the end of the project', () => {
    const ops = clusterDropOps(sessions, 'a', 'd', false);
    expect(replay(sessions, ops).slice(-2)).toEqual(['a', 'c']);
  });

  it('keeps other projects untouched', () => {
    const cross = [...sessions, S('z', 'B', 4), S('y', 'B', 5)];
    const ops = clusterDropOps(cross, 'a', 'd', false);
    expect(replay(cross, ops).slice(-2)).toEqual(['z', 'y']);
  });

  it('keeps a group contiguous when its stored order starts split', () => {
    // a and d share a worktree with b and c wedged between them in
    // `.order`; they already paint as a,d,b,c.
    const wedged = [
      S('a', 'A', 0, '/wt/x'),
      S('b', 'A', 1),
      S('c', 'A', 2),
      S('d', 'A', 3, '/wt/x'),
    ];
    const ops = clusterDropOps(wedged, 'a', 'c', false);
    expect(replay(wedged, ops)).toEqual(['b', 'c', 'a', 'd']);
  });

  // The bug review found: the drop slot used to be resolved in `.order`
  // space while the user dragged in painted space, so any drop past a group
  // landed one painted row off — or did nothing at all. Both fixtures below
  // paint as a,c,b,d,e while `.order` says a,b,c,d,e, which is exactly where
  // the two spaces disagree.
  const split = [
    S('a', 'A', 0, '/wt/x'),
    S('b', 'A', 1),
    S('c', 'A', 2, '/wt/x'),
    S('d', 'A', 3),
    S('e', 'A', 4),
  ];
  const paintedIds = (
    ss: typeof split | typeof sessions,
    ops: { id: string; order: number }[],
  ) => paintedAfter(ss, ops, replay);

  it('lands where the painted row was, not where .order put it', () => {
    // Dropped below the painted `c` row, e belongs between c and b.
    const ops = clusterDropOps(split, 'e', 'c', false);
    expect(paintedIds(split, ops)).toEqual(['a', 'c', 'e', 'b', 'd']);
  });

  it('is not a no-op when the painted neighbour differs from the .order one', () => {
    // d below c: identical in `.order` (so the old math emitted nothing),
    // a real move in painted space.
    const ops = clusterDropOps(split, 'd', 'c', false);
    expect(ops.length).toBeGreaterThan(0);
    expect(paintedIds(split, ops)).toEqual(['a', 'c', 'd', 'b', 'e']);
  });

  // Dropping on your own group member reorders INSIDE the group — the
  // block is contiguous, so there is nothing else such a drop could mean,
  // and it used to be a dead gesture.
  it('reorders within the group when dropped on a fellow member', () => {
    const ops = clusterDropOps(sessions, 'a', 'c', false);
    expect(paintedIds(sessions, ops)).toEqual(['c', 'a', 'b', 'd']);
  });

  it('keeps the group in place when reordering inside it', () => {
    // b and d must not move: only the two members swap.
    const ops = clusterDropOps(sessions, 'a', 'c', false);
    const after = paintedIds(sessions, ops);
    expect(after.slice(2)).toEqual(['b', 'd']);
  });

  it('is a no-op when dropped on itself', () => {
    expect(clusterDropOps(sessions, 'a', 'a', false)).toEqual([]);
  });

  it('refuses a cross-project drop', () => {
    const cross = [...sessions, S('z', 'B', 4)];
    expect(clusterDropOps(cross, 'a', 'z', true)).toEqual([]);
  });

  it('spends no round-trip when the drop changes nothing', () => {
    const inPlace = [
      S('a', 'A', 0, '/wt/x'),
      S('c', 'A', 1, '/wt/x'),
      S('b', 'A', 2),
    ];
    expect(clusterDropOps(inPlace, 'a', 'b', true)).toEqual([]);
  });
});

describe('clusterReorderOps', () => {
  // Painted order is a,c,b,d — `.order` is a,b,c,d. This is the keyboard
  // half of the same "one order" rule: ⌘↑/⌘↓ used to walk `.order`, which
  // made a move on a grouped row a silent no-op.
  const sessions = [
    S('a', 'A', 0, '/wt/x'),
    S('b', 'A', 1),
    S('c', 'A', 2, '/wt/x'),
    S('d', 'A', 3),
  ];
  const paintedAfter = (ops: { id: string; order: number }[]) =>
    clusterSessions(
      replay(sessions, ops).map((id, i) => ({
        ...(sessions.find((s) => s.id === id) as (typeof sessions)[number]),
        order: i,
      })),
    ).map((s) => s.id);

  it('moves an ungrouped session one painted slot down', () => {
    expect(paintedAfter(clusterReorderOps(sessions, 'b', +1))).toEqual([
      'a',
      'c',
      'd',
      'b',
    ]);
  });

  // Step 1 of the two-step rule: while the member has room inside its own
  // group, that is what moves.
  it('moves a member within its group before moving the group', () => {
    const ops = clusterReorderOps(sessions, 'a', +1);
    expect(ops.length).toBeGreaterThan(0);
    // a and c swap; b and d stay put.
    expect(paintedAfter(ops)).toEqual(['c', 'a', 'b', 'd']);
  });

  it('moves a member back up within its group', () => {
    expect(paintedAfter(clusterReorderOps(sessions, 'c', -1))).toEqual([
      'c',
      'a',
      'b',
      'd',
    ]);
  });

  // Step 2: at the group's edge the block moves, so the key is never a dead
  // press — and the group is reachable by walking the member to the end.
  it('moves the whole group once the member is at the group edge', () => {
    expect(paintedAfter(clusterReorderOps(sessions, 'c', +1))).toEqual([
      'b',
      'a',
      'c',
      'd',
    ]);
  });

  it('wraps within the project rather than escaping it', () => {
    expect(paintedAfter(clusterReorderOps(sessions, 'd', +1))).toEqual([
      'd',
      'a',
      'c',
      'b',
    ]);
  });

  it('reorders a group that is the whole project', () => {
    const solo = [S('a', 'A', 0, '/wt/x'), S('b', 'A', 1, '/wt/x')];
    expect(replay(solo, clusterReorderOps(solo, 'a', +1))).toEqual(['b', 'a']);
  });

  it('does nothing at the top of a group that is the whole project', () => {
    // No room inside the group, and no siblings for the block to move past.
    const solo = [S('a', 'A', 0, '/wt/x'), S('b', 'A', 1, '/wt/x')];
    expect(clusterReorderOps(solo, 'a', -1)).toEqual([]);
  });

  it('does nothing for delta 0 or an unknown session', () => {
    expect(clusterReorderOps(sessions, 'a', 0)).toEqual([]);
    expect(clusterReorderOps(sessions, 'zz', +1)).toEqual([]);
  });
});

// The bug class the contiguous fixtures above cannot see: a session sitting
// directly beside a group, where the only "slot" between its neighbours is
// one INSIDE that group. A slot space that enumerates rows resolves there,
// the next paint undoes it, and the gesture dies silently — forever, not
// just once.
describe('slots are between blocks, never inside one', () => {
  // Painted [a,c,b,e]; a and c are one block.
  const beside = [
    S('a', 'A', 0, '/wt/x'),
    S('b', 'A', 1),
    S('c', 'A', 2, '/wt/x'),
    S('e', 'A', 3),
  ];
  const painted = (ops: { id: string; order: number }[]) =>
    paintedAfter(beside, ops, replay);

  it('keyboard: a session beside a group can move past it', () => {
    const ops = clusterReorderOps(beside, 'b', -1);
    expect(ops.length).toBeGreaterThan(0); // not a dead press
    expect(painted(ops)).toEqual(['b', 'a', 'c', 'e']);
  });

  it('keyboard: and back down again', () => {
    const up = clusterReorderOps(beside, 'b', -1);
    const moved = replay(beside, up).map((id, i) => ({
      ...(beside.find((s) => s.id === id) as (typeof beside)[number]),
      order: i,
    }));
    expect(
      paintedAfter(moved, clusterReorderOps(moved, 'b', +1), replay),
    ).toEqual(['a', 'c', 'b', 'e']);
  });

  it('drag: dropping a non-member inside a group snaps past the block', () => {
    // e dropped below the painted `a` row — a row-slot inside the a/c block.
    const ops = clusterDropOps(beside, 'e', 'a', false);
    expect(ops.length).toBeGreaterThan(0);
    expect(painted(ops)).toEqual(['a', 'c', 'e', 'b']);
  });

  it('drag: dropping above a group member lands above the whole block', () => {
    const ops = clusterDropOps(beside, 'e', 'c', true);
    expect(painted(ops)).toEqual(['e', 'a', 'c', 'b']);
  });
});

// The regression class the deleted reorder.test.ts covered: r.order
// interleaves projects, so display position and `.order` disagree even
// without any grouping. globalTarget's splice-back is what has to hold.
describe('interleaved projects', () => {
  const mixed = [
    S('a0', 'A', 0),
    S('b0', 'B', 1),
    S('a1', 'A', 2),
    S('b1', 'B', 3),
    S('a2', 'A', 4),
  ];

  it('reorders within one project without disturbing the other', () => {
    const ops = clusterDropOps(mixed, 'a2', 'a0', true);
    const after = replay(mixed, ops);
    // Project B keeps both its members and their relative order.
    expect(after.filter((id) => id.startsWith('b'))).toEqual(['b0', 'b1']);
    expect(after.filter((id) => id.startsWith('a'))).toEqual([
      'a2',
      'a0',
      'a1',
    ]);
  });

  it('keeps a group contiguous across an interleaved list', () => {
    const grouped = [
      S('a0', 'A', 0, '/wt/x'),
      S('b0', 'B', 1),
      S('a1', 'A', 2),
      S('b1', 'B', 3),
      S('a2', 'A', 4, '/wt/x'),
    ];
    const ops = clusterDropOps(grouped, 'a0', 'a1', false);
    const after = replay(grouped, ops);
    const as = after.filter((id) => id.startsWith('a'));
    expect(as).toEqual(['a1', 'a0', 'a2']);
    expect(after.filter((id) => id.startsWith('b'))).toEqual(['b0', 'b1']);
  });

  it('keyboard reorder stays inside its project', () => {
    const ops = clusterReorderOps(mixed, 'a0', +1);
    const after = replay(mixed, ops);
    expect(after.filter((id) => id.startsWith('b'))).toEqual(['b0', 'b1']);
    expect(after.filter((id) => id.startsWith('a'))).toEqual([
      'a1',
      'a0',
      'a2',
    ]);
  });
});
