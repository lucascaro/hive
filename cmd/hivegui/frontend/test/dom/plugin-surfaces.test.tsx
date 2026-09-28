// @vitest-environment jsdom
//
// The four UI plugin surfaces (spec 471) through the real app wiring:
// the palette and ⌘/ overlay (commands), the keyboard pipeline (chords,
// with core always first), the session view modal and panel, and the
// session banner. Plugins load through app/plugin-host.ts with an
// injected importer, so each "module" is a plain object here.
import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  beforeEach,
  afterEach,
} from 'vitest';
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import type { SessionInfo, PluginInfo } from '../../src/app/state.js';
import type { HiveAPI } from '../../src/app/plugin-host.js';

vi.mock('../../src/lib/platform.js', async (orig) => ({
  ...(await orig<typeof import('../../src/lib/platform.js')>()),
  isMac: true,
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
    UpdateSession: fn(),
    ListAgents: vi.fn(() => Promise.resolve([])),
    ListCustomAgents: fn(),
    SaveCustomAgents: fn(),
    GetAgentSettings: fn(),
    SaveAgentSettings: fn(),
    CreateProject: fn(),
    KillProject: fn(),
    UpdateProject: fn(),
    LaunchDir: vi.fn(() => Promise.resolve('')),
    PickDirectory: fn(),
    OpenNewWindow: fn(),
    CloseWindow: fn(),
    IsGitRepo: vi.fn(() => Promise.resolve(false)),
    OpenURL: fn(),
    OpenTerminalAt: fn(),
    Notify: fn(),
    Confirm: fn(),
    RestartDaemon: fn(),
    CheckForUpdate: fn(),
    SetClipboardText: fn(),
    ClipboardGetText: fn(),
    ResolvePrompt: fn(),
    ResolveWorktreeChoice: fn(),
    StateDirID: fn(),
    LogFrontend: vi.fn(),
    EventsOn: vi.fn(() => () => {}),
    WindowSetTitle: vi.fn(),
    PluginAssetBase: vi.fn(() => Promise.resolve('')),
    SetPluginConfig: fn(),
    SetClientUI: fn(),
  };
});

vi.mock('../../src/app/view.js', () => ({
  switchTo: vi.fn(),
  setView: vi.fn(),
  gridSpatialMove: vi.fn(),
  shiftActiveProject: vi.fn(),
  restoreSession: vi.fn(),
  minimizeProject: vi.fn(),
  minimizeSession: vi.fn(),
  isMinimized: () => false,
  enforceViewFloor: vi.fn(),
  toggleMinimizeActive: vi.fn(),
}));

vi.mock('../../src/app/events.js', () => ({
  clearAttention: vi.fn(),
  raiseWorktreeChoice: vi.fn(),
}));

let store: typeof import('../../src/store/store.js');
let host: typeof import('../../src/app/plugin-host.js');
let surfaces: typeof import('../../src/components/PluginSurfaces.js');
let palette: typeof import('../../src/app/modals/command-palette.js');

const BODY =
  '<div id="app"><div id="terms"></div><ul id="projects"></ul>' +
  '<div id="status"><span id="status-text"></span><span id="status-hint"></span></div>' +
  '<div id="banners"></div><aside id="activity-panel" hidden></aside>' +
  '<aside id="plugin-panel" hidden></aside>' +
  '<div id="plugin-view" class="hv-dialog hidden" role="dialog"></div>' +
  '<div id="help-overlay" class="hidden"></div></div>';

beforeAll(async () => {
  document.body.innerHTML = BODY;
  store = await import('../../src/store/store.js');
  host = await import('../../src/app/plugin-host.js');
  surfaces = await import('../../src/components/PluginSurfaces.js');
  palette = await import('../../src/app/modals/command-palette.js');
  const kb = await import('../../src/app/keyboard.js');
  kb.initKeyboard({
    withoutNavHistory: (fn: () => void) => fn(),
    bumpFontSize: () => {},
    resetFontSize: () => {},
    focusActiveTerm: () => {},
  });
});

const session = {
  id: 's1',
  name: 'one',
  project_id: 'p1',
} as unknown as SessionInfo;

function notes(): PluginInfo {
  return {
    id: 'notes',
    name: 'Notes',
    version: '1.0.0',
    api_version: '0.2',
    source: '/src/notes',
    command: [],
    enabled: true,
    status: 'running',
    restarts: 0,
    ui: { entry: 'ui.mjs' },
  };
}

let api: HiveAPI | null = null;
const runs = { toggle: vi.fn(), steal: vi.fn(), banner: vi.fn() };

async function flush() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await Promise.resolve();
    });
  }
}

async function loadNotes() {
  host.initPluginHost({
    components: {},
    switchTo: vi.fn(),
    refocusActiveTerm: vi.fn(),
    importer: async () => ({
      default: (hive: HiveAPI) => {
        api = hive;
        return {
          commands: [
            {
              id: 'toggle',
              title: 'Toggle notes panel',
              keys: { key: 'o', shift: true },
              run: runs.toggle,
            },
            {
              id: 'steal',
              title: 'Steal new session',
              keys: { key: 't' },
              run: runs.steal,
            },
          ],
          sessionView: {
            modal: {
              title: 'Session note',
              component: ({
                session: s,
                props,
              }: {
                session: SessionInfo;
                props: unknown;
              }) => (
                <p id="notes-modal-body">
                  note for {s.name} {String(props)}
                </p>
              ),
            },
            panel: {
              title: 'Notes',
              component: ({ session: s }: { session: SessionInfo }) => (
                <p id="notes-panel-body">panel for {s.name}</p>
              ),
            },
            banner: (s: SessionInfo) =>
              s.id === 's1'
                ? {
                    text: 'Pinned: ship it',
                    action: { label: 'Open', run: runs.banner },
                  }
                : null,
          },
        };
      },
    }),
  });
  store.setPlugins([notes()]);
  await flush();
}

function press(key: string, init: KeyboardEventInit = {}) {
  window.dispatchEvent(
    new KeyboardEvent('keydown', {
      key,
      bubbles: true,
      cancelable: true,
      ...init,
    }),
  );
}

beforeEach(async () => {
  document.body.innerHTML = BODY;
  store.resetStore();
  host.resetPluginHostForTest();
  for (const f of Object.values(runs)) f.mockClear();
  api = null;
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  store.setSessions([session]);
  store.setActiveId('s1');
  store.setView('single', false);
  await loadNotes();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('commands', () => {
  it('appear in the palette with their key hint', () => {
    const cmds = palette.paletteCommands();
    const toggle = cmds.find((c) => c.id === 'plugin:notes:toggle');
    expect(toggle?.name).toBe('Toggle notes panel');
    expect(toggle?.shortcut).toBe('⇧⌘O');
    // ⌘T is core's (new session): the command stays, without a key.
    expect(cmds.find((c) => c.id === 'plugin:notes:steal')?.shortcut).toBe('');
    toggle?.run();
    expect(runs.toggle).toHaveBeenCalledOnce();
  });

  it('list their chords in the ⌘/ overlay under Plugins', async () => {
    const { HelpOverlay } = await import(
      '../../src/components/modals/HelpOverlay.js'
    );
    const root = document.getElementById('help-overlay') as HTMLElement;
    const { container } = render(<HelpOverlay root={root} />);
    act(() => store.openModal({ id: 'help' }));
    const groups = [
      ...container.querySelectorAll('#help-overlay-groups section'),
    ];
    const plugins = groups.find(
      (g) => g.querySelector('h4')?.textContent === 'Plugins',
    );
    expect(plugins?.textContent).toContain('⇧⌘O');
    expect(plugins?.textContent).toContain('Toggle notes panel');
    expect(plugins?.textContent).not.toContain('Steal new session');
  });

  it('a plugin chord runs its command; a core chord never reaches a plugin', () => {
    // Ctrl, not ⌘: the platform mock above changes the exported isMac,
    // but cmdOrCtrl's default argument closes over jsdom's real one.
    press('O', { ctrlKey: true, shiftKey: true, code: 'KeyO' });
    expect(runs.toggle).toHaveBeenCalledOnce();
    press('t', { ctrlKey: true, code: 'KeyT' });
    expect(runs.steal).not.toHaveBeenCalled();
    expect(store.isModalOpen('launcher')).toBe(true);
  });
});

describe('session view', () => {
  it('opens a modal for the session, and Escape resolves "dismissed"', async () => {
    render(<surfaces.PluginSurfaces />);
    let result: string | null = null;
    act(() => {
      void (api as HiveAPI).openSessionView('s1', 'hello').then((r) => {
        result = r;
      });
    });
    const root = document.getElementById('plugin-view') as HTMLElement;
    expect(root.classList).not.toContain('hidden');
    expect(root.querySelector('#notes-modal-body')?.textContent).toBe(
      'note for one hello',
    );
    expect(root.querySelector('#plugin-view-title')?.textContent).toContain(
      'Session note',
    );
    press('Escape');
    await flush();
    expect(result).toBe('dismissed');
    expect(store.isModalOpen('plugin-view')).toBe(false);
    expect(root.classList).toContain('hidden');
  });

  it('toggles a panel beside the terminal, exclusive with the inspector', () => {
    render(<surfaces.PluginSurfaces />);
    const aside = document.getElementById('plugin-panel') as HTMLElement;
    expect(aside.hidden).toBe(true);
    act(() => (api as HiveAPI).togglePanel());
    expect(aside.hidden).toBe(false);
    expect(aside.querySelector('#notes-panel-body')?.textContent).toBe(
      'panel for one',
    );
    expect(document.getElementById('app')?.classList).toContain(
      'plugin-panel-open',
    );
    act(() => store.setActivityPanel(true));
    expect(aside.hidden).toBe(true);
    expect(store.appStore.getState().pluginPanel).toBeNull();
    act(() => (api as HiveAPI).togglePanel());
    expect(store.appStore.getState().activityPanel).toBe(false);
  });

  it('shows a banner for the active session', () => {
    const { container } = render(<surfaces.PluginBanner />);
    expect(
      container.querySelector('.hv-plugin-banner__label')?.textContent,
    ).toBe('Pinned: ship it');
    fireEvent.click(
      container.querySelector('.hv-plugin-banner__action') as HTMLElement,
    );
    expect(runs.banner).toHaveBeenCalledOnce();
    act(() => store.setActiveId(null));
    expect(container.querySelector('.hv-plugin-banner')).toBeNull();
  });

  it('closes the view when its plugin is disabled', async () => {
    render(<surfaces.PluginSurfaces />);
    act(() => {
      void (api as HiveAPI).openSessionView('s1');
    });
    expect(store.isModalOpen('plugin-view')).toBe(true);
    act(() =>
      store.setPlugins([{ ...notes(), enabled: false, status: 'stopped' }]),
    );
    await flush();
    expect(store.isModalOpen('plugin-view')).toBe(false);
  });
});
