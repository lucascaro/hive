// @vitest-environment jsdom
//
// Inline rename from the sidebar, and the node-identity invariant it
// rests on.
//
// The bug this pins: the imperative sidebar's only structural path was
// `projectsUL.innerHTML = ''`, and a `session:event` kind `updated` —
// which the daemon emits on every phase step, once per surviving session
// after a kill recompacts the order, and again when the agent-session-id
// capture poll lands up to 30s after a spawn — took it. If one landed
// between a user's two clicks, the second click hit a DIFFERENT <li> and
// no dblclick pair ever formed, so the rename simply never opened.
//
// React keys rows by session id, so the node survives the update. That
// is what these assertions are: the same node, before and after.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { fireEvent } from '@testing-library/react';
import { appStore } from '../../src/store/store.js';
import * as store from '../../src/store/store.js';
import {
  card,
  loadSidebar,
  mountSidebar,
  row,
  seed,
  update,
} from './sidebar-harness.js';

const renameCalls: Array<[string, string]> = [];
const projectRenameCalls: Array<[string, string]> = [];

vi.mock('../../src/bridge.js', async (orig) => {
  const real = (await orig()) as Record<string, unknown>;
  return {
    ...real,
    UpdateSession: (id: string, name: string) => {
      renameCalls.push([id, name]);
      return Promise.resolve();
    },
    UpdateProject: (id: string, name: string) => {
      projectRenameCalls.push([id, name]);
      return Promise.resolve();
    },
  };
});

let Sidebar: Awaited<ReturnType<typeof loadSidebar>>;

beforeAll(async () => {
  Sidebar = await loadSidebar();
});

beforeEach(() => {
  renameCalls.length = 0;
  projectRenameCalls.length = 0;
  seed({
    projects: [{ id: 'p1', name: 'proj', color: '#888' }],
    sessions: [
      { id: 'a', name: 'api', project_id: 'p1', order: 0, alive: true },
      { id: 'b', name: 'web', project_id: 'p1', order: 1, alive: true },
    ],
    collapsed: new Set(),
    activeId: null,
  });
  mountSidebar(Sidebar);
});

const first = () => appStore.getState().sessions[0];
const nameInput = () =>
  document.querySelector<HTMLInputElement>('input.name-input');

describe('sidebar dblclick rename', () => {
  it('keeps the row node across an update landing between the two clicks', () => {
    const before = row('a');
    fireEvent.click(before);

    // The `updated` that used to wipe the list. Twice, for good measure —
    // the poll fires repeatedly while the user is still deciding.
    update(() => store.updateSession({ ...first(), title: 'npm run build' }));
    update(() => store.updateSession({ ...first(), agent: 'claude' }));

    expect(row('a')).toBe(before);

    fireEvent.dblClick(before);
    expect(before.querySelector('input.name-input')).not.toBeNull();
  });

  it('opens an input over the name and commits on Enter', () => {
    const r = row('a');
    fireEvent.dblClick(r);
    const input = nameInput();
    expect(input).not.toBeNull();
    if (!input) return;
    expect(input.value).toBe('api');
    // The label is out of the DOM while the editor holds its place.
    expect(r.querySelector('.hv-session-row__name')).toBeNull();

    input.value = 'gateway';
    fireEvent.keyDown(input, { key: 'Enter' });

    expect(renameCalls).toEqual([['a', 'gateway']]);
    // …and the label comes back, ready for the store update to fill it.
    expect(r.querySelector('.hv-session-row__name')).not.toBeNull();
    expect(nameInput()).toBeNull();
  });

  it('cancels on Escape without renaming', () => {
    const r = row('a');
    fireEvent.dblClick(r);
    const input = nameInput();
    if (!input) throw new Error('no rename input');
    input.value = 'nope';
    fireEvent.keyDown(input, { key: 'Escape' });

    expect(renameCalls).toEqual([]);
    expect(r.querySelector('.hv-session-row__name')?.textContent).toBe('api');
  });

  it('re-renders the row with the new name once the daemon confirms', () => {
    const r = row('a');
    fireEvent.dblClick(r);
    const input = nameInput();
    if (!input) throw new Error('no rename input');
    input.value = 'gateway';
    fireEvent.keyDown(input, { key: 'Enter' });

    update(() => store.updateSession({ ...first(), name: 'gateway' }));
    expect(row('a')).toBe(r);
    expect(r.querySelector('.hv-session-row__name')?.textContent).toBe(
      'gateway',
    );
  });

  it('renames a project from a dblclick on its name', () => {
    const name = card('p1')?.querySelector<HTMLElement>(
      '.hv-project-card__name',
    );
    if (!name) throw new Error('no project name');
    fireEvent.dblClick(name);
    const input = document.querySelector<HTMLInputElement>(
      'input.project-name-input',
    );
    expect(input).not.toBeNull();
    if (!input) return;
    input.value = 'hive';
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(projectRenameCalls).toEqual([['p1', 'hive']]);
  });

  // Only the header background and the name open the editor; every
  // control in the header has its own job.
  it('ignores a dblclick on a header action button', () => {
    const btn = card('p1')?.querySelector<HTMLElement>('[data-action="edit"]');
    if (!btn) throw new Error('no edit button');
    fireEvent.dblClick(btn);
    expect(document.querySelector('input.project-name-input')).toBeNull();
  });
});
