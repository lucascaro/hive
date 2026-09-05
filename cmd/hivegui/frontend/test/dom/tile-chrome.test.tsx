// @vitest-environment jsdom
//
// The terminal tile's header (components/TileChrome.tsx), which renders
// through a portal into a host app/session-term.ts owns.
//
// What these pin, and why each one is here rather than left to the e2e
// suite: the header's DOM contract is selected by class from eight
// Playwright specs (chrome, theme, minimize, worktrees, silent-failures,
// grid-scroll-regressions, ux-polish, session-lifecycle), so a change in
// child order or a dropped class fails far from its cause. These are the
// close-range versions of the same assertions, plus the two invariants
// the port introduced: the header's box exists before React fills it,
// and a bell repaints one tile without touching the attach path.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';

import type { SessionInfo, TermTile } from '../../src/app/state.js';
import * as store from '../../src/store/store.js';
import { clearTerms, setTerm } from '../../src/store/terms.js';

const UpdateSession = vi.fn(
  (_id: string, _name: string, _color: string, _order: number) =>
    Promise.resolve(),
);

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
    UpdateSession: (id: string, name: string, color: string, order: number) =>
      UpdateSession(id, name, color, order),
    ListAgents: fn(),
    ListCustomAgents: fn(),
    SaveCustomAgents: fn(),
    CreateProject: fn(),
    KillProject: fn(),
    UpdateProject: fn(),
    ListWorktrees: fn(),
    CreateWorktree: fn(),
    RemoveWorktree: fn(),
    SetClipboardText: fn(),
    ClipboardGetText: fn(),
    OpenURL: fn(),
    LogFrontend: fn(),
    Version: fn(),
    CheckForUpdates: fn(),
    ApplyUpdate: fn(),
    RestartDaemon: fn(),
  };
});

// app/dom.ts resolves #terms / #status at import time, so the markup has
// to exist before the component is imported.
const SCAFFOLD = `
  <div id="app">
    <ul id="projects"></ul>
    <div id="minimized-projects" class="hidden" role="toolbar"></div>
    <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    <div id="terms"></div><div id="minimized-tray"></div>
    <div id="empty-state"></div>
  </div>`;

let TileChromeHost: typeof import('../../src/components/TileChrome.js').TileChromeHost;

beforeAll(async () => {
  document.body.innerHTML = SCAFFOLD;
  // app/grid-layout.ts (reached through app/view.ts) constructs a
  // ResizeObserver at module load; jsdom has none. Same no-op stub the
  // other tile suites install.
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  ({ TileChromeHost } = await import('../../src/components/TileChrome.js'));
});

const SESSION: SessionInfo = {
  id: 's1',
  name: 'alpha',
  agent: 'claude',
  project_id: 'p1',
  alive: true,
  phase: '',
};

// A stub tile: the two mount points and nothing else. TermTile's other
// members are optional precisely so the dom tests can do this — no
// xterm, no WebGL slot, no PTY.
function stubTile(id: string): { tile: TermTile; host: HTMLDivElement } {
  const host = document.createElement('div');
  host.className = 'term-host';
  host.dataset.sid = id;
  const header = document.createElement('div');
  header.className = 'tile-header';
  const body = document.createElement('div');
  body.className = 'term-body';
  const overlays = document.createElement('div');
  overlays.className = 'tile-overlays';
  host.append(header, body, overlays);
  document.getElementById('terms')?.appendChild(host);
  const tile = {
    host,
    header,
    overlays,
    attached: false,
    needsReattach: false,
    deadOverlayShown: false,
    phase: '',
    _closeDead: () => {},
    _dismissDead: () => {},
    show: () => {},
    hide: () => {},
    ensureAttached: () => {
      throw new Error('ensureAttached must not be reached from a render');
    },
    rebaselineReplayCols: () => {},
    _onBodyResize: () => {},
    setInfo: () => {},
    setProject: () => {},
    setDead: () => {},
    setPhase: () => {},
    revealAfterReplay: () => {},
    writeData: () => {},
    destroy: () => {},
  } as unknown as TermTile;
  return { tile, host };
}

function mount(info: Partial<SessionInfo> = {}, phase = '') {
  const session = { ...SESSION, ...info };
  store.resetStore();
  store.setProjects([{ id: 'p1', name: 'proj', color: '#4af' }]);
  store.setSessions([session]);
  const { tile, host } = stubTile(session.id);
  store.addTileChrome(session.id, store.initialTileChrome(phase));
  setTerm(session.id, tile);
  render(<TileChromeHost />);
  return { session, tile, host };
}

const header = () =>
  document.querySelector<HTMLElement>('.term-host .tile-header');

beforeEach(() => {
  clearTerms();
  UpdateSession.mockClear();
  document.getElementById('terms')?.replaceChildren();
});

describe('tile header', () => {
  it('renders header children in contract order', () => {
    mount({ worktree_branch: 'feat/x' });
    const kids = [...(header()?.children ?? [])].map((el) =>
      el.getAttribute('class'),
    );
    expect(kids).toEqual([
      'hv-icon hv-state-icon',
      'tile-name',
      'hv-icon-btn tile-worktree',
      'tile-term-title',
      'tile-project',
      'tile-actions',
    ]);
    // The minimize button is inside .tile-actions; the worktree marker
    // deliberately is not — worktree-ness is a fact, not an action.
    expect(
      header()?.querySelector('.tile-actions > .tile-minimize'),
    ).toBeTruthy();
    expect(header()?.querySelector('.tile-actions .tile-worktree')).toBeNull();
  });

  it('renders the session name, project and state icon', () => {
    mount();
    expect(header()?.querySelector('.tile-name')?.textContent).toBe('alpha');
    expect(header()?.querySelector('.tile-project')?.textContent).toBe('proj');
    expect(
      header()?.querySelector('.hv-state-icon')?.getAttribute('data-state'),
    ).toBe('running');
  });

  // The regression this replaces: the header rendered from a snapshot
  // SessionTerm published, refreshed only when the layout ran
  // ensureTerm(). A SESSION_EVENT(state) repainted the sidebar row and
  // left the tile showing the previous glyph, so the same session
  // disagreed with itself depending on where you looked.
  it('follows the session list, so it cannot disagree with the sidebar', () => {
    const { session } = mount();
    const icon = () => header()?.querySelector('.hv-state-icon');
    expect(icon()?.getAttribute('data-state')).toBe('running');

    act(() => {
      store.updateSession({ ...session, state: 'working' });
    });
    expect(icon()?.getAttribute('data-state')).toBe('working');

    act(() => {
      store.updateSession({ ...session, name: 'renamed' });
    });
    expect(header()?.querySelector('.tile-name')?.textContent).toBe('renamed');
  });

  it('hides the worktree marker without a branch', () => {
    const { session } = mount();
    const marker = () =>
      header()?.querySelector<HTMLButtonElement>('.tile-worktree');
    expect(marker()?.hidden).toBe(true);

    act(() => {
      store.updateSession({ ...session, worktree_branch: 'feat/x' });
    });
    expect(marker()?.hidden).toBe(false);
    expect(marker()?.title).toBe(
      'Worktree: feat/x — click to manage worktrees',
    );
  });

  it('hides the term title until one arrives', () => {
    // The separator lives in .tile-term-title::before, so an
    // empty-but-visible span renders a lone '·' next to the name.
    const { session } = mount();
    const span = () =>
      header()?.querySelector<HTMLSpanElement>('.tile-term-title');
    expect(span()?.hidden).toBe(true);

    act(() => {
      store.updateSession({ ...session, title: 'vim README.md' });
    });
    expect(span()?.hidden).toBe(false);
    expect(span()?.textContent).toBe('vim README.md');

    // lib/term-title.ts's rule: a title that just echoes the session
    // name reports nothing, so the span goes away again.
    act(() => {
      store.updateSession({ ...session, title: 'alpha' });
    });
    expect(span()?.hidden).toBe(true);
  });

  it('follows the live phase, not the phase on the payload', () => {
    // The other half of session-phase.test.ts's pair: setPhase publishes
    // the tile's own phase because it never writes back to info, and the
    // icon has to resolve from that override or it shows the stale
    // answer for exactly the transition setPhase exists to signal.
    const { session } = mount({ phase: 'starting' }, 'starting');
    const state = () =>
      header()?.querySelector('.hv-state-icon')?.getAttribute('data-state');
    expect(state()).toBe('starting');

    act(() => {
      // PHASE.ready is the empty string. The payload still says
      // 'starting' — only the tile knows better.
      store.patchTileChrome(session.id, { phase: '' });
    });
    expect(session.phase).toBe('starting');
    expect(state()).toBe('running');
  });

  it('repaints one tile on a bell, and reaches no attach path', () => {
    // needs_attention lives on the session itself, read off the same
    // `info` selector the rest of the header uses — there is no second
    // `attention` subscription left to keep narrow. GridView.tsx never
    // calls a layout pass for it either way — the stub's ensureAttached
    // throws, so a regression that routes a bell into one fails here.
    const { session } = mount();
    const state = () =>
      header()?.querySelector('.hv-state-icon')?.getAttribute('data-state');
    expect(state()).toBe('running');

    act(() => {
      store.updateSession({ ...session, needs_attention: true });
    });
    expect(state()).toBe('attention');

    act(() => {
      // Unknown id: updateSession no-ops rather than touching the
      // sessions array, so this session's tile has nothing to repaint
      // from anyway.
      store.updateSession({ id: 'someone-else', needs_attention: true });
    });
    expect(state()).toBe('attention');
  });

  it('leaves the header box measurable before React fills it', () => {
    // The reason the header element is created by SessionTerm and only
    // its CHILDREN are portalled: tile-header.css pins it to 28px, so an
    // empty header keeps .term-body's box byte-identical on the frame
    // before React renders. If the element itself moved into React the
    // body would be 28px taller for one frame and first-attach fit()
    // would measure the wrong rows.
    const { tile } = stubTile('s-empty');
    expect(tile.header?.className).toBe('tile-header');
    expect(tile.header?.parentElement).toBe(tile.host);
    expect(tile.host.children[0]).toBe(tile.header);
    expect(tile.host.children[1]).toBe(tile.host.querySelector('.term-body'));
    // The overlays mount goes last so paint order is unchanged.
    expect(tile.host.children[2]).toBe(tile.overlays);
  });

  it('renders nothing for a term with no chrome state', () => {
    const { tile } = stubTile('s-bare');
    setTerm('s-bare', tile);
    render(<TileChromeHost />);
    expect(tile.header?.children.length).toBe(0);
  });
});

// The tile-name rename. Ported to React by reusing app/inline-rename.ts
// exactly as components/Sidebar.tsx already does — the control flow
// (focus, commit on Enter or blur, cancel on Escape, restore the label)
// stays in the one module all four rename surfaces share, so this pins
// the wiring, not a second copy of the dance.
describe('tile rename', () => {
  const name = () =>
    document.querySelector<HTMLSpanElement>('.term-host .tile-name');
  const input = () =>
    document.querySelector<HTMLInputElement>(
      '.term-host input.tile-name-input',
    );

  it('opens an editor on double-click and commits on enter', () => {
    mount();
    expect(input()).toBeNull();

    const span = name();
    if (!span) throw new Error('no .tile-name');
    fireEvent.doubleClick(span);
    const el = input();
    expect(el).toBeTruthy();
    // The class is not cosmetic: test/unit/focus.test.ts snapshots it as
    // the marker that keyboard focus is inside an editor, and
    // silent-failures.spec.ts selects it by that name.
    expect(el?.value).toBe('alpha');
    expect(name()).toBeNull();

    if (!el) throw new Error('no rename input');
    el.value = 'renamed';
    fireEvent.keyDown(el, { key: 'Enter' });
    expect(UpdateSession).toHaveBeenCalledWith('s1', 'renamed', '', -1);
    // The span is put back; the next session:event(updated) refreshes
    // its text through setInfo.
    expect(name()).toBeTruthy();
    expect(input()).toBeNull();
  });

  it('cancels on escape without calling the daemon', () => {
    mount();
    const span = name();
    if (!span) throw new Error('no .tile-name');
    fireEvent.doubleClick(span);
    const el = input();
    if (!el) throw new Error('no rename input');
    el.value = 'discarded';
    fireEvent.keyDown(el, { key: 'Escape' });
    expect(UpdateSession).not.toHaveBeenCalled();
    expect(name()).toBeTruthy();
    expect(input()).toBeNull();
  });

  it('does not commit an unchanged name', () => {
    mount();
    const span = name();
    if (!span) throw new Error('no .tile-name');
    fireEvent.doubleClick(span);
    const el = input();
    if (!el) throw new Error('no rename input');
    fireEvent.keyDown(el, { key: 'Enter' });
    expect(UpdateSession).not.toHaveBeenCalled();
  });
});
