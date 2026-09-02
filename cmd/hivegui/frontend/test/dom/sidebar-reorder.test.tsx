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
const projectOrders: Array<[string, number]> = [];

vi.mock('../../src/bridge.js', async (orig) => {
  const real = (await orig()) as Record<string, unknown>;
  return {
    ...real,
    UpdateSession: (id: string, _n: string, _c: string, order: number) => {
      sessionOrders.push([id, order]);
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
    attention: new Set(),
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
