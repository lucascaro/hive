// @vitest-environment jsdom
//
// The loading panel and the attach gate (src/app/session-term.ts).
//
// Two bugs this pins down. First: SESSION_EVENT(added) now arrives
// before the session's PTY exists, so a tile that attaches on sight
// gets refused by the daemon and used to paint
// `[attach failed: …]` in red into the pane the user was waiting on.
// Second: a killed session's tile lives on until `removed` lands
// (seconds, on a big worktree), and every render/focus/resize in that
// window re-dialled and painted the same red error.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import * as store from '../../src/store/store.js';
import { setTerm, clearTerms } from '../../src/store/terms.js';

const OpenSession = vi.fn((_id: string, _cols: number, _rows: number) =>
  Promise.resolve({}),
);

vi.mock('../../src/bridge.js', () => {
  const fn = () => vi.fn(() => Promise.resolve());
  return {
    ConnectControl: fn(),
    OpenSession: (id: string, cols: number, rows: number) =>
      OpenSession(id, cols, rows),
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
    LogFrontend: vi.fn(),
    EventsOn: vi.fn(),
    WindowSetTitle: vi.fn(),
    ClipboardGetText: fn(),
  };
});

type SessionTermClass =
  typeof import('../../src/app/session-term.js').SessionTerm;
type Tile = InstanceType<SessionTermClass>;
type Info = import('../../src/app/state.js').SessionInfo;

let SessionTerm: SessionTermClass;

beforeAll(async () => {
  // view.ts (pulled in via session-term) installs a container
  // ResizeObserver at module load; jsdom has none. A no-op stub is
  // enough — the grid-reflow path is not what this file tests.
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  // xterm's DPR watcher (lib/renderer-recovery.ts) needs matchMedia,
  // which jsdom doesn't implement.
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    onchange: null,
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  ({ SessionTerm } = await import('../../src/app/session-term.js'));
});

beforeEach(() => {
  OpenSession.mockClear();
  clearTerms();
  // resetStore FIRST: SessionTerm's constructor now seeds the tileChrome
  // slice, and without a reset a test that reuses a session id would
  // read the previous test's phase and info. Today every test here picks
  // a distinct id, so this is pinning the isolation rather than fixing a
  // live bug — which is exactly when it is cheap to add.
  store.resetStore();
  store.setAliveById(new Map());
});

// jsdom gives every element a 0×0 box, which ensureAttached treats as
// "not laid out yet". Force a real size so the attach path is reached.
function withBox(st: Tile) {
  Object.defineProperty(st.body, 'clientWidth', { value: 400 });
  Object.defineProperty(st.body, 'clientHeight', { value: 300 });
  st.fit.fit = () => {};
  return st;
}

function makeTerm(info: Info) {
  const st = withBox(new SessionTerm(info));
  setTerm(info.id, st);
  return st;
}

// The panel is a model in the store now, not DOM: components/
// TileOverlays.tsx renders it, and tile-overlays.test.tsx pins that
// half. What this file owns is the imperative half — which phase edge
// raises the panel, which drops it, and the attach gate around them.
const chrome = (st: Tile) => {
  const c = store.appStore.getState().tileChrome.get(st.info.id);
  if (!c) throw new Error(`no tile chrome for ${st.info.id}`);
  return c;
};

const steps = (st: Tile) =>
  (chrome(st).phasePanel?.steps ?? []).map((s) => `${s.state}:${s.label}`);

describe('phase overlay', () => {
  it('shows the checklist while the session is still being created', () => {
    const st = makeTerm({
      id: 's1',
      name: 'wt claude',
      agent: 'claude',
      worktree_branch: 'stone-valley',
      phase: 'starting',
    });
    st.setPhase('starting');

    expect(chrome(st).phaseVisible).toBe(true);
    expect(steps(st)).toEqual([
      'active:Registered session',
      'todo:Fetching origin',
      'todo:Creating worktree stone-valley',
      'todo:Starting claude',
    ]);

    st.setPhase('worktree');
    expect(chrome(st).phasePanel?.status).toBe(
      'Creating worktree stone-valley…',
    );
  });

  it('does not attach while pending, and attaches on the ready edge', async () => {
    const st = makeTerm({ id: 's2', phase: 'starting' });
    st.setPhase('starting');

    await st.ensureAttached();
    expect(OpenSession).not.toHaveBeenCalled();
    expect(st.attached).toBe(false);

    // Reaching ready has to drive the attach itself: _pendingAttach is
    // only ever re-entered by the ResizeObserver, and a phase change
    // fires no resize.
    st.setPhase('');
    await Promise.resolve();
    await Promise.resolve();
    expect(OpenSession).toHaveBeenCalledTimes(1);
  });

  it('holds the panel past ready until the replay has painted', async () => {
    const st = makeTerm({ id: 's3', phase: 'spawning' });
    st.setPhase('spawning');
    st.setPhase('');
    // Still up: the terminal has nothing on it yet.
    expect(st.phaseOverlayShown).toBe(true);

    st.revealAfterReplay();
    expect(st.phaseOverlayShown).toBe(false);
    expect(chrome(st).phaseVisible).toBe(false);
    // The model stays put behind `hidden`: dropping the panel is one
    // attribute flip, not a rebuild.
    expect(chrome(st).phasePanel).not.toBeNull();
  });

  it('ignores a replay that lands while the session is not ready', () => {
    const st = makeTerm({ id: 's4', phase: 'restarting' });
    st.setPhase('restarting');
    st.revealAfterReplay();
    expect(st.phaseOverlayShown).toBe(true);
  });

  it('drops the panel at once when the session comes up dead', () => {
    const st = makeTerm({ id: 's7', phase: 'spawning' });
    st.setPhase('spawning');
    // The daemon's ready event carries alive:false — the spawn failed.
    // events.ts records that before the tile's setPhase runs.
    store.setAlive('s7', false);
    st.setPhase('');
    // No spinner left sitting on top of the dead overlay.
    expect(st.phaseOverlayShown).toBe(false);
  });

  it('writes no error into a closing pane', async () => {
    const st = makeTerm({ id: 's5' });
    st.setPhase('');
    const written: string[] = [];
    st.term.write = ((data: string) => written.push(data)) as never;

    st.setPhase('closing');
    await st.ensureAttached();

    expect(OpenSession).not.toHaveBeenCalled();
    expect(written.join('')).not.toContain('attach failed');
    expect(st.host.classList.contains('closing')).toBe(true);
  });

  it('still reports a genuine attach failure on a ready session', async () => {
    const st = makeTerm({ id: 's6' });
    st.setPhase('');
    const written: string[] = [];
    st.term.write = ((data: string) => written.push(data)) as never;
    OpenSession.mockRejectedValueOnce(new Error('boom'));

    await st.ensureAttached();
    expect(written.join('')).toContain('attach failed');
  });
});

// The tile's state icon has two inputs that can disagree: the phase on the
// SessionInfo payload, and the tile's own live phase from setPhase(). Only
// the latter is current — setPhase never writes back to info — so resolving
// the icon from info alone repaints the stale answer for exactly the
// transition setPhase exists to signal. setPhase must therefore publish the
// tile's phase separately from `info`; dropping that separate field must
// fail here. (That the header RENDERS it as the state icon is
// tile-chrome.test.tsx's half of the pair.)
describe('tile phase publishes the live phase', () => {
  const livePhase = (st: Tile) =>
    store.appStore.getState().tileChrome.get(st.info.id)?.phase;

  it('leaves "starting" when setPhase says ready, without a fresh info', () => {
    const st = makeTerm({
      id: 'live-1',
      name: 'booting',
      agent: 'claude',
      phase: 'starting',
      alive: true,
    });
    st.setPhase('starting');
    expect(livePhase(st)).toBe('starting');

    // The payload still says 'starting' — only the tile knows better.
    expect(st.info.phase).toBe('starting');
    // PHASE.ready is the empty string, not 'ready'.
    st.setPhase('');
    expect(livePhase(st)).toBe('');
  });

  it('shows "starting" when the tile enters a starting phase', () => {
    const st = makeTerm({
      id: 'live-2',
      name: 'ready-then-closing',
      agent: 'claude',
      phase: '',
      alive: true,
    });
    st.setPhase('');
    expect(livePhase(st)).toBe('');

    st.setPhase('starting');
    expect(livePhase(st)).toBe('starting');
  });
});
