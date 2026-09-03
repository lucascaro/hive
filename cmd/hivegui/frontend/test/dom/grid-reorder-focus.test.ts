// @vitest-environment jsdom
//
// renderGrid must not blur the focused tile (src/app/view.ts +
// src/lib/preserve-focus.ts).
//
// The regression: renderGrid ended its per-tile loop with
// `termsHost.appendChild(st.host)` for EVERY tile, unconditionally.
// appendChild on an already-attached node is a remove+insert, and the
// browser blurs whatever is focused inside it — which is why focus.ts
// carries a 500ms _focusGuard for the view-switch case. But renderGrid
// runs on every repaint, and most of them reorder nothing: killing a
// NON-active session leaves the survivors exactly where they were. The
// guard is armed only by setFocusedTile, which that path never calls, so
// the blur went unrepaired and keyboard focus was silently lost.
// That is the mock E2E focus-invariants F2 failure (spec 257).
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
// The layout pass itself, called directly: this file is a unit test of
// preserveFocus inside it, and mounting GridView would only add a
// subscription between the store write and the same call.
let applyGridLayout: typeof import('../../src/app/grid-layout.js').applyGridLayout;

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
    <div id="app"><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    <div id="terms"></div><div id="minimized-tray"></div>
    <div id="empty-state"></div></div>`;
  ({ state } = await import('../../src/app/state.js'));
  view = await import('../../src/app/view.js');
  ({ applyGridLayout } = await import('../../src/app/grid-layout.js'));
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

describe('grid layout tile reordering', () => {
  // state.terms is re-seeded per test but #terms keeps every host a
  // previous test appended, so clear it or the order assertions below
  // compare against stale tiles.
  beforeEach(() => {
    const terms = document.getElementById('terms');
    if (terms) terms.innerHTML = '';
  });

  // A focusable stand-in for the xterm helper textarea: the thing that
  // actually holds the keyboard inside a tile.
  function focusableIn(t: TermTile): HTMLTextAreaElement {
    const ta = document.createElement('textarea');
    ta.className = 'xterm-helper-textarea';
    t.host.append(ta);
    return ta;
  }

  function hostsInDom(): HTMLElement[] {
    const terms = document.getElementById('terms');
    return Array.from(terms?.children ?? []) as HTMLElement[];
  }

  function gridHosts(): HTMLElement[] {
    return hostsInDom().filter((h) => h.classList.contains('in-grid'));
  }

  it('does not touch the DOM when the tile order already matches', () => {
    view.setView('grid-project');
    applyGridLayout(); // setView writes the store; GridView, not mounted
    // here, is what repaints in production.
    const before = hostsInDom();
    const ta = focusableIn(tile('a'));
    ta.focus();
    expect(document.activeElement).toBe(ta);

    // Comparing the child list before/after cannot see the bug:
    // re-appending every node in the order it already had leaves an
    // identical list. The MOVE is the problem — it is what blurs the
    // focused textarea — and a move is only observable as a mutation.
    const terms = document.getElementById('terms');
    if (!terms) throw new Error('no #terms');
    const seen = new MutationObserver(() => {});
    seen.observe(terms, { childList: true });

    applyGridLayout();

    expect(seen.takeRecords()).toEqual([]);
    seen.disconnect();
    expect(hostsInDom()).toEqual(before);
    expect(document.activeElement).toBe(ta);
  });

  it('keeps focus on the active tile when a non-active session is killed', () => {
    // F2, in miniature: three tiles, focus on the first, the third dies.
    // The survivors' relative order is unchanged, so nothing should move.
    state.sessions = [
      { id: 'a', project_id: 'p1', order: 0 },
      { id: 'b', project_id: 'p1', order: 1 },
      { id: 'c', project_id: 'p1', order: 2 },
    ];
    state.terms = new Map(state.sessions.map((s) => [s.id, fakeTerm()]));
    view.setView('grid-project');
    applyGridLayout(); // setView writes the store; GridView, not mounted
    // here, is what repaints in production.
    const ta = focusableIn(tile('a'));
    ta.focus();

    state.sessions = state.sessions.filter((s) => s.id !== 'c');
    applyGridLayout();

    expect(document.activeElement).toBe(ta);
  });

  it('restores focus after a reorder that really does move the tile', () => {
    view.setView('grid-project');
    applyGridLayout(); // setView writes the store; GridView, not mounted
    // here, is what repaints in production.
    const ta = focusableIn(tile('a'));
    ta.focus();

    // Swap the nav order: now the tiles genuinely have to move, so the
    // focused node IS re-parented. preserveFocus has to put it back.
    state.sessions = [
      { id: 'b', project_id: 'p1', order: 0 },
      { id: 'a', project_id: 'p1', order: 1 },
    ];
    applyGridLayout();

    expect(gridHosts()).toEqual([tile('b').host, tile('a').host]);
    expect(document.activeElement).toBe(ta);
  });
});
