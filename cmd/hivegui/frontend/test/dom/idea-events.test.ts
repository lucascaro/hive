// @vitest-environment jsdom
//
// The daemon-event half of the idea inbox (src/app/events.ts): the
// `idea:list` / `idea:event` sinks, and the `project_has_ideas`
// confirm-then-force branch of `control:error`.
//
// That branch is the one in this feature that can destroy captured
// work — the daemon refuses the delete, the GUI asks, and only then
// re-issues it with deleteIdeas=true — and it is reachable from no
// other suite: the mock Playwright bridge models the refusal, but no
// spec drives a project delete with open ideas.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';

const KillProject = vi.fn(
  (_id: string, _kill: boolean, _ideas: boolean): Promise<void> =>
    Promise.resolve(),
);
// Handlers registered by wireDaemonEvents, keyed by event name, so a
// test can deliver a daemon payload the way the daemon would.
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
    UpdateSession: fn(),
    ListAgents: fn(),
    ListCustomAgents: fn(),
    SaveCustomAgents: fn(),
    CreateProject: fn(),
    KillProject: (...a: Parameters<typeof KillProject>) => KillProject(...a),
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
    Notify: fn(),
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
let resolveChoiceDialog: typeof import('../../src/app/modals/choice-dialog.js')['resolveChoiceDialog'];

beforeAll(async () => {
  // dom.ts dereferences these at import time.
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  const events = await import('../../src/app/events.js');
  store = await import('../../src/store/store.js');
  ({ resolveChoiceDialog } = await import(
    '../../src/app/modals/choice-dialog.js'
  ));
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
  KillProject.mockReset();
  KillProject.mockResolvedValue(undefined);
  store.resetStore();
});

const deliver = (name: string, payload: unknown) => {
  const h = handlers.get(name);
  if (!h) throw new Error(`no handler registered for ${name}`);
  h(typeof payload === 'string' ? payload : JSON.stringify(payload));
};
const flush = () => new Promise((r) => setTimeout(r, 0));

const IDEA = {
  id: 'i1',
  project_id: 'p1',
  kind: 'idea',
  text: 'a note',
  status: 'open',
  created: '2026-09-05T10:00:00Z',
  updated: '2026-09-05T10:00:00Z',
};

describe('idea daemon events', () => {
  it('seeds the store from idea:list', () => {
    deliver('idea:list', { ideas: [IDEA] });
    expect(store.appStore.getState().ideas).toHaveLength(1);
  });

  it('applies added / updated / removed from idea:event', () => {
    deliver('idea:event', { kind: 'added', idea: IDEA });
    expect(store.appStore.getState().ideas[0].text).toBe('a note');
    deliver('idea:event', {
      kind: 'updated',
      idea: { ...IDEA, text: 'sharper' },
    });
    expect(store.appStore.getState().ideas[0].text).toBe('sharper');
    deliver('idea:event', { kind: 'removed', idea: IDEA });
    expect(store.appStore.getState().ideas).toHaveLength(0);
  });

  it('survives a malformed payload rather than taking the window down', () => {
    deliver('idea:list', 'not json');
    deliver('idea:event', 'not json');
    expect(store.appStore.getState().ideas).toHaveLength(0);
  });
});

describe('project_has_ideas', () => {
  beforeEach(() => {
    store.setProjects([{ id: 'p1', name: 'hive' }]);
    store.setSessions([{ id: 's1', project_id: 'p1' }]);
  });

  it('re-issues the delete with deleteIdeas once the user confirms', async () => {
    deliver('control:error', {
      code: 'project_has_ideas',
      message: '2 open',
      project_id: 'p1',
    });
    await flush();
    resolveChoiceDialog('delete');
    await flush();
    // killSessions stays derived from the project's live sessions, the
    // same way confirmAndDeleteProject derives it.
    expect(KillProject).toHaveBeenCalledWith('p1', true, true);
  });

  it('sends nothing when the user backs out', async () => {
    deliver('control:error', {
      code: 'project_has_ideas',
      message: '2 open',
      project_id: 'p1',
    });
    await flush();
    resolveChoiceDialog('cancel');
    await flush();
    expect(KillProject).not.toHaveBeenCalled();
  });

  it('does not re-issue a delete for a project that vanished mid-question', async () => {
    deliver('control:error', {
      code: 'project_has_ideas',
      message: '2 open',
      project_id: 'p1',
    });
    await flush();
    // Another window finished the delete while the dialog was up.
    store.removeProject('p1');
    resolveChoiceDialog('delete');
    await flush();
    expect(KillProject).not.toHaveBeenCalled();
  });
});
