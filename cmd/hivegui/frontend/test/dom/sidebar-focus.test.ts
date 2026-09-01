// @vitest-environment jsdom
//
// Focus survives a sidebar repaint (src/app/sidebar.ts +
// src/lib/preserve-focus.ts).
//
// The regression: renderSidebar does `projectsUL.innerHTML = ''` and
// rebuilds every row, so anything focused inside it was destroyed and the
// browser dropped focus to <body>. Every `session:event` kind `updated`
// took that path — and the daemon emits one per phase step, one per
// surviving session after a kill recompacts the order, and one when the
// 200ms agent-session-id capture poll lands, up to 30s after a spawn
// (internal/registry/create.go). So focusing a sidebar button and then
// waiting silently lost the keyboard. It reds the mock E2E suite
// (test/e2e/worktrees.spec.ts, spec 257) and it is a real bug in the app.
//
// Two things are pinned here: the state-only path patches rows instead of
// rebuilding them, and the rebuild path — still reachable whenever the
// shape genuinely moves — puts focus back where it was.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { SessionInfo } from '../../src/app/state.js';
import * as store from '../../src/store/store.js';

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

let state: typeof import('../../src/app/state.js').state;
let renderSidebar: () => void;
let updateSidebarRows: () => void;

const noop = () => {};

beforeAll(async () => {
  document.body.innerHTML = `
    <div id="app"><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    <div id="terms"></div><div id="minimized-tray"></div>
    <div id="empty-state"></div></div>`;
  ({ state } = await import('../../src/app/state.js'));
  const sidebar = await import('../../src/app/sidebar.js');
  ({ renderSidebar, updateSidebarRows } = sidebar);
  sidebar.initSidebar({
    switchTo: noop,
    switchToProject: noop,
    minimizeProject: noop,
    restoreProject: noop,
    minimizeSession: noop,
    restoreSession: noop,
    confirmAndDeleteProject: noop,
    renderEmptyState: noop,
    refocusActiveTerm: noop,
  });
});

function seed(sessions: SessionInfo[]) {
  state.projects = [{ id: 'p1', name: 'proj', color: '#888' }];
  state.sessions = sessions.map((s) => ({
    project_id: 'p1',
    alive: true,
    ...s,
  }));
  state.collapsed = new Set();
  state.attention = new Set();
  state.minimized = new Set();
  state.minimizedProjects = new Set();
  state.activeId = null;
  renderSidebar();
}

function row(id: string): HTMLElement {
  const el = document.querySelector<HTMLElement>(
    `.hv-session-row[data-sid="${id}"]`,
  );
  if (!el) throw new Error(`no sidebar row for ${id}`);
  return el;
}

function killBtn(id: string): HTMLButtonElement {
  const el = row(id).querySelector<HTMLButtonElement>('[data-action="kill"]');
  if (!el) throw new Error(`no kill button for ${id}`);
  return el;
}

beforeEach(() => {
  const ul = document.getElementById('projects');
  if (ul) ul.innerHTML = '';
});

describe('sidebar repaint and focus', () => {
  it('keeps the same row node when only session state changed', () => {
    seed([{ id: 'a', name: 'api', order: 0 }]);
    const before = row('a');

    state.sessions[0] = { ...state.sessions[0], phase: '' };
    updateSidebarRows();

    // Same node, not a rebuilt one: this is what makes focus, dblclick
    // pairs and an in-progress inline rename survive an `updated`.
    expect(row('a')).toBe(before);
  });

  it('holds focus on a row button across a state-only update', () => {
    seed([{ id: 'a', name: 'api', order: 0 }]);
    const btn = killBtn('a');
    btn.focus();
    expect(document.activeElement).toBe(btn);

    state.sessions[0] = { ...state.sessions[0], agent: 'claude' };
    updateSidebarRows();

    expect(document.activeElement).toBe(btn);
  });

  it('grows the worktree glyph in place when the branch arrives late', () => {
    // The daemon announces a session in phase `starting` with no branch
    // and only reports the worktree on a later `updated`. The in-place
    // path has to be able to CREATE that control, or a worktree-backed
    // session sits there with no way to open the browser until something
    // else forces a rebuild.
    seed([{ id: 'a', name: 'api', order: 0, phase: 'starting' }]);
    const before = row('a');
    expect(before.querySelector('.hv-session-row__worktree')).toBeNull();

    state.sessions[0] = {
      ...state.sessions[0],
      phase: '',
      worktree_branch: 'feat/x',
      worktree_path: '/mock/.worktrees/feat-x',
    };
    updateSidebarRows();

    expect(row('a')).toBe(before);
    const wt = row('a').querySelector<HTMLElement>('.hv-session-row__worktree');
    expect(wt).not.toBeNull();
    expect(wt?.getAttribute('aria-label')).toBe(
      'Worktree: feat/x — manage worktrees',
    );
  });

  it('restores focus to the equivalent control when a rebuild is forced', () => {
    seed([
      { id: 'a', name: 'api', order: 0 },
      { id: 'b', name: 'web', order: 1 },
    ]);
    const btn = killBtn('a');
    btn.focus();

    // A new session is a genuine shape change, so this falls back to the
    // full rebuild — the node holding focus is destroyed.
    store.addSession({
      id: 'c',
      name: 'db',
      order: 2,
      project_id: 'p1',
      alive: true,
    } as SessionInfo);
    updateSidebarRows();

    const after = killBtn('a');
    expect(after).not.toBe(btn); // really was rebuilt
    expect(document.activeElement).toBe(after);
  });

  // A row's handlers now outlive the SessionInfo the row was drawn from,
  // because `updated` no longer rebuilds the row. Anything a handler reads
  // beyond the id has to be read at call time. killSession is the sharp
  // edge: it branches on `alive`, and a session is announced in phase
  // `starting` — not yet alive — so a captured snapshot sends force=true
  // and skips the daemon's dirty-worktree refusal outright. The mock E2E
  // suite caught exactly this.
  it('reads live session state in row handlers, not the build-time snapshot', async () => {
    killCalls.length = 0;
    seed([{ id: 'a', name: 'api', order: 0, alive: false, phase: 'starting' }]);
    const btn = killBtn('a');

    // The daemon reports it up. Same node, patched in place.
    state.sessions[0] = { ...state.sessions[0], alive: true, phase: '' };
    updateSidebarRows();
    expect(killBtn('a')).toBe(btn);

    btn.click();
    await Promise.resolve();
    await Promise.resolve();

    // force=false, so the daemon still gets to refuse a dirty worktree.
    expect(killCalls).toEqual([['a', false]]);
  });

  it('leaves focus alone when it was never inside the sidebar', () => {
    seed([{ id: 'a', name: 'api', order: 0 }]);
    const outside = document.createElement('button');
    document.body.append(outside);
    outside.focus();

    store.addSession({
      id: 'z',
      name: 'other',
      order: 9,
      project_id: 'p1',
      alive: true,
    } as SessionInfo);
    updateSidebarRows();

    expect(document.activeElement).toBe(outside);
    outside.remove();
  });
});
