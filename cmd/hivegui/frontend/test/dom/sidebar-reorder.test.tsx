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
import { dropTargetIndex } from '../../src/lib/reorder.js';
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
  it('forwards exactly what dropTargetIndex resolves', () => {
    dropOn(row('a'), 'text/x-hive-session', 'c');
    const expected = dropTargetIndex(
      appStore.getState().sessions,
      'c',
      'a',
      false,
    );
    expect(expected).toBe(1); // pinned, so a broken helper can't agree
    expect(sessionOrders).toEqual([['c', expected]]);
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
    expect(sessionOrders).toEqual([
      ['a', 3],
      ['c', 3],
    ]);
  });

  it('moves the group identically when dragged by a non-head member', async () => {
    dropOn(row('e'), 'text/x-hive-session', 'c');
    await flush();
    expect(sessionOrders).toEqual([
      ['a', 3],
      ['c', 3],
    ]);
  });

  it('does nothing when the drop lands inside the dragged group', async () => {
    dropOn(row('c'), 'text/x-hive-session', 'a');
    await flush();
    expect(sessionOrders).toEqual([]);
  });

  it('stops after a failed op rather than half-applying the rest', async () => {
    failAt = 1;
    dropOn(row('e'), 'text/x-hive-session', 'a');
    await flush();
    expect(sessionOrders).toEqual([['a', 3]]);
  });
});
