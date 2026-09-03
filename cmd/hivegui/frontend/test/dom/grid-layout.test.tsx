// @vitest-environment jsdom
//
// The grid shell: app/grid-layout.ts (what a layout pass does) and
// components/GridView.tsx (when one happens).
//
// The three assertions on the pass are the ones that cost a shipped bug
// when they were violated: the grid template has to be set BEFORE any
// tile attaches, a tile has to be reparented rather than recreated, and
// a tile that leaves the scope has to keep its DOM node. The assertions
// on GridView are the other half of invariant 2 — a pass calls
// ensureAttached() on every in-grid tile, so a pass that today's code
// does not run must not start running now that a subscription drives it.
import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  beforeEach,
  afterEach,
} from 'vitest';
import { render, cleanup, act } from '@testing-library/react';
import type { TermTile } from '../../src/app/state.js';

vi.mock('../../src/bridge.js', () => ({
  LogFrontend: vi.fn(() => Promise.resolve()),
  WindowSetTitle: vi.fn(),
  EventsOn: vi.fn(),
}));

let state: typeof import('../../src/app/state.js').state;
let grid: typeof import('../../src/app/grid-layout.js');
let GridView: typeof import('../../src/components/GridView.js').GridView;

// Every ensureAttached() call records the grid template as it stood at
// that moment, which is what makes "template before attach" observable
// as a sequence rather than an end state.
const attachedWithTemplate: string[] = [];

function fakeTerm(): TermTile {
  const host = document.createElement('div');
  return {
    host,
    attached: true,
    needsReattach: false,
    deadOverlayShown: false,
    phase: '',
    setPhase() {},
    revealAfterReplay() {},
    ensureAttached() {
      const terms = document.getElementById('terms');
      attachedWithTemplate.push(terms?.style.gridTemplateColumns ?? '');
    },
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
  } as unknown as TermTile;
}

function termsHost(): HTMLElement {
  const el = document.getElementById('terms');
  if (!el) throw new Error('no #terms');
  return el;
}

function hostsInGrid(): HTMLElement[] {
  return Array.from(termsHost().children).filter((c) =>
    c.classList.contains('in-grid'),
  ) as HTMLElement[];
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
    <div id="empty-state"></div><div id="grid-root" hidden></div></div>`;
  ({ state } = await import('../../src/app/state.js'));
  grid = await import('../../src/app/grid-layout.js');
  ({ GridView } = await import('../../src/components/GridView.js'));
  grid.initGridLayout({
    // The real ensureTerm builds an xterm; here the tile is pre-seeded
    // and this only hands it back, which is also what makes "reparent,
    // never recreate" checkable by node identity.
    ensureTerm: (info) => {
      const t = state.terms.get(info.id);
      if (!t) throw new Error(`no term stub for ${info.id}`);
      return t;
    },
    scrollTrace: {
      rec: Object.assign(() => {}, { enabled: false }),
      count: () => {},
    } as never,
  });
});

beforeEach(() => {
  // jsdom has no requestIdleCallback, so grid-layout's _ric takes its
  // setTimeout(16) fallback and every pass leaves a real timer behind for
  // the deferred (non-active) tiles. Under real timers those stragglers
  // outlive the test — and, for the last test in the file, the jsdom
  // environment itself — firing ensureAttached() against a torn-down
  // `document`. Fake timers keep the chain inert unless a test advances
  // them on purpose; afterEach discards whatever is still queued.
  vi.useFakeTimers();
  attachedWithTemplate.length = 0;
  termsHost().innerHTML = '';
  termsHost().removeAttribute('style');
  state.projects = [{ id: 'p1' }];
  state.sessions = [
    { id: 'a', project_id: 'p1', order: 0 },
    { id: 'b', project_id: 'p1', order: 1 },
  ];
  state.terms = new Map(state.sessions.map((s) => [s.id, fakeTerm()]));
  state.activeId = 'a';
  state.currentProjectId = 'p1';
  state.view = 'grid-project';
  state.gridProjectId = 'p1';
  state.minimized = new Set();
  state.attention = new Set();
});

afterEach(() => {
  cleanup();
  vi.clearAllTimers();
  vi.useRealTimers();
});

describe('a layout pass', () => {
  it('sets the grid template before any tile attaches', () => {
    grid.applyGridLayout();

    // Not just "the template is set afterwards": the ACTIVE tile attaches
    // synchronously inside the pass, and if it measures its body box
    // before the template lays it out, the stale width arms a second
    // scrollback replay on top of the attach replay — the double-restream
    // that visibly jumps the tile on grid entry.
    expect(attachedWithTemplate.length).toBeGreaterThan(0);
    for (const template of attachedWithTemplate) {
      expect(template).toMatch(/^repeat\(\d+, 1fr\)$/);
    }
  });

  it('reparents tiles instead of recreating them', () => {
    grid.applyGridLayout();
    const first = hostsInGrid();
    expect(first).toHaveLength(2);

    // A reorder: the nodes genuinely move, and they must be the SAME
    // nodes — a recreated host would drop the xterm, its WebGL slot and
    // its live PTY attachment on the floor.
    state.sessions = [
      { id: 'b', project_id: 'p1', order: 0 },
      { id: 'a', project_id: 'p1', order: 1 },
    ];
    grid.applyGridLayout();

    // toBe, not toEqual: vitest compares DOM nodes structurally, so two
    // freshly built hosts carrying the same classes pass toEqual. Only
    // Object.is can tell "reparented" from "recreated", which is the
    // whole invariant this case exists to pin.
    const after = hostsInGrid();
    expect(after[0]).toBe(first[1]);
    expect(after[1]).toBe(first[0]);
  });

  it('leaves an out-of-scope tile its DOM node', () => {
    grid.applyGridLayout();
    const bHost = state.terms.get('b')?.host;

    state.minimized = new Set(['b']);
    grid.applyGridLayout();

    const inGrid = hostsInGrid();
    expect(inGrid).toHaveLength(1);
    expect(inGrid[0]).toBe(state.terms.get('a')?.host);
    // Still the same node, still in the document, just not in the grid:
    // its scrollback has to survive being minimized and restored.
    expect(state.terms.get('b')?.host).toBe(bHost);
    expect(bHost?.isConnected).toBe(true);
    expect(bHost?.classList.contains('in-grid')).toBe(false);
  });
});

describe('GridView', () => {
  it('renders no DOM of its own', () => {
    const { container } = render(<GridView />);
    expect(container.innerHTML).toBe('');
  });

  it('lays the grid out when the scope changes', () => {
    render(<GridView />);
    expect(hostsInGrid()).toHaveLength(2);

    act(() => {
      state.minimized = new Set(['b']);
    });

    expect(hostsInGrid()).toHaveLength(1);
  });

  it('lays the grid out when the scope reorders', () => {
    render(<GridView />);
    const first = hostsInGrid();
    expect(first).toHaveLength(2);

    // events.ts no longer calls a repaint on an `updated` reorder — the
    // ordered ids in GridView's signature are the repaint. This is the
    // subscription half of it: a signature that stopped carrying order
    // (sorted, set-ified) would leave the tiles in stale DOM order and
    // every other case in this file would stay green.
    act(() => {
      state.sessions = [
        { id: 'b', project_id: 'p1', order: 0 },
        { id: 'a', project_id: 'p1', order: 1 },
      ];
    });

    const after = hostsInGrid();
    expect(after[0]).toBe(first[1]);
    expect(after[1]).toBe(first[0]);
  });

  it('does not lay out again when a bell marks attention', () => {
    render(<GridView />);
    const passes = attachedWithTemplate.length;

    act(() => {
      state.attention = new Set(['b']);
    });

    // A bell never called renderGrid(): events.ts patches the class onto
    // the host. A pass here would call ensureAttached() on every in-grid
    // tile, which re-latches follow-bottom — a background tile parked in
    // history would jump to the bottom at bell rate.
    expect(attachedWithTemplate.length).toBe(passes);
  });

  it('does not lay out again when a session is renamed', () => {
    render(<GridView />);
    const passes = attachedWithTemplate.length;

    act(() => {
      state.sessions = state.sessions.map((s) =>
        s.id === 'b' ? { ...s, name: 'renamed' } : s,
      );
    });

    // `updated` events replace the sessions array at the child program's
    // redraw rate. Only the ones that move the grid's own scope — an
    // add, a removal, a reorder — are a repaint.
    expect(attachedWithTemplate.length).toBe(passes);
  });

  it('swaps to the single-tile layout when the view changes', () => {
    render(<GridView />);
    expect(termsHost().classList.contains('grid')).toBe(true);

    act(() => {
      state.view = 'single';
    });

    expect(termsHost().classList.contains('single')).toBe(true);
    expect(termsHost().classList.contains('grid')).toBe(false);
  });
});
