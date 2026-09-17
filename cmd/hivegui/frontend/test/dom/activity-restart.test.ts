// @vitest-environment jsdom
//
// Spec 416 phase 4: a restart or revive keeps the session id but gives
// the daemon a fresh activity machine. The client's copy of the previous
// life — calls it still thinks are running included — must go, so the
// next render refetches.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { createScrollTrace } from '../../src/lib/scroll-debug.js';
import * as store from '../../src/store/store.js';
import { activityStore, applyActivityFrame } from '../../src/store/activity.js';

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
    SetSessionAttention: vi.fn(() => Promise.resolve()),
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
    GetActivity: fn(),
  };
});

let bridge: typeof import('../../src/bridge.js');
let wireDaemonEvents: typeof import('../../src/app/events.js').wireDaemonEvents;

beforeAll(async () => {
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  bridge = await import('../../src/bridge.js');
  ({ wireDaemonEvents } = await import('../../src/app/events.js'));
});

function sessionEventHandler() {
  vi.mocked(bridge.EventsOn).mockClear();
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
  const call = vi
    .mocked(bridge.EventsOn)
    .mock.calls.find(([name]) => name === 'session:event');
  if (!call) throw new Error('session:event handler was never registered');
  return call[1] as (json: string) => void;
}

const S = { id: 's1', name: 'one', order: 0, alive: true };
const emitUpdated = (emit: (j: string) => void, over: object) =>
  emit(JSON.stringify({ kind: 'updated', session: { ...S, ...over } }));

beforeEach(() => {
  store.resetStore({ sessions: [structuredClone(S)] as never });
  activityStore.setState({ byId: new Map() });
});

function seedRunningCall() {
  applyActivityFrame({
    session_id: 's1',
    events: [
      {
        tool: 'Bash',
        call_id: 'c1',
        plan_idx: -1,
        started_at: new Date().toISOString(),
      },
    ],
    stale_at: new Date().toISOString(),
  });
  expect(activityStore.getState().byId.has('s1')).toBe(true);
}

describe('activity across a restart', () => {
  it('drops the session activity when a restart reaches ready', () => {
    const emit = sessionEventHandler();
    emitUpdated(emit, { phase: '' });
    seedRunningCall();
    emitUpdated(emit, { phase: 'restarting' });
    expect(activityStore.getState().byId.has('s1')).toBe(true);
    emitUpdated(emit, { phase: '' });
    expect(activityStore.getState().byId.has('s1')).toBe(false);
  });

  it('drops it when a dead session is revived', () => {
    const emit = sessionEventHandler();
    emitUpdated(emit, { phase: '', alive: false });
    seedRunningCall();
    emitUpdated(emit, { phase: '', alive: true });
    expect(activityStore.getState().byId.has('s1')).toBe(false);
  });

  it('keeps it across an ordinary update', () => {
    const emit = sessionEventHandler();
    emitUpdated(emit, { phase: '' });
    seedRunningCall();
    emitUpdated(emit, { phase: '', name: 'renamed' });
    expect(activityStore.getState().byId.has('s1')).toBe(true);
  });
});
