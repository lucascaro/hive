// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// @vitest-environment jsdom
//
// The daemon-event half of session messaging (spec 338, src/app/events.ts):
//
//   • session:event kind idle_ping stores the cleared session and fires ONE
//     desktop notification, tagged with the session id so several windows
//     collapse to a single toast
//   • session:sent closes the note sheet
//   • control:error with session_busy is claimed by the sheet (and is NOT
//     also flashed as a generic error), while an unrelated error is not
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';

const Notify = vi.fn(
  (_t: string, _s: string, _b: string, _tag: string): Promise<void> =>
    Promise.resolve(),
);
const handlers = new Map<string, (...a: unknown[]) => void>();

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
    SetSessionAttention: fn(),
    RestartSession: fn(),
    RestoreSession: fn(),
    ListClosedSessions: fn(),
    ArchiveSession: fn(),
    ListArchivedSessions: fn(),
    UnarchiveSession: fn(),
    DeleteArchivedSession: fn(),
    UpdateSession: fn(),
    ListAgents: fn(),
    ListCustomAgents: fn(),
    SaveCustomAgents: fn(),
    CreateProject: fn(),
    KillProject: fn(),
    UpdateProject: fn(),
    ListIdeas: fn(),
    AddIdea: fn(),
    UpdateIdea: fn(),
    RemoveIdea: fn(),
    ListWorktrees: fn(),
    RemoveWorktree: fn(),
    CreateWorktree: fn(),
    RenameWorktree: fn(),
    DeleteBranch: fn(),
    LaunchDir: fn(),
    StateDirID: fn(),
    PickDirectory: fn(),
    OpenNewWindow: fn(),
    CloseWindow: fn(),
    IsGitRepo: fn(),
    OpenURL: fn(),
    OpenTerminalAt: fn(),
    Notify: (...a: unknown[]) => Notify(...(a as Parameters<typeof Notify>)),
    SendToSession: fn(),
    SetSessionNotifyWhenIdle: fn(),
    Confirm: fn(),
    RestartDaemon: fn(),
    ReloadGUI: fn(),
    RequestReloadAllGUIs: fn(),
    CheckForUpdate: fn(),
    UpdateStatus: fn(),
    StartUpdate: fn(),
    ApplyUpdateAndRestart: fn(),
    GetUpdateSettings: fn(),
    SaveUpdateSettings: fn(),
    SourceRepoStatusFor: fn(),
    SetClipboardText: fn(),
    LogFrontend: fn(),
    SetDebugTrace: fn(),
    MenuBarLoginItemStatus: fn(),
    SetMenuBarLoginItem: fn(),
    EventsOn: (name: string, handler: (...a: unknown[]) => void) => {
      handlers.set(name, handler);
    },
    WindowSetTitle: vi.fn(),
    ClipboardGetText: fn(),
  };
});

type Store = typeof import('../../src/store/store.js');
let store: Store;
let msg: typeof import('../../src/app/modals/message-session.js');

beforeAll(async () => {
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  const events = await import('../../src/app/events.js');
  store = await import('../../src/store/store.js');
  msg = await import('../../src/app/modals/message-session.js');
  events.wireDaemonEvents({
    switchTo: () => {},
    renderAll: () => {},
    setFocusedTile: () => {},
    refocusActiveTerm: () => {},
    isDaemonRestarting: () => false,
    checkForUpdates: () => {},
  } as unknown as Parameters<typeof events.wireDaemonEvents>[0]);
});

beforeEach(() => {
  Notify.mockClear();
  store.resetStore();
});

const deliver = (name: string, payload: unknown) => {
  const h = handlers.get(name);
  if (!h) throw new Error(`no handler registered for ${name}`);
  h(typeof payload === 'string' ? payload : JSON.stringify(payload));
};

const sess = (over: Record<string, unknown> = {}) => ({
  id: 's1',
  name: 'refactor',
  alive: true,
  project_id: 'p1',
  ...over,
});

describe('idle_ping', () => {
  it('stores the disarmed session and notifies once, tagged by session', () => {
    store.setProjects([{ id: 'p1', name: 'hive' }] as never);
    store.addSession(sess({ notify_when_idle: true }) as never);
    deliver('session:event', {
      kind: 'idle_ping',
      session: sess({ last_summary: 'tests green' }),
    });
    expect(
      store.appStore.getState().sessions.find((s) => s.id === 's1')
        ?.notify_when_idle,
    ).toBeFalsy();
    expect(Notify).toHaveBeenCalledTimes(1);
    expect(Notify).toHaveBeenCalledWith(
      'refactor',
      'hive',
      'Finished — tests green',
      's1',
    );
  });

  it('words an exit and an error differently from finishing', () => {
    store.addSession(sess() as never);
    deliver('session:event', {
      kind: 'idle_ping',
      session: sess({ state: 'exited' }),
    });
    deliver('session:event', {
      kind: 'idle_ping',
      session: sess({ state: 'error' }),
    });
    expect(Notify.mock.calls[0][2]).toMatch(/^Exited/);
    expect(Notify.mock.calls[1][2]).toMatch(/^Stopped on an error/);
  });
});

describe('send answers', () => {
  it('session:sent closes the sheet', () => {
    store.addSession(sess() as never);
    msg.openMessageSession('s1');
    expect(store.isModalOpen('message-session')).toBe(true);
    deliver('session:sent', { session_id: 's1', delivery: 'typed' });
    expect(store.isModalOpen('message-session')).toBe(false);
  });

  it('session_busy is claimed by the sheet', () => {
    store.addSession(sess() as never);
    msg.openMessageSession('s1');
    deliver('control:error', { code: 'session_busy', session_id: 's1' });
    expect(store.modalEntry('message-session')?.busy).toBe(true);
  });

  it('an unrelated error leaves the sheet alone', () => {
    store.addSession(sess() as never);
    msg.openMessageSession('s1');
    deliver('control:error', { code: 'session_busy', session_id: 'other' });
    expect(store.modalEntry('message-session')?.busy).toBeFalsy();
  });
});
