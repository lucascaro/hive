// @vitest-environment jsdom
//
// The terminal tile's overlays (components/TileOverlays.tsx), which
// render through a portal into the `.tile-overlays` mount
// app/session-term.ts owns.
//
// The React half only. The imperative half — setDead() / setPhase() /
// revealAfterReplay() writing the store — is pinned by
// session-phase.test.ts against a real SessionTerm, so what is here is
// what a given store shape renders, plus the one behaviour that moved
// into React with the markup: the dead card's focus grab.
//
// Both overlays live in one file rather than two because they share this
// scaffold entirely and neither is large enough to earn a second copy of
// it.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';

import type { SessionInfo, TermTile } from '../../src/app/state.js';
import type { PhasePanel } from '../../src/lib/phase-steps.js';
import * as store from '../../src/store/store.js';
import { clearTerms, setTerm } from '../../src/store/terms.js';

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

const closeDead = vi.fn();
const dismissDead = vi.fn();

function stubTile(id: string): TermTile {
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
  return {
    host,
    header,
    overlays,
    attached: false,
    needsReattach: false,
    deadOverlayShown: false,
    phase: '',
    _closeDead: closeDead,
    _dismissDead: dismissDead,
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
}

function mount(chrome: Partial<store.TileChromeState> = {}) {
  store.resetStore();
  store.setProjects([{ id: 'p1', name: 'proj', color: '#4af' }]);
  store.setSessions([SESSION]);
  const tile = stubTile(SESSION.id);
  store.addTileChrome(SESSION.id, {
    ...store.initialTileChrome(''),
    ...chrome,
  });
  setTerm(SESSION.id, tile);
  const view = render(<TileChromeHost />);
  return { tile, view };
}

const dead = () =>
  document.querySelector<HTMLElement>('.term-host .dead-overlay');
const phase = () =>
  document.querySelector<HTMLElement>('.term-host .phase-overlay');

const patch = (p: Partial<store.TileChromeState>) =>
  act(() => store.patchTileChrome(SESSION.id, p));

// Let the deferred focus in DeadOverlay's effect run.
const tick = () => act(() => new Promise((r) => setTimeout(r, 0)));

beforeEach(() => {
  clearTerms();
  closeDead.mockClear();
  dismissDead.mockClear();
  document.getElementById('terms')?.replaceChildren();
});

describe('dead-session overlay', () => {
  it('mounts hidden, inside .tile-overlays, with the alertdialog contract', () => {
    const { tile } = mount();
    const el = dead();
    // ux-polish.spec.ts:375 selects exactly this pair.
    expect(el?.getAttribute('role')).toBe('alertdialog');
    expect(el?.getAttribute('aria-label')).toBe('Session ended');
    expect(el?.hidden).toBe(true);
    expect(el?.parentElement).toBe(tile.overlays);
  });

  it('shows the failure reason, and the default line without one', () => {
    mount();
    expect(dead()?.querySelector('.dead-subtitle')?.textContent).toBe(
      'The process running in this session has exited.',
    );

    patch({ dead: true, deadReason: 'exit status 127' });
    expect(dead()?.hidden).toBe(false);
    expect(dead()?.querySelector('.dead-subtitle')?.textContent).toBe(
      'exit status 127',
    );
  });

  it('closes and dismisses through the tile, not through React', () => {
    // The two handlers stay on SessionTerm: _closeDead kills the
    // session, _dismissDead records the dismissal and hands focus back.
    // keyboard.ts routes Enter/Escape to the same pair.
    mount({ dead: true });
    const btns = dead()?.querySelectorAll<HTMLButtonElement>('.dead-btn');
    expect([...(btns ?? [])].map((b) => b.className)).toEqual([
      'dead-btn primary',
      'dead-btn secondary',
    ]);
    fireEvent.click(btns?.[0] as HTMLButtonElement);
    expect(closeDead).toHaveBeenCalledOnce();
    fireEvent.click(btns?.[1] as HTMLButtonElement);
    expect(dismissDead).toHaveBeenCalledOnce();
  });

  it('takes focus when it appears', async () => {
    mount();
    patch({ dead: true });
    await tick();
    expect(document.activeElement).toBe(dead()?.querySelector('.dead-btn'));
  });

  it('does not steal focus while a modal is open', async () => {
    // A session can die at any moment — the daemon drives this, not the
    // user — and pulling focus out of a modal mid-keystroke drops what
    // was being typed. The launcher closes outright when focus leaves.
    mount();
    act(() => store.openModal({ id: 'settings' }));
    patch({ dead: true });
    await tick();
    expect(document.activeElement).not.toBe(dead()?.querySelector('.dead-btn'));
  });

  it('survives an unmount while shown', () => {
    // destroy() with the overlay up: the deferred focus must not fire
    // against a detached button.
    //
    // The assertion is the timer count, not a console spy: React nulls
    // closeRef on unmount, so a LEAKED timer would run `undefined?.focus()`
    // and neither throw nor log. Only "the timeout is gone" distinguishes
    // the effect's cleanup from no cleanup at all.
    vi.useFakeTimers();
    try {
      const { view } = mount({ dead: true });
      expect(vi.getTimerCount()).toBe(1);
      view.unmount();
      expect(vi.getTimerCount()).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('phase overlay', () => {
  const PANEL: PhasePanel = {
    status: 'Creating worktree feature-x…',
    steps: [
      { label: 'Registered session', state: 'done' },
      { label: 'Creating worktree feature-x', state: 'active' },
      { label: 'Starting shell', state: 'todo' },
    ],
  };

  const steps = () =>
    [...(phase()?.querySelectorAll('.phase-step') ?? [])].map((li) => [
      (li as HTMLElement).dataset.state,
      li.querySelector('span')?.textContent,
      li.querySelector('use')?.getAttribute('href') ?? null,
    ]);

  it('mounts hidden, inside .tile-overlays, with the status contract', () => {
    const { tile } = mount();
    // session-lifecycle.spec.ts:37 selects .phase-overlay under the host.
    expect(phase()?.getAttribute('role')).toBe('status');
    expect(phase()?.getAttribute('aria-live')).toBe('polite');
    expect(phase()?.hidden).toBe(true);
    expect(phase()?.parentElement).toBe(tile.overlays);
  });

  it('renders the panel steps with a mark per state', () => {
    mount({ phaseVisible: true, phasePanel: PANEL });
    expect(phase()?.hidden).toBe(false);
    expect(phase()?.querySelector('.phase-status')?.textContent).toBe(
      'Creating worktree feature-x…',
    );
    // 'done' gets a check, 'active' the starting state icon, 'todo' no
    // mark at all — the indent in phase-step::before holds the column.
    expect(steps()).toEqual([
      ['done', 'Registered session', '#hv-check'],
      ['active', 'Creating worktree feature-x', '#hv-state-starting'],
      ['todo', 'Starting shell', null],
    ]);
  });

  it('keeps the last panel behind hidden when it is dropped', () => {
    // _hidePhaseOverlay() clears phaseVisible and leaves the model, so
    // the reveal is one attribute flip rather than a rebuild — and the
    // panel is held past PhaseReady, where phasePanel() itself is null.
    mount({ phaseVisible: true, phasePanel: PANEL });
    patch({ phaseVisible: false });
    expect(phase()?.hidden).toBe(true);
    expect(steps()).toHaveLength(3);
  });
});
