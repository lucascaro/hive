// @vitest-environment jsdom
//
// The performance half of the rewrite, pinned.
//
// `updateSession()` replaces the sessions ARRAY, so the sidebar
// re-renders on every `session:event` — one per phase step, one per
// surviving session when a kill recompacts the order, one when the
// agent-session-id capture poll lands up to 30s after a spawn, and one
// per `title` event, which a busy agent emits as fast as it redraws.
//
// The imperative sidebar grew a whole second code path
// (`updateSidebarTitles`, spec 248) to stop that churn from rebuilding
// every row. The React island's equivalent is the memo on SessionItem
// plus the referentially stable `sidebar` prop. Without both, a title
// event re-renders every row in the list and the migration trades one
// perf problem for the same one — so this asserts the scope directly,
// by counting SessionRow renders.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { appStore } from '../../src/store/store.js';
import * as store from '../../src/store/store.js';
import { loadSidebar, mountSidebar, seed, update } from './sidebar-harness.js';

const renders: string[] = [];

vi.mock('../../src/components/SessionRow.js', () => ({
  SessionRow: (p: { session: { id: string } }) => {
    renders.push(p.session.id);
    return <li className="hv-session-row" data-sid={p.session.id} />;
  },
}));

let Sidebar: Awaited<ReturnType<typeof loadSidebar>>;

beforeAll(async () => {
  Sidebar = await loadSidebar();
});

beforeEach(() => {
  seed({
    projects: [{ id: 'p1', name: 'proj' }],
    sessions: [
      { id: 'a', name: 'api', project_id: 'p1', order: 0, alive: true },
      { id: 'b', name: 'web', project_id: 'p1', order: 1, alive: true },
      { id: 'c', name: 'db', project_id: 'p1', order: 2, alive: true },
    ],
    collapsed: new Set(),
    activeId: null,
  });
  mountSidebar(Sidebar);
  renders.length = 0;
});

const byId = (id: string) =>
  appStore.getState().sessions.find((s) => s.id === id);

describe('sidebar re-render scope', () => {
  it('re-renders only the retitled row on a title event', () => {
    const a = byId('a');
    if (!a) throw new Error('no session a');
    update(() => store.updateSession({ ...a, title: 'npm run build' }));
    expect(renders).toEqual(['a']);
  });

  it('re-renders only the two rows a selection change touches', () => {
    update(() => store.setActiveId('a'));
    expect(renders).toEqual(['a']);
    renders.length = 0;
    update(() => store.setActiveId('b'));
    // The row losing the selection and the row gaining it — not the third.
    expect(renders.sort()).toEqual(['a', 'b']);
  });

  it('re-renders only the ringing row on a bell', () => {
    update(() => {
      const s = appStore.getState().sessions.find((x) => x.id === 'c');
      if (s) store.updateSession({ ...s, needs_attention: true });
    });
    expect(renders).toEqual(['c']);
  });

  // A store write that changes nothing must not notify at all — the
  // contract store.ts's `set()` keeps.
  it('does not re-render on a no-op store write', () => {
    const a = byId('a');
    if (!a) throw new Error('no session a');
    update(() => store.updateSession(a));
    expect(renders).toEqual([]);
  });

  // A new session is an insertion, not a rebuild: the rows already on
  // screen keep their markup. Their key hints are unchanged here because
  // the newcomer sorts last.
  it('renders only the new row when a session is added', () => {
    update(() =>
      store.addSession({
        id: 'd',
        name: 'cache',
        project_id: 'p1',
        order: 3,
        alive: true,
      }),
    );
    expect(renders).toEqual(['d']);
  });
});
