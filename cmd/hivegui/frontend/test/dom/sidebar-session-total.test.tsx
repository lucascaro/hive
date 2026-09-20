// @vitest-environment jsdom
//
// The session total next to the sidebar's "Hive" brand (spec 434).
// Rendered by components/Sidebar.tsx › SidebarHeaderControls as a portal
// INTO .brand — a header sibling would be pushed right with the button
// cluster by .brand's `margin-right: auto`. It counts every session in the
// store, minimized ones included, the same as a project card's count.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render } from '@testing-library/react';
import type { SessionInfo } from '../../src/app/state.js';

vi.mock('../../src/bridge.js', () => ({ EventsOn: vi.fn() }));

type Store = typeof import('../../src/store/store.js');
// Fresh module registry per test, so `store` is re-read on every mount
// (see check-updates-button.test.tsx for why).
let store: Store;

const sess = (id: string, project_id: string) =>
  ({ id, name: id, project_id, alive: true }) as SessionInfo;

async function mount(sessions: SessionInfo[]) {
  document.body.innerHTML = `
    <div id="app">
      <div id="terms"></div>
      <aside id="sidebar">
        <header>
          <span class="brand">Hive</span>
          <button id="new-project-btn" type="button" class="hv-icon-btn" data-size="22"></button>
        </header>
        <ul id="projects"></ul>
      </aside>
      <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    </div>`;
  store = await import('../../src/store/store.js');
  store.setSessions(sessions);
  const { SidebarHeaderControls } = await import(
    '../../src/components/Sidebar.js'
  );
  return render(<SidebarHeaderControls />);
}

const brand = () => document.querySelector('#sidebar header .brand');
const total = () => document.querySelector<HTMLElement>('.brand-count');

beforeEach(() => {
  vi.resetModules();
  // minimizeProject persists to localStorage; don't leak it across tests.
  localStorage.clear();
});

afterEach(() => {
  document.body.innerHTML = '';
});

describe('sidebar session total', () => {
  it('shows the total number of sessions next to the brand', async () => {
    await mount([sess('a', 'p1'), sess('b', 'p1'), sess('c', 'p2')]);
    const el = total();
    expect(el?.textContent).toBe('3');
    expect(el?.title).toBe('3 sessions');
    // Inside the brand, not a header sibling.
    expect(el?.parentElement).toBe(brand());
    // A real space, so the accessible name reads "Hive 3", not "Hive3".
    expect(brand()?.textContent).toBe('Hive 3');
  });

  it('counts minimized sessions and sessions of minimized projects', async () => {
    await mount([
      sess('a', 'p1'),
      sess('b', 'p1'),
      sess('c', 'p2'),
      sess('d', 'p2'),
    ]);
    // A session minimized in a project that stays visible, and a whole
    // project minimized: filtering on either set would drop the total.
    act(() => {
      store.minimizeSession('a');
      store.minimizeProject('p2');
    });
    expect(store.appStore.getState().minimized.has('a')).toBe(true);
    expect(store.appStore.getState().minimizedProjects.has('p2')).toBe(true);
    expect(total()?.textContent).toBe('4');
  });

  it('updates live as sessions are added and removed', async () => {
    await mount([sess('a', 'p1'), sess('b', 'p1')]);
    expect(total()?.textContent).toBe('2');
    act(() => store.addSession(sess('c', 'p1')));
    expect(total()?.textContent).toBe('3');
    act(() => store.removeSession('a'));
    expect(total()?.textContent).toBe('2');
  });

  it('is hidden at zero sessions and singular at one', async () => {
    await mount([]);
    expect(total()).toBeNull();
    expect(brand()?.textContent).toBe('Hive');
    act(() => store.addSession(sess('a', 'p1')));
    expect(total()?.textContent).toBe('1');
    expect(total()?.title).toBe('1 session');
  });
});
