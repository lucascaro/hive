// @vitest-environment jsdom
//
// Regression test for the focus switch on the close path
// (src/app/events.ts, session:event kind `updated`).
//
// The daemon publishes phase `checking` while it runs the pre-flight
// `git status` on the session's worktree, and that check can still end
// in a refusal — the "Close this session anyway?" dialog. The switch
// used to fire on isClosing(), which covers `checking` too, so the
// dialog appeared over a *different* session than the one it was about
// and a cancel left the user parked on the neighbour.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { createScrollTrace } from '../../src/lib/scroll-debug.js';
import * as store from '../../src/store/store.js';

// The bridge re-exports the Wails runtime, which doesn't exist under
// vitest. Mock the full surface so the whole src/app import graph
// resolves; EventsOn is the seam this test drives.
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
    KillSessionAndWorktree: fn(),
    RestartSession: fn(),
    UpdateSession: fn(),
    RestoreSession: fn(),
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

let bridge: typeof import('../../src/bridge.js');
let state: typeof import('../../src/store/store.js').hiveStateView;
let wireDaemonEvents: typeof import('../../src/app/events.js').wireDaemonEvents;

const SESSIONS = [
  { id: 's1', name: 'one', order: 0 },
  { id: 's2', name: 'two', order: 1 },
];

beforeAll(async () => {
  // dom.ts dereferences these at import time.
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  bridge = await import('../../src/bridge.js');
  ({ hiveStateView: state } = await import('../../src/store/store.js'));
  ({ wireDaemonEvents } = await import('../../src/app/events.js'));
});

// Wires a fresh set of handlers and returns the session:event one plus
// the switchTo spy it was built with.
function sessionEventHandler() {
  const switchTo = vi.fn();
  vi.mocked(bridge.EventsOn).mockClear();
  wireDaemonEvents({
    switchTo,
    enforceViewFloor: vi.fn(),
    updateAppTitle: vi.fn(),
    focusActiveTerm: vi.fn(),
    refocusActiveTerm: vi.fn(),
    isDaemonRestarting: () => false,
    scrollTrace: createScrollTrace({ enabled: false }),
  });
  const call = vi
    .mocked(bridge.EventsOn)
    .mock.calls.find(([name]) => name === 'session:event');
  if (!call) throw new Error('session:event handler was never registered');
  return { emit: call[1] as (json: string) => void, switchTo };
}

function emitPhase(
  emit: (json: string) => void,
  id: string,
  phase: string,
): void {
  const session = { ...SESSIONS.find((s) => s.id === id), phase };
  emit(JSON.stringify({ kind: 'updated', session }));
}

beforeEach(() => {
  store.setSessions(structuredClone(SESSIONS));
  state.activeId = 's2';
});

describe('session:event updated — focus on the close path', () => {
  it('keeps focus while the daemon is only checking the worktree', () => {
    const { emit, switchTo } = sessionEventHandler();
    emitPhase(emit, 's2', 'checking');
    expect(switchTo).not.toHaveBeenCalled();
  });

  it('switches to the neighbour once the session is really closing', () => {
    const { emit, switchTo } = sessionEventHandler();
    emitPhase(emit, 's2', 'closing');
    expect(switchTo).toHaveBeenCalledWith('s1');
  });

  it('leaves an inactive session alone while it closes', () => {
    const { emit, switchTo } = sessionEventHandler();
    emitPhase(emit, 's1', 'closing');
    expect(switchTo).not.toHaveBeenCalled();
  });
});
