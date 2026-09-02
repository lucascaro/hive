// @vitest-environment jsdom
//
// Focus survives a sidebar repaint (components/Sidebar.tsx).
//
// The regression this file was written for: the imperative renderSidebar
// did `projectsUL.innerHTML = ''` and rebuilt every row, so anything
// focused inside it was destroyed and the browser dropped focus to
// <body>. Every `session:event` kind `updated` took that path — and the
// daemon emits one per phase step, one per surviving session after a
// kill recompacts the order, and one when the 200ms agent-session-id
// capture poll lands, up to 30s after a spawn
// (internal/registry/create.go). So focusing a sidebar button and then
// waiting silently lost the keyboard. It redded the mock E2E suite
// (test/e2e/worktrees.spec.ts, spec 257) and was a real bug in the app.
//
// React with a stable key={session.id} removes the rebuild entirely, so
// the second half of the old contract — lib/preserve-focus.ts putting
// focus back after a wipe — has nothing left to do here: focus is never
// lost in the first place, including when the list's SHAPE changes.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { SessionInfo } from '../../src/app/state.js';
import { appStore } from '../../src/store/store.js';
import * as store from '../../src/store/store.js';
import {
  loadSidebar,
  mountSidebar,
  row,
  seed,
  update,
} from './sidebar-harness.js';

const killCalls: Array<[string, boolean | undefined]> = [];

vi.mock('../../src/bridge.js', async (orig) => {
  const real = (await orig()) as Record<string, unknown>;
  return {
    ...real,
    Confirm: () => Promise.resolve(true),
    KillSession: (id: string, force?: boolean) => {
      killCalls.push([id, force]);
      return Promise.resolve();
    },
  };
});

let Sidebar: Awaited<ReturnType<typeof loadSidebar>>;

beforeAll(async () => {
  Sidebar = await loadSidebar();
});

function withSessions(sessions: SessionInfo[]) {
  seed({
    projects: [{ id: 'p1', name: 'proj', color: '#888' }],
    sessions: sessions.map((s) => ({ project_id: 'p1', alive: true, ...s })),
    collapsed: new Set(),
    attention: new Set(),
    minimized: new Set(),
    minimizedProjects: new Set(),
    activeId: null,
  });
  mountSidebar(Sidebar);
}

function killBtn(id: string): HTMLButtonElement {
  const el = row(id).querySelector<HTMLButtonElement>('[data-action="kill"]');
  if (!el) throw new Error(`no kill button for ${id}`);
  return el;
}

const first = () => appStore.getState().sessions[0];

beforeEach(() => {
  seed({});
});

describe('sidebar repaint and focus', () => {
  it('keeps the same row node when only session state changed', () => {
    withSessions([{ id: 'a', name: 'api', order: 0 }]);
    const before = row('a');

    update(() => store.updateSession({ ...first(), phase: '' }));

    // Same node, not a rebuilt one: this is what makes focus, dblclick
    // pairs and an in-progress inline rename survive an `updated`.
    expect(row('a')).toBe(before);
  });

  it('holds focus on a row button across a state-only update', () => {
    withSessions([{ id: 'a', name: 'api', order: 0 }]);
    const btn = killBtn('a');
    btn.focus();
    expect(document.activeElement).toBe(btn);

    update(() => store.updateSession({ ...first(), agent: 'claude' }));

    expect(document.activeElement).toBe(btn);
  });

  it('grows the worktree glyph in place when the branch arrives late', () => {
    // The daemon announces a session in phase `starting` with no branch
    // and only reports the worktree on a later `updated`. The row has to
    // be able to grow that control without being rebuilt, or a
    // worktree-backed session sits there with no way to open the browser.
    withSessions([{ id: 'a', name: 'api', order: 0, phase: 'starting' }]);
    const before = row('a');
    expect(before.querySelector('.hv-session-row__worktree')).toBeNull();

    update(() =>
      store.updateSession({
        ...first(),
        phase: '',
        worktree_branch: 'feat/x',
        worktree_path: '/mock/.worktrees/feat-x',
      }),
    );

    expect(row('a')).toBe(before);
    const wt = row('a').querySelector<HTMLElement>('.hv-session-row__worktree');
    expect(wt).not.toBeNull();
    expect(wt?.getAttribute('aria-label')).toBe(
      'Worktree: feat/x — manage worktrees',
    );
  });

  // The old sidebar fell back to a full innerHTML rebuild whenever the
  // list's shape moved, and then had to put focus back by hand. Keyed
  // reconciliation means a new sibling is an insertion, not a rebuild —
  // the focused node is the SAME node afterwards.
  it('keeps the focused control when a new session joins the list', () => {
    withSessions([
      { id: 'a', name: 'api', order: 0 },
      { id: 'b', name: 'web', order: 1 },
    ]);
    const btn = killBtn('a');
    btn.focus();

    update(() =>
      store.addSession({
        id: 'c',
        name: 'db',
        order: 2,
        project_id: 'p1',
        alive: true,
      } as SessionInfo),
    );

    expect(row('c')).not.toBeNull();
    expect(killBtn('a')).toBe(btn);
    expect(document.activeElement).toBe(btn);
  });

  // A row's handlers outlive the SessionInfo the row was drawn from,
  // because an `updated` re-renders rather than rebuilding. Anything a
  // handler reads beyond the id has to be read at call time. killSession
  // is the sharp edge: it branches on `alive`, and a session is announced
  // in phase `starting` — not yet alive — so a captured snapshot sends
  // force=true and skips the daemon's dirty-worktree refusal outright.
  // The mock E2E suite caught exactly this.
  it('reads live session state in row handlers, not the build-time snapshot', async () => {
    killCalls.length = 0;
    withSessions([
      { id: 'a', name: 'api', order: 0, alive: false, phase: 'starting' },
    ]);
    const btn = killBtn('a');

    // The daemon reports it up. Same node, re-rendered in place.
    update(() => store.updateSession({ ...first(), alive: true, phase: '' }));
    expect(killBtn('a')).toBe(btn);

    btn.click();
    await Promise.resolve();
    await Promise.resolve();

    // force=false, so the daemon still gets to refuse a dirty worktree.
    expect(killCalls).toEqual([['a', false]]);
  });

  // Reordering is the one update that can MOVE a row rather than patch it,
  // and a raw insertBefore of a focused node is a remove+insert that blurs
  // (jsdom included — measured). The imperative sidebar wrapped its whole
  // render in lib/preserve-focus.ts partly for that.
  //
  // Through React it does not happen: the reconciler moves the minimum set
  // of children, and the surviving rows keep both their node and their
  // parent, so nothing is detached and nothing is blurred. This pins that
  // — it is the observation that lets the island carry no focus-restore
  // layer of its own. If React ever started rebuilding rows here, this
  // fails.
  const reorder = (order: string[]) =>
    update(() => {
      store.setSessions(
        order.map((id, i) => ({
          id,
          name: id,
          project_id: 'p1',
          order: i,
          alive: true,
        })),
      );
    });

  it('keeps focus on a row control across a reorder that moves it', () => {
    withSessions([
      { id: 'a', name: 'a', order: 0 },
      { id: 'b', name: 'b', order: 1 },
      { id: 'c', name: 'c', order: 2 },
    ]);
    const btn = killBtn('a');
    btn.focus();

    reorder(['c', 'a', 'b']);

    const rows = [
      ...document.querySelectorAll<HTMLElement>('.hv-session-row'),
    ].map((el) => el.dataset.sid);
    expect(rows).toEqual(['c', 'a', 'b']); // really did move
    expect(killBtn('a')).toBe(btn); // …without rebuilding the row
    expect(document.activeElement).toBe(btn);
  });

  // Never yank the keyboard back from something that claimed it on
  // purpose during the same update — a terminal, a modal, an inline
  // rename. Same rule as lib/preserve-focus.ts and app/focus.ts.
  it('leaves focus where an update deliberately moved it', () => {
    withSessions([
      { id: 'a', name: 'a', order: 0 },
      { id: 'b', name: 'b', order: 1 },
      { id: 'c', name: 'c', order: 2 },
    ]);
    killBtn('a').focus();
    const elsewhere = document.createElement('button');
    document.body.append(elsewhere);

    update(() => {
      store.setSessions(
        ['c', 'a', 'b'].map((id, i) => ({
          id,
          name: id,
          project_id: 'p1',
          order: i,
          alive: true,
        })),
      );
      elsewhere.focus();
    });

    expect(document.activeElement).toBe(elsewhere);
    elsewhere.remove();
  });

  it('leaves focus alone when it was never inside the sidebar', () => {
    withSessions([{ id: 'a', name: 'api', order: 0 }]);
    const outside = document.createElement('button');
    document.body.append(outside);
    outside.focus();

    update(() =>
      store.addSession({
        id: 'z',
        name: 'other',
        order: 9,
        project_id: 'p1',
        alive: true,
      } as SessionInfo),
    );

    expect(document.activeElement).toBe(outside);
    outside.remove();
  });
});
