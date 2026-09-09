import { describe, expect, it } from 'vitest';
import { dropTargetIndex } from '../../src/lib/reorder.js';
import {
  clusterDropOps,
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

describe('clusterDropOps', () => {
  // a and c share a worktree; b and d do not. r.order is a…d = 0…3.
  const sessions = [
    S('a', 'A', 0, '/wt/x'),
    S('b', 'A', 1),
    S('c', 'A', 2, '/wt/x'),
    S('d', 'A', 3),
  ];

  it('matches dropTargetIndex exactly for an unshared session', () => {
    const ops = clusterDropOps(sessions, 'd', 'b', true);
    expect(ops).toEqual([
      { id: 'd', order: dropTargetIndex(sessions, 'd', 'b', true) as number },
    ]);
  });

  it('moves the whole group below a target, contiguously', () => {
    const ops = clusterDropOps(sessions, 'a', 'd', false);
    expect(ops.map((o) => o.id)).toEqual(['a', 'c']);
    expect(replay(sessions, ops)).toEqual(['b', 'd', 'a', 'c']);
  });

  it('moves the whole group above a target, contiguously', () => {
    // Drag the group up: b is above nothing, so drop above b.
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

  it('keeps a group contiguous when it starts split', () => {
    // a and d share a worktree with b and c wedged between them.
    const split = [
      S('a', 'A', 0, '/wt/x'),
      S('b', 'A', 1),
      S('c', 'A', 2),
      S('d', 'A', 3, '/wt/x'),
    ];
    const ops = clusterDropOps(split, 'a', 'c', false);
    expect(replay(split, ops)).toEqual(['b', 'c', 'a', 'd']);
  });

  it('is a no-op when the target is inside the dragged group', () => {
    expect(clusterDropOps(sessions, 'a', 'c', false)).toEqual([]);
    expect(clusterDropOps(sessions, 'a', 'a', false)).toEqual([]);
  });

  it('refuses a cross-project drop', () => {
    const cross = [...sessions, S('z', 'B', 4)];
    expect(clusterDropOps(cross, 'a', 'z', true)).toEqual([]);
  });

  it('spends no round-trip on a member already in place', () => {
    // Dropping the group where it already sits: a is at 0, c follows.
    const inPlace = [
      S('a', 'A', 0, '/wt/x'),
      S('c', 'A', 1, '/wt/x'),
      S('b', 'A', 2),
    ];
    expect(clusterDropOps(inPlace, 'a', 'b', true)).toEqual([]);
  });
});
