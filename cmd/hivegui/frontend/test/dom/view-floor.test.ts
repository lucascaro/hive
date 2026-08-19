// @vitest-environment jsdom
//
// The two-tile floor on grid views, against the REAL view.ts.
//
// A grid holding one tile looks exactly like focused mode but loses the
// focused-mode keybindings, so ⌘G / ⇧⌘G / ⌘↩ must be no-ops below the
// floor — and a live grid that shrinks to one tile (session killed, or
// minimized away) must fall back on its own. The sibling
// keyboard-arrows.test.ts mocks view.ts, so it cannot see any of this.
import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  beforeEach,
  afterEach,
} from 'vitest';
import { createScrollTrace } from '../../src/lib/scroll-debug.js';
import type { TermTile } from '../../src/app/state.js';

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
    DuplicateSession: fn(),
    KillSession: fn(),
    RestartSession: fn(),
    UpdateSession: fn(),
    ListAgents: fn(),
    CreateProject: fn(),
    KillProject: fn(),
    UpdateProject: fn(),
    LaunchDir: fn(),
    PickDirectory: fn(),
    OpenNewWindow: fn(),
    CloseWindow: fn(),
    IsGitRepo: fn(),
    OpenURL: fn(),
    OpenTerminalAt: fn(),
    Notify: fn(),
    Confirm: fn(),
    RestartDaemon: fn(),
    CheckForUpdate: fn(),
    SetClipboardText: fn(),
    EventsOn: vi.fn(),
    WindowSetTitle: vi.fn(),
    ClipboardGetText: fn(),
  };
});

let state: typeof import('../../src/app/state.js').state;
let view: typeof import('../../src/app/view.js');

function fakeTerm(): TermTile {
  return {
    host: document.createElement('div'),
    attached: true,
    needsReattach: false,
    deadOverlayShown: false,
    phase: '',
    setPhase() {},
    revealAfterReplay() {},
    ensureAttached() {},
    show() {},
    hide() {},
    rebaselineReplayCols() {},
    _onBodyResize() {},
    setInfo() {},
    setProject() {},
    setDead() {},
    writeData() {},
    destroy() {},
    _closeDead() {},
    _dismissDead() {},
  };
}

function tile(id: string): TermTile {
  const t = state.terms.get(id);
  expect(t, `no term stub for ${id}`).toBeDefined();
  return t as TermTile;
}

beforeAll(async () => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  document.body.innerHTML = `
    <div id="app"><ul id="projects"></ul><div id="status"></div>
    <div id="terms"></div><div id="minimized-tray"></div>
    <div id="empty-state"></div></div>`;
  ({ state } = await import('../../src/app/state.js'));
  view = await import('../../src/app/view.js');
  const { setActive } = await import('../../src/app/focus.js');
  view.initView({
    ensureTerm: (info) => tile(info.id),
    setActive,
    focusActiveTerm: () => {},
    scrollTrace: createScrollTrace({ enabled: false }),
  });
});

beforeEach(() => {
  vi.useFakeTimers(); // setView arms a 250ms post-switch snap
  // p1 has two sessions, p2 has one.
  state.projects = [{ id: 'p1' }, { id: 'p2' }];
  state.sessions = [
    { id: 'a', project_id: 'p1', order: 0 },
    { id: 'b', project_id: 'p1', order: 1 },
    { id: 'z', project_id: 'p2', order: 2 },
  ];
  state.terms = new Map(state.sessions.map((s) => [s.id, fakeTerm()]));
  state.activeId = 'a';
  state.currentProjectId = 'p1';
  state.view = 'single';
  state.gridProjectId = null;
  state.minimized = new Set();
  state.attention = new Set();
});

// setView's snap timer would otherwise fire against a torn-down jsdom.
afterEach(() => {
  vi.runOnlyPendingTimers();
  vi.useRealTimers();
});

describe('setView floor', () => {
  it('stays focused when the project has a single session (⌘G)', () => {
    state.activeId = 'z';
    state.currentProjectId = 'p2';
    view.setView('grid-project');
    expect(state.view).toBe('single');
  });

  it('stays focused when there is a single session overall (⇧⌘G)', () => {
    state.sessions = [state.sessions[0]];
    view.setView('grid-all');
    expect(state.view).toBe('single');
  });

  it('enters grid-project with two sessions in the project', () => {
    view.setView('grid-project');
    expect(state.view).toBe('grid-project');
  });

  it('enters grid-all with two sessions overall', () => {
    view.setView('grid-all');
    expect(state.view).toBe('grid-all');
  });

  it('does not count minimized sessions toward the floor', () => {
    state.minimized = new Set(['b']);
    view.setView('grid-project');
    expect(state.view).toBe('single');
  });
});

describe('enforceViewFloor', () => {
  it('leaves a grid with two or more tiles alone', () => {
    view.setView('grid-project');
    view.enforceViewFloor();
    expect(state.view).toBe('grid-project');
  });

  it('exits grid mode when a session is removed down to one', () => {
    view.setView('grid-all');
    expect(state.view).toBe('grid-all');
    // What events.ts's `removed` branch does before calling the floor.
    state.sessions = state.sessions.slice(0, 1);
    view.enforceViewFloor();
    expect(state.view).toBe('single');
  });

  it('exits grid mode when a session is minimized down to one', () => {
    state.sessions = state.sessions.slice(0, 2); // just p1's pair
    view.setView('grid-all');
    expect(state.view).toBe('grid-all');
    view.minimizeSession('b');
    expect(state.view).toBe('single');
  });

  it('is a no-op in focused mode', () => {
    view.enforceViewFloor();
    expect(state.view).toBe('single');
  });
});
