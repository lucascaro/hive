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
let state: typeof import('../../src/app/state.js').state;

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
    '<div id="terms"></div><ul id="projects"></ul><div id="status"></div>';
  ({ state } = await import('../../src/app/state.js'));
  ({ SessionTerm } = await import('../../src/app/session-term.js'));
});

beforeEach(() => {
  OpenSession.mockClear();
  state.terms.clear();
  state.aliveById.clear();
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
  state.terms.set(info.id, st);
  return st;
}

// Read the label span's text, not the li's whole textContent: an
// 'active' step's leading mark is a stateIcon with a <title> child
// ("Starting"), which would otherwise leak into the li's textContent.
const steps = (st: Tile) =>
  Array.from(st.phaseSteps.children).map(
    (li) =>
      `${li.className.replace('phase-step ', '')}:${li.querySelector('span')?.textContent}`,
  );

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

    expect(st.phaseOverlay.hidden).toBe(false);
    expect(steps(st)).toEqual([
      'active:Registered session',
      'todo:Fetching origin',
      'todo:Creating worktree stone-valley',
      'todo:Starting claude',
    ]);

    st.setPhase('worktree');
    expect(st.phaseStatus.textContent).toBe('Creating worktree stone-valley…');
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
    expect(st.phaseOverlay.hidden).toBe(true);
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
    state.aliveById.set('s7', false);
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
