// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// @vitest-environment jsdom
//
// The ad-hoc grid (spec 533): ⌘/Ctrl-click sidebar sessions to tile just
// those, across projects. jsdom reports a non-mac platform, so the toggle
// modifier here is Ctrl; the ⌘ half of the rule is pinned by
// isSelectToggle's unit test.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { fireEvent, render } from '@testing-library/react';
import type { ReactNode } from 'react';
import type { SidebarProps } from '../../src/components/Sidebar.js';
import * as store from '../../src/store/store.js';

vi.mock('../../src/bridge.js', () => {
  const fn = () => vi.fn(() => Promise.resolve());
  return {
    ConnectControl: fn(),
    OpenSession: fn(),
    CloseAttach: fn(),
    WriteStdin: fn(),
    ResizeSession: fn(),
    RequestScrollbackReplay: fn(),
    CreateSession: fn(),
    KillSession: fn(),
    RestartSession: fn(),
    UpdateSession: fn(),
    UpdateProject: fn(),
    Notify: fn(),
    Confirm: fn(),
    EventsOn: vi.fn(),
    WindowSetTitle: vi.fn(),
    ClipboardGetText: fn(),
  };
});

let state: typeof import('../../src/store/store.js').hiveStateView;
let Sidebar: (p: SidebarProps) => ReactNode;
let view: typeof import('../../src/app/view.js');
let actions: typeof import('../../src/app/actions.js');
let scope: typeof import('../../src/app/grid-layout.js').gridScopeSessions;

const noop = () => {};

beforeAll(async () => {
  document.body.innerHTML = `
    <div id="app">
      <ul id="projects"></ul>
      <div id="minimized-projects" class="hidden" role="toolbar" aria-label="Minimized projects"></div>
      <div id="status"><span id="status-text"></span><span id="status-hint"></span></div><div id="terms"></div>
      <div id="minimized-tray"></div><div id="empty-state"></div>
    </div>`;
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  ({ hiveStateView: state } = await import('../../src/store/store.js'));
  view = await import('../../src/app/view.js');
  await import('../../src/app/keyboard.js');
  actions = await import('../../src/app/actions.js');
  scope = (await import('../../src/app/grid-layout.js')).gridScopeSessions;
  ({ Sidebar } = await import('../../src/components/Sidebar.js'));
  const { createScrollTrace } = await import('../../src/lib/scroll-debug.js');
  view.initView({
    ensureTerm: () => undefined as never,
    setActive: (id) => {
      state.activeId = id;
    },
    focusActiveTerm: noop,
    scrollTrace: createScrollTrace({ enabled: false }),
  });
});

const NS = 'domtest-gs';

const sess = (id: string, project_id: string, order: number) => ({
  id,
  name: id,
  project_id,
  order,
  alive: true,
  phase: '',
});

beforeEach(() => {
  localStorage.clear();
  store.hydratePersistedProjectSets(NS);
  state.projects = [
    { id: 'p1', name: 'one', color: '#111', order: 0 },
    { id: 'p2', name: 'two', color: '#222', order: 1 },
  ];
  state.sessions = [
    sess('a', 'p1', 0),
    sess('b', 'p1', 1),
    sess('c', 'p2', 2),
    sess('d', 'p2', 3),
  ];
  state.collapsed = new Set();
  state.minimized = new Set();
  state.minimizedProjects = new Set();
  state.gridSelection = [];
  state.gridSelectMode = 'auto';
  state.activeId = 'a';
  state.currentProjectId = 'p1';
  state.view = 'single';
  render(
    <Sidebar
      selectSession={view.selectSession}
      toggleGridSelection={view.toggleGridSelection}
      switchToProject={noop}
      minimizeProject={view.minimizeProject}
      restoreProject={view.restoreProject}
      minimizeSession={view.minimizeSession}
      restoreSession={view.restoreSession}
      confirmAndDeleteProject={noop}
      refocusActiveTerm={noop}
      trayEl={document.getElementById('minimized-projects')}
    />,
    { container: document.getElementById('projects') as HTMLElement },
  );
});

const rowEl = (id: string) =>
  document.querySelector<HTMLElement>(
    `.hv-session-row[data-sid="${id}"]`,
  ) as HTMLElement;
const ctrlClick = (id: string) => fireEvent.click(rowEl(id), { ctrlKey: true });
const click = (id: string) => fireEvent.click(rowEl(id));
const tiles = () => scope().map((s) => s.id);

describe('⌘/Ctrl-click opens and edits the ad-hoc grid', () => {
  it('opens a 2-tile grid of the active session and the clicked one, across projects', () => {
    ctrlClick('c'); // a is in p1, c in p2
    expect(state.view).toBe('grid-select');
    expect(tiles()).toEqual(['a', 'c']);
  });

  it('adds a tile on a further click and removes one on a repeat', () => {
    ctrlClick('c');
    ctrlClick('b');
    expect(tiles()).toEqual(['a', 'b', 'c']);
    ctrlClick('b');
    expect(tiles()).toEqual(['a', 'c']);
    expect(state.view).toBe('grid-select');
  });

  it('returns to single on the remaining session below two tiles', () => {
    ctrlClick('c');
    ctrlClick('c');
    expect(state.view).toBe('single');
    expect(state.activeId).toBe('a');
  });

  it('lands on the survivor when the ACTIVE tile is toggled off a 2-grid', () => {
    ctrlClick('c');
    ctrlClick('a');
    expect(state.view).toBe('single');
    expect(state.activeId).toBe('c');
  });

  it('moves focus to a remaining tile when the active tile leaves a 3-grid', () => {
    ctrlClick('c');
    ctrlClick('b');
    ctrlClick('a');
    expect(state.view).toBe('grid-select');
    expect(tiles()).toEqual(['b', 'c']);
    expect(['b', 'c']).toContain(state.activeId);
  });

  it('treats Meta-click off macOS as a plain click', () => {
    fireEvent.click(rowEl('c'), { metaKey: true });
    expect(state.view).toBe('single');
    expect(state.activeId).toBe('c');
    expect(state.gridSelection).toEqual([]);
  });

  it('marks selected rows other than the active one', () => {
    ctrlClick('c');
    ctrlClick('d');
    expect(rowEl('c').hasAttribute('data-in-selection')).toBe(true);
    expect(rowEl('d').hasAttribute('data-in-selection')).toBe(true);
    expect(rowEl('b').hasAttribute('data-in-selection')).toBe(false);
  });

  it('never saves the ad-hoc view', () => {
    ctrlClick('c');
    expect(localStorage.getItem('hive.view')).not.toBe('grid-select');
    ctrlClick('c');
    expect(localStorage.getItem('hive.view')).not.toBe('grid-select');
  });
});

describe('plain click and navigation', () => {
  it('a plain click clears the selection and leaves the grid', () => {
    ctrlClick('c');
    click('d');
    expect(state.view).toBe('single');
    expect(state.gridSelection).toEqual([]);
    expect(state.activeId).toBe('d');
  });

  it('switching to a session outside the selection leaves the grid and clears it', () => {
    ctrlClick('c');
    view.switchTo('b'); // ⌘1–9 / palette / nav all land here
    expect(state.view).toBe('single');
    expect(state.gridSelection).toEqual([]);
    expect(state.activeId).toBe('b');
  });

  it('switching to a session that IS a tile keeps the grid', () => {
    ctrlClick('c');
    view.switchTo('c');
    expect(state.view).toBe('grid-select');
    expect(tiles()).toEqual(['a', 'c']);
  });

  it('⌘G leaves the ad-hoc grid; ⇧⌘G goes to the all-sessions grid', () => {
    ctrlClick('c');
    actions.toggleProjectGrid();
    expect(state.view).toBe('single');
    ctrlClick('c');
    actions.toggleAllGrid();
    expect(state.view).toBe('grid-all');
    expect(state.gridSelection).toEqual([]);
  });

  it('⌘G and ⇧⌘G keep their meaning with no selection', () => {
    actions.toggleProjectGrid();
    expect(state.view).toBe('grid-project');
    expect(tiles()).toEqual(['a', 'b']);
    actions.toggleAllGrid();
    expect(state.view).toBe('grid-all');
    expect(tiles()).toEqual(['a', 'b', 'c', 'd']);
  });
});

describe('sessions going away', () => {
  it('a removed tile drops out and the grid stays above two', () => {
    ctrlClick('c');
    ctrlClick('b');
    act_remove('b');
    expect(tiles()).toEqual(['a', 'c']);
    expect(state.view).toBe('grid-select');
  });

  it('removing down to one tile returns to single', () => {
    ctrlClick('c');
    act_remove('c');
    view.enforceViewFloor();
    expect(state.view).toBe('single');
  });

  it('a minimized tile leaves the grid', () => {
    ctrlClick('c');
    ctrlClick('b');
    view.minimizeSession('b');
    expect(tiles()).toEqual(['a', 'c']);
    expect(state.gridSelection).toEqual(['a', 'c']);
  });

  it('minimizing down to one tile returns to single without saving it over the saved view', () => {
    store.setView('grid-project');
    ctrlClick('c');
    view.minimizeSession('c');
    expect(state.view).toBe('single');
    expect(localStorage.getItem('hive.view')).toBe('grid-project');
  });
});

function act_remove(id: string) {
  store.removeSession(id);
}

describe('select-then-⌘G mode', () => {
  beforeEach(() => {
    state.gridSelectMode = 'manual';
  });

  it('⌘-click only marks rows', () => {
    ctrlClick('c');
    expect(state.view).toBe('single');
    expect(state.gridSelection).toEqual(['a', 'c']);
    expect(rowEl('c').hasAttribute('data-in-selection')).toBe(true);
  });

  it('⌘G then tiles exactly the marked sessions', () => {
    ctrlClick('c');
    ctrlClick('d');
    actions.toggleProjectGrid();
    expect(state.view).toBe('grid-select');
    expect(tiles()).toEqual(['a', 'c', 'd']);
  });

  it('⌘G with fewer than two marks is the ordinary project grid', () => {
    actions.toggleProjectGrid();
    expect(state.view).toBe('grid-project');
  });

  it('removing marks down to one never strands a one-tile grid', () => {
    ctrlClick('c');
    actions.toggleProjectGrid();
    ctrlClick('c'); // now only a is marked
    expect(state.view).toBe('single');
  });

  it('marks survive ⇧⌘G and a plain click clears them', () => {
    ctrlClick('c');
    actions.toggleAllGrid();
    expect(state.view).toBe('grid-all');
    expect(state.gridSelection).toEqual(['a', 'c']);
    click('b');
    expect(state.gridSelection).toEqual([]);
  });
});

describe('the toggle click', () => {
  it('does not start a drag: the row is prevented on a toggle click', () => {
    const ev = new MouseEvent('click', {
      bubbles: true,
      cancelable: true,
      ctrlKey: true,
    });
    rowEl('c').dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(true);
  });

  it('leaves a plain click alone', () => {
    const ev = new MouseEvent('click', { bubbles: true, cancelable: true });
    rowEl('c').dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(false);
  });
});
