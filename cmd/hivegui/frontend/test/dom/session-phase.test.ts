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
const RestartSession = vi.fn((_id: string) => Promise.resolve());

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
    RestartSession: (id: string) => RestartSession(id),
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
  RestartSession.mockReset();
  RestartSession.mockImplementation(() => Promise.resolve());
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

// setDead()'s store write, against a REAL SessionTerm. tile-overlays.test.tsx
// covers what a given store shape renders, but it patches the store by hand
// and every other `setDead` under test/ is a stub — so a regression in this
// method (dropping the reason, or clearing it on a later call) would pass
// every other test in the suite.
describe('setDead', () => {
  it('publishes the reason, and never clears it once set', () => {
    const st = makeTerm({ id: 'd1', name: 'doomed', alive: true });

    expect(chrome(st).dead).toBe(false);
    expect(chrome(st).deadReason).toBe('');

    st.setDead(true, 'exit status 127');
    expect(chrome(st).dead).toBe(true);
    expect(chrome(st).deadReason).toBe('exit status 127');
    // The class on the host stays SessionTerm's — it dims the whole tile,
    // and it is not part of what React renders.
    expect(st.host.classList.contains('dead')).toBe(true);

    // Sticky by design, exactly as the imperative subtitle was: the write
    // is guarded on a reason being supplied, so a later transition with
    // none leaves the last one in place behind the hidden overlay.
    st.setDead(true);
    expect(chrome(st).deadReason).toBe('exit status 127');

    st.setDead(false);
    expect(chrome(st).dead).toBe(false);
    expect(chrome(st).deadReason).toBe('exit status 127');
    expect(st.host.classList.contains('dead')).toBe(false);
    // keyboard.ts routes Enter/Escape off this flag, so it tracks the
    // store write rather than lagging it.
    expect(st.deadOverlayShown).toBe(false);
  });
});

describe('_restartDead', () => {
  it('sends one RestartSession per death, however often it is pressed', () => {
    // A second request while the first is in flight would tear down the
    // process the first one just spawned.
    const st = makeTerm({ id: 'r1', name: 'restart me', alive: true });
    st.setDead(true, 'exit status 1');
    st._restartDead();
    st._restartDead();
    expect(RestartSession).toHaveBeenCalledOnce();
    expect(RestartSession).toHaveBeenCalledWith('r1');
    // The overlay is not dropped here: events.ts clears it on the alive
    // edge, so a failed restart leaves Close/Dismiss in place.
    expect(st.deadOverlayShown).toBe(true);

    // Revived, then died again: the next restart goes out.
    st.setDead(false);
    st.setDead(true, 'exit status 1');
    st._restartDead();
    expect(RestartSession).toHaveBeenCalledTimes(2);
  });

  it('stays guarded while ensureAttached re-asserts the dead card', () => {
    // Every render/focus/resize of a dead tile goes through
    // ensureAttached, which calls setDead(true) again. That must not
    // re-arm Restart while the first request is still in flight.
    const st = makeTerm({ id: 'r3', name: 'busy', alive: false });
    store.setAliveById(new Map([['r3', false]]));
    st.setDead(true, 'exit status 1');
    st._restartDead();
    st.ensureAttached();
    expect(st.deadOverlayShown).toBe(true);
    st._restartDead();
    expect(RestartSession).toHaveBeenCalledOnce();
  });

  it('allows a retry after the request fails', async () => {
    RestartSession.mockImplementationOnce(() => Promise.reject('boom'));
    const st = makeTerm({ id: 'r2', name: 'flaky', alive: true });
    st.setDead(true);
    st._restartDead();
    await Promise.resolve();
    await Promise.resolve();
    expect(st.deadOverlayShown).toBe(true);
    st._restartDead();
    expect(RestartSession).toHaveBeenCalledTimes(2);
  });
});
