// @vitest-environment jsdom
//
// The daemon-event half of Settings → Plugins (src/app/events.ts): the
// `plugin:list` / `plugin:event` sinks, their malformed-payload
// branches, and the `control:error` claim that keeps this window's own
// install failure off the generic status line while leaving another
// window's (or a nonce-less) plugin_install_failed on it.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';

const handlers = new Map<string, (...a: unknown[]) => void>();
const flashStatus = vi.fn();

// The real dom.js, with the status line observed.
vi.mock('../../src/app/dom.js', async (orig) => ({
  ...(await orig<typeof import('../../src/app/dom.js')>()),
  flashStatus: (...a: unknown[]) => flashStatus(...a),
}));

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
    KillProject: fn(),
    UpdateProject: fn(),
    ListIdeas: fn(),
    AddIdea: fn(),
    UpdateIdea: fn(),
    RemoveIdea: fn(),
    ListPlugins: fn(),
    InstallPlugin: fn(),
    SetPluginEnabled: fn(),
    RemovePlugin: fn(),
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
let plugins: typeof import('../../src/app/plugins.js');
let bridge: typeof import('../../src/bridge.js');

beforeAll(async () => {
  // dom.ts dereferences these at import time.
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  const events = await import('../../src/app/events.js');
  store = await import('../../src/store/store.js');
  plugins = await import('../../src/app/plugins.js');
  bridge = await import('../../src/bridge.js');
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
  store.resetStore();
  flashStatus.mockClear();
});

const deliver = (name: string, payload: unknown) => {
  const h = handlers.get(name);
  if (!h) throw new Error(`no handler registered for ${name}`);
  h(typeof payload === 'string' ? payload : JSON.stringify(payload));
};
const flush = () => new Promise((r) => setTimeout(r, 0));
const statusText = () => flashStatus.mock.calls.map((c) => c[0]).join('\n');

const PLUGIN = {
  id: 'webhook',
  name: 'Webhook',
  version: '0.1.0',
  api_version: '0.1',
  source: '/src/webhook',
  command: ['node', 'main.mjs'],
  enabled: false,
  status: 'stopped',
  restarts: 0,
};

describe('plugin daemon events', () => {
  it('seeds the store from plugin:list', () => {
    deliver('plugin:list', { plugins: [PLUGIN] });
    expect(store.appStore.getState().plugins).toHaveLength(1);
  });

  it('applies added / updated / removed from plugin:event', () => {
    deliver('plugin:event', { kind: 'added', plugin: PLUGIN });
    deliver('plugin:event', {
      kind: 'updated',
      plugin: { ...PLUGIN, enabled: true, status: 'running' },
    });
    expect(store.appStore.getState().plugins[0].status).toBe('running');
    deliver('plugin:event', { kind: 'removed', plugin: PLUGIN });
    expect(store.appStore.getState().plugins).toHaveLength(0);
  });

  it('survives a malformed plugin:list and says so', () => {
    store.setPlugins([PLUGIN]);
    deliver('plugin:list', 'not json');
    expect(statusText()).toContain('bad plugin payload');
    // The list the window already had is kept, not wiped.
    expect(store.appStore.getState().plugins).toHaveLength(1);
  });

  it('survives a malformed plugin:event and says so', () => {
    deliver('plugin:event', 'not json');
    expect(statusText()).toContain('bad plugin event');
    expect(store.appStore.getState().plugins).toHaveLength(0);
  });
});

describe('plugin_install_failed', () => {
  it("claims this window's own failure and keeps it off the status line", async () => {
    const pending = plugins.installPlugin('/src/nothing');
    const nonce = vi.mocked(bridge.InstallPlugin).mock.calls.at(-1)![1];
    deliver('control:error', {
      code: 'plugin_install_failed',
      message: 'no hive-plugin.json',
      nonce,
    });
    await expect(pending).rejects.toThrow('no hive-plugin.json');
    await flush();
    expect(statusText()).not.toContain('plugin_install_failed');
  });

  it("leaves another window's failure to the generic status line", async () => {
    deliver('control:error', {
      code: 'plugin_install_failed',
      message: 'boom',
      nonce: 'someone-else',
    });
    await flush();
    expect(statusText()).toContain('plugin_install_failed: boom');
  });
});
