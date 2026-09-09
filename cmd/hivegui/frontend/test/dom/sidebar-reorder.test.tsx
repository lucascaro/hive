// @vitest-environment jsdom
//
// Drag-to-reorder from the sidebar (components/Sidebar.tsx →
// src/lib/reorder.ts).
//
// The index math is NOT re-derived here: a drop hands its target and
// side to dropTargetIndex() and forwards whatever comes back. That is
// the invariant this file pins — the off-by-one that spec 305 fixed
// lives in the pure function and is table-tested next to it
// (test/unit/reorder.test.ts), and a second copy of the arithmetic in
// the component is exactly how the two drift apart.
//
// jsdom gives every element a zero rect, so `clientY - top < height / 2`
// is always false: every drop below reads as "insert after the target".
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { appStore } from '../../src/store/store.js';
import { loadSidebar, mountSidebar, row, seed } from './sidebar-harness.js';

const sessionOrders: Array<[string, number]> = [];
// 1-based index of the UpdateSession call that should reject, or null.
let failAt: number | null = null;
const projectOrders: Array<[string, number]> = [];

vi.mock('../../src/bridge.js', async (orig) => {
  const real = (await orig()) as Record<string, unknown>;
  return {
    ...real,
    UpdateSession: (id: string, _n: string, _c: string, order: number) => {
      sessionOrders.push([id, order]);
      if (failAt !== null && sessionOrders.length === failAt) {
        return Promise.reject(new Error('daemon said no'));
      }
      return Promise.resolve();
    },
    UpdateProject: (
      id: string,
      _n: string,
      _c: string,
      _cwd: string,
      order: number,
    ) => {
      projectOrders.push([id, order]);
      return Promise.resolve();
    },
  };
});

let Sidebar: Awaited<ReturnType<typeof loadSidebar>>;

beforeAll(async () => {
  Sidebar = await loadSidebar();
});

beforeEach(() => {
  failAt = null;
  sessionOrders.length = 0;
  projectOrders.length = 0;
  seed({
    projects: [
      { id: 'p1', name: 'one', order: 0 },
      { id: 'p2', name: 'two', order: 1 },
    ],
    sessions: [
      { id: 'a', name: 'a', project_id: 'p1', order: 0, alive: true },
      { id: 'b', name: 'b', project_id: 'p1', order: 1, alive: true },
      { id: 'c', name: 'c', project_id: 'p1', order: 2, alive: true },
      { id: 'd', name: 'd', project_id: 'p2', order: 3, alive: true },
    ],
    collapsed: new Set(),
    activeId: null,
  });
  mountSidebar(Sidebar);
});

// A drop carrying `key` as its payload. The event is a plain Event with
// dataTransfer and clientY defined on it, because jsdom implements
// neither DragEvent nor DataTransfer.
function dropOn(target: HTMLElement, key: string, draggedID: string) {
  // getData honours the key, like a real DataTransfer: the session drop
  // handler does not gate on `types` (it never did) — it asks for its own
  // payload and gets '' when the drag is carrying something else.
  const dt = {
    types: [key],
    dropEffect: '',
    getData: (k: string) => (k === key ? draggedID : ''),
  };
  const ev = new Event('drop', { bubbles: true, cancelable: true });
  Object.defineProperty(ev, 'dataTransfer', { value: dt });
  Object.defineProperty(ev, 'clientY', { value: 0 });
  target.dispatchEvent(ev);
}

function cardFor(pid: string): HTMLElement {
  const el = document.querySelector<HTMLElement>(
    `.hv-project-card[data-pid="${pid}"]`,
  );
  if (!el) throw new Error(`no card for ${pid}`);
  return el;
}

describe('sidebar session reorder', () => {
  it('drops a session below the row it landed on', () => {
    // [a,b,c] with c dropped below a → [a,c,b]. The index is pinned rather
    // than re-derived from the helper under test, so a broken helper cannot
    // agree with itself.
    dropOn(row('a'), 'text/x-hive-session', 'c');
    expect(sessionOrders).toEqual([['c', 1]]);
  });

  it('does nothing when a session is dropped on itself', () => {
    dropOn(row('a'), 'text/x-hive-session', 'a');
    expect(sessionOrders).toEqual([]);
  });

  // Cross-project moves would also have to rewrite project_id on the
  // wire, which the daemon does not accept yet.
  it('refuses a cross-project drop', () => {
    dropOn(row('d'), 'text/x-hive-session', 'a');
    expect(sessionOrders).toEqual([]);
  });

  it('ignores a drop whose payload is not a session', () => {
    dropOn(row('a'), 'text/x-hive-project', 'c');
    expect(sessionOrders).toEqual([]);
  });
});

describe('sidebar project reorder', () => {
  // The daemon's moveProjectLocked removes the dragged project and then
  // inserts at newOrder, so a source that sits BEFORE the target has to
  // compensate by one. p2 dropped below p1 is already where it is, so
  // the only non-trivial direction is the other one.
  it('sends the compensated index for a drop below the target', () => {
    dropOn(cardFor('p2'), 'text/x-hive-project', 'p1');
    expect(projectOrders).toEqual([['p1', 1]]);
  });

  it('does nothing when a project is dropped on itself', () => {
    dropOn(cardFor('p1'), 'text/x-hive-project', 'p1');
    expect(projectOrders).toEqual([]);
  });

  it('ignores a drop whose payload is not a project', () => {
    dropOn(cardFor('p2'), 'text/x-hive-session', 'p1');
    expect(projectOrders).toEqual([]);
  });
});

// Sessions that share a worktree paint as a block (lib/worktree-groups.ts:
// clusterSessions), so a drag has to move the block. Moving one member
// alone would look like nothing happened: the cluster rule puts it straight
// back beside its group on the next paint.
describe('sidebar reorder with a shared worktree', () => {
  const WT = '/repo/.worktrees/feat';

  // a and c share a worktree; b and e do not. r.order is a,b,c,e = 0…3.
  const seedShared = () =>
    seed({
      projects: [{ id: 'p1', name: 'one', order: 0 }],
      sessions: [
        {
          id: 'a',
          name: 'a',
          project_id: 'p1',
          order: 0,
          alive: true,
          worktree_path: WT,
        },
        { id: 'b', name: 'b', project_id: 'p1', order: 1, alive: true },
        {
          id: 'c',
          name: 'c',
          project_id: 'p1',
          order: 2,
          alive: true,
          worktree_path: WT,
        },
        { id: 'e', name: 'e', project_id: 'p1', order: 3, alive: true },
      ],
      collapsed: new Set(),
      activeId: null,
    });

  // The ops after the first are issued from a microtask, so let the queue
  // drain before asserting on the full sequence.
  const flush = () => new Promise((r) => setTimeout(r, 0));

  // Assertions are on the ORDER the captured ops produce, not on which
  // sessions they name: a reorder computes the target order and emits the
  // shortest sequence of daemon moves that reaches it, so the moved ids are
  // an implementation detail while the resulting list is the behaviour.
  const moveInOrder = (ids: string[], id: string, at: number) => {
    const cur = ids.indexOf(id);
    if (cur < 0) return ids;
    const out = ids.slice();
    out.splice(cur, 1);
    out.splice(Math.min(Math.max(at, 0), out.length), 0, id);
    return out;
  };
  const resultOrder = () =>
    sessionOrders.reduce(
      (ids, [id, at]) => moveInOrder(ids, id, at),
      ['a', 'b', 'c', 'e'],
    );

  beforeEach(() => {
    sessionOrders.length = 0;
    seedShared();
    mountSidebar(Sidebar);
  });

  it('paints the group together, at its lowest-order member', () => {
    const ids = [
      ...document.querySelectorAll<HTMLElement>(
        '.hv-project-card[data-pid="p1"] .hv-session-row',
      ),
    ].map((el) => el.dataset.sid);
    expect(ids).toEqual(['a', 'c', 'b', 'e']);
  });

  it('moves the whole group when any member is dragged', async () => {
    dropOn(row('e'), 'text/x-hive-session', 'a');
    await flush();
    // a and c land below e, still adjacent.
    expect(resultOrder()).toEqual(['b', 'e', 'a', 'c']);
  });

  it('moves the group identically when dragged by a non-head member', async () => {
    dropOn(row('e'), 'text/x-hive-session', 'c');
    await flush();
    expect(resultOrder()).toEqual(['b', 'e', 'a', 'c']);
  });

  it('reorders within the group when the drop lands on a fellow member', async () => {
    // a and c share a worktree. Dropping a below c swaps the two inside
    // the group; b and e must not move.
    dropOn(row('c'), 'text/x-hive-session', 'a');
    await flush();
    expect(resultOrder()).toEqual(['c', 'a', 'b', 'e']);
  });

  it('stops after a failed op rather than half-applying the rest', async () => {
    failAt = 1;
    dropOn(row('e'), 'text/x-hive-session', 'a');
    await flush();
    // The move needs two ops; the first rejects, so the second is never
    // issued. Half a cluster move is bad, but a cluster move that keeps
    // going after the daemon refused is worse.
    expect(sessionOrders).toHaveLength(1);
  });
});
