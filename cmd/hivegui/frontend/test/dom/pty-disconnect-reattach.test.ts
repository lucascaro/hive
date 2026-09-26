// @vitest-environment jsdom
//
// Reattaching a tile whose attach connection dropped (#461).
//
// The daemon now hangs up on a client that stops reading. Unlike
// Restart Session, that drop is followed by no session:event(updated,
// alive=true), so before this the visible terminal sat detached until
// the user switched away and back. pty:disconnect now arms a backoff
// timer; these tests drive the real handlers (events.ts) against a
// real SessionTerm with the bridge mocked.
import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  beforeEach,
  afterEach,
} from 'vitest';
import * as bridge from '../../src/bridge.js';
import * as store from '../../src/store/store.js';
import { setTerm, clearTerms } from '../../src/store/terms.js';
import { createScrollTrace } from '../../src/lib/scroll-debug.js';

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
    SetSessionAttention: fn(),
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
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
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
  const { wireDaemonEvents } = await import('../../src/app/events.js');
  wireDaemonEvents({
    switchTo: vi.fn(),
    enforceViewFloor: vi.fn(),
    updateAppTitle: vi.fn(),
    focusActiveTerm: vi.fn(),
    refocusActiveTerm: vi.fn(),
    isDaemonRestarting: () => false,
    checkForUpdates: vi.fn(),
    scrollTrace: createScrollTrace({ enabled: false }),
  });
});

function emit(event: string, ...args: unknown[]) {
  for (const call of vi.mocked(bridge.EventsOn).mock.calls) {
    if (call[0] === event) (call[1] as (...a: unknown[]) => void)(...args);
  }
}

// A visible, alive, attached tile: the state a live terminal is in when
// the daemon hangs up on it.
function liveTile(id: string): Tile {
  const info = { id, name: id, alive: true } as Info;
  emit('session:list', JSON.stringify({ sessions: [info] }));
  const st = new SessionTerm(info);
  Object.defineProperty(st.body, 'clientWidth', { value: 400 });
  Object.defineProperty(st.body, 'clientHeight', { value: 300 });
  st.fit.fit = () => {};
  setTerm(id, st);
  store.setAliveById(new Map([[id, true]]));
  store.hiveStateView.view = 'single';
  store.hiveStateView.activeId = id;
  st.attached = true;
  return st;
}

// Let the timer's awaited OpenSession promise settle.
async function flush() {
  for (let i = 0; i < 5; i++) await Promise.resolve();
}

beforeEach(() => {
  vi.useFakeTimers();
  OpenSession.mockReset();
  OpenSession.mockImplementation(() => Promise.resolve({}));
  clearTerms();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('pty:disconnect on a live tile', () => {
  it('reattaches a visible, alive tile after the first backoff step', async () => {
    const st = liveTile('a1');
    emit('pty:disconnect', 'a1');
    expect(st.attached).toBe(false);
    expect(st.needsReattach).toBe(true);

    await vi.advanceTimersByTimeAsync(499);
    expect(OpenSession).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    await flush();
    expect(OpenSession).toHaveBeenCalledTimes(1);
    expect(st.attached).toBe(true);
    expect(st.needsReattach).toBe(false);
  });

  it('never reattaches a tile whose session is closing', async () => {
    const st = liveTile('a2');
    st.setPhase('killing');
    emit('pty:disconnect', 'a2');
    await vi.advanceTimersByTimeAsync(10_000);
    expect(OpenSession).not.toHaveBeenCalled();
  });

  it('never reattaches a dead session', async () => {
    liveTile('a3');
    emit('pty:disconnect', 'a3');
    store.setAliveById(new Map([['a3', false]]));
    await vi.advanceTimersByTimeAsync(10_000);
    expect(OpenSession).not.toHaveBeenCalled();
  });

  it('attaches once when the alive=true event beats the timer', async () => {
    const st = liveTile('a4');
    emit('pty:disconnect', 'a4');
    emit(
      'session:event',
      JSON.stringify({
        kind: 'updated',
        session: { id: 'a4', name: 'a4', alive: true },
      }),
    );
    await flush();
    expect(OpenSession).toHaveBeenCalledTimes(1);
    expect(st.attached).toBe(true);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(OpenSession).toHaveBeenCalledTimes(1);
  });

  it('backs off 500, 1000, 2000 ms across repeated drops', async () => {
    liveTile('a5');
    const gaps: number[] = [];
    let last = Date.now();
    OpenSession.mockImplementation(() => {
      gaps.push(Date.now() - last);
      return Promise.resolve({});
    });
    for (let i = 0; i < 3; i++) {
      last = Date.now();
      emit('pty:disconnect', 'a5');
      await vi.advanceTimersByTimeAsync(5_000);
      await flush();
    }
    expect(gaps).toEqual([500, 1000, 2000]);
  });

  it('resets the backoff once a replay completes', async () => {
    const st = liveTile('a6');
    emit('pty:disconnect', 'a6');
    await vi.advanceTimersByTimeAsync(500);
    await flush();
    emit('pty:event', 'a6', JSON.stringify({ kind: 'scrollback_replay_done' }));
    expect(st._reattachAttempts).toBe(0);
  });

  it('retries a failed dial quietly on the next tick', async () => {
    const st = liveTile('a7');
    const write = vi.spyOn(st.term, 'write');
    OpenSession.mockImplementationOnce(() =>
      Promise.reject(new Error('dial failed')),
    );
    emit('pty:disconnect', 'a7');
    await vi.advanceTimersByTimeAsync(500);
    await flush();
    expect(OpenSession).toHaveBeenCalledTimes(1);
    expect(st.attached).toBe(false);
    expect(st.needsReattach).toBe(true);
    expect(
      write.mock.calls.some((c) => String(c[0]).includes('attach failed')),
    ).toBe(false);

    await vi.advanceTimersByTimeAsync(1000);
    await flush();
    expect(OpenSession).toHaveBeenCalledTimes(2);
    expect(st.attached).toBe(true);
    expect(st.needsReattach).toBe(false);
  });

  it('does not wipe a terminal that an update finds already attaching', async () => {
    const st = liveTile('a8');
    let resolveOpen: () => void = () => {};
    OpenSession.mockImplementationOnce(
      () => new Promise((r) => (resolveOpen = () => r({}))),
    );
    emit('pty:disconnect', 'a8');
    await vi.advanceTimersByTimeAsync(500);
    expect(st._attaching).toBe(true);

    const reset = vi.spyOn(st.term, 'reset');
    // A title change or rename lands mid-dial.
    emit(
      'session:event',
      JSON.stringify({
        kind: 'updated',
        session: { id: 'a8', name: 'renamed', alive: true },
      }),
    );
    resolveOpen();
    await flush();
    // And another after the dial succeeded, before replay done.
    emit(
      'session:event',
      JSON.stringify({
        kind: 'updated',
        session: { id: 'a8', name: 'renamed again', alive: true },
      }),
    );
    expect(reset).not.toHaveBeenCalled();
    expect(OpenSession).toHaveBeenCalledTimes(1);
  });

  it('schedules no retry spam for a deferred attach', async () => {
    const st = liveTile('a9');
    emit('pty:disconnect', 'a9');
    // The session went back to starting: ensureAttached defers to the
    // setPhase re-entry instead of dialing.
    st.phase = 'starting';
    await vi.advanceTimersByTimeAsync(10_000);
    expect(OpenSession).not.toHaveBeenCalled();
    expect(st._reattachTimer).toBe(0);
  });
});
