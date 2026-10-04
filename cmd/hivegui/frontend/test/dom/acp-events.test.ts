// @vitest-environment jsdom
//
// The daemon-event half of the ACP store (spec 496): src/app/events.ts
// must reset every ACP transcript on a session list (sent on every
// (re)connect, when deltas may have been missed) and drop one whose
// session is removed. The store functions have their own unit tests;
// this pins the wiring that calls them.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { createScrollTrace } from '../../src/lib/scroll-debug.js';
import * as store from '../../src/store/store.js';
import {
  acpStore,
  applyAcpFrame,
  noteSentPrompt,
} from '../../src/store/acp.js';

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
    GetAcpTranscript: fn(),
    PromptAcp: fn(),
    AnswerPermission: fn(),
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

function wire(): (name: string, json: string) => void {
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
  return (name, json) => {
    const call = vi
      .mocked(bridge.EventsOn)
      .mock.calls.find(([n]) => n === name);
    if (!call) throw new Error(`${name} handler was never registered`);
    (call[1] as (j: string) => void)(json);
  };
}

const S = (id: string) => ({ id, name: id, order: 0, alive: true });

function seedLoaded(id: string) {
  applyAcpFrame({ session_id: id, epoch: 1, reset: true, items: [] } as never);
  expect(acpStore.getState().byId.get(id)?.tx.loaded).toBe(true);
}

beforeEach(() => {
  store.resetStore({ sessions: [S('a'), S('b')] as never });
  acpStore.setState({ byId: new Map() });
});

describe('ACP transcripts across daemon events', () => {
  it('acp:transcript folds a frame and drops a malformed one', () => {
    const emit = wire();
    emit(
      'acp:transcript',
      JSON.stringify({ session_id: 'a', epoch: 1, reset: true, items: [] }),
    );
    expect(acpStore.getState().byId.get('a')?.tx.loaded).toBe(true);
    expect(() => emit('acp:transcript', '{not json')).not.toThrow();
    expect(acpStore.getState().byId.get('a')?.tx.loaded).toBe(true);
  });

  it('a session list marks live transcripts for refetch and drops gone ones', () => {
    const emit = wire();
    seedLoaded('a');
    seedLoaded('b');
    emit('session:list', JSON.stringify({ sessions: [S('a')] }));
    const byId = acpStore.getState().byId;
    expect(byId.get('a')?.tx.loaded).toBe(false);
    expect(byId.has('b')).toBe(false);
  });

  it('a removed session drops its transcript', () => {
    const emit = wire();
    seedLoaded('a');
    seedLoaded('b');
    emit('session:event', JSON.stringify({ kind: 'removed', session: S('a') }));
    const byId = acpStore.getState().byId;
    expect(byId.has('a')).toBe(false);
    expect(byId.has('b')).toBe(true);
  });

  it('a control:error naming an ACP session gives its refused prompt back', () => {
    const emit = wire();
    seedLoaded('a');
    noteSentPrompt('a', 'hello');
    emit(
      'control:error',
      JSON.stringify({ code: 'acp_busy', message: 'busy', session_id: 'a' }),
    );
    const e = acpStore.getState().byId.get('a');
    expect(e?.sent).toBeNull();
    expect(e?.returned).toBe('hello');
  });
});
