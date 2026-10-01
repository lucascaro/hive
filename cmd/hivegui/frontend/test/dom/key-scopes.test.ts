// @vitest-environment jsdom
//
// The dispatcher (app/keyboard.ts) walking KEY_SCOPES (app/key-scopes.ts).
//
// runCommand is spied, so these tests pin ROUTING — which command a key
// reaches and whether the key is consumed — not what the command does.
// The existing keyboard-precedence / keyboard-arrows suites cover the
// commands end to end; keymap-parity.test.ts covers the chord data.
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest';

// Commands that decline (return false) or throw in the current test.
const declines = new Set<string>();
const throws = new Set<string>();
const runCommand = vi.fn((id: string) => {
  if (throws.has(id)) throw new Error(`boom ${id}`);
  return !declines.has(id);
});
vi.mock('../../src/app/command-registry.js', async (orig) => ({
  ...(await orig<typeof import('../../src/app/command-registry.js')>()),
  runCommand: (id: string) => runCommand(id),
}));

// The Wails bridge: no Go side under jsdom.
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
    RestartSession: fn(),
    UpdateSession: fn(),
    ListAgents: fn(),
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
    EventsOn: vi.fn(),
    WindowSetTitle: vi.fn(),
    ClipboardGetText: fn(),
  };
});

// view.ts needs layout jsdom does not have; routing is all this pins.
vi.mock('../../src/app/view.js', () => ({
  switchTo: vi.fn(),
  setView: vi.fn(),
  gridSpatialMove: vi.fn(),
  shiftActiveProject: vi.fn(),
  restoreSession: vi.fn(),
  minimizeProject: vi.fn(),
  minimizeSession: vi.fn(),
  isSessionHidden: () => false,
  gridWouldTile: () => false,
}));

const findBox = { value: false };
vi.mock('../../src/app/find-session.js', () => ({
  findBoxActive: () => findBox.value,
  openFindInSession: vi.fn(),
}));
const renameOpen = { value: false };
vi.mock('../../src/app/inline-rename.js', () => ({
  inlineRenameActive: () => renameOpen.value,
  cancelInlineRename: vi.fn(),
  beginInlineRename: vi.fn(),
}));
const choiceOpen = { value: false };
vi.mock('../../src/app/modals/choice-dialog.js', () => ({
  choiceDialogOpen: () => choiceOpen.value,
  dismissChoiceDialog: vi.fn(),
  openChoiceDialog: vi.fn(),
  resolveChoiceDialog: vi.fn(),
}));
const blocked = { value: false };
vi.mock('../../src/lib/phase-steps.js', async (orig) => ({
  ...(await orig<typeof import('../../src/lib/phase-steps.js')>()),
  phaseOf: () => (blocked.value ? 'blocked' : 'working'),
}));
const openQuickIdea = vi.fn();
vi.mock('../../src/app/modals/quick-idea.js', () => ({
  openQuickIdea: (...a: unknown[]) => openQuickIdea(...a),
  closeQuickIdea: vi.fn(),
  initQuickIdea: vi.fn(),
  submitIdea: vi.fn(),
  IDEA_KINDS: ['idea', 'bug', 'feedback'],
}));

type Store = typeof import('../../src/store/store.js');
let store: Store;
let keyboard: typeof import('../../src/app/keyboard.js');
let scopes: typeof import('../../src/app/key-scopes.js');
let actions: typeof import('../../src/app/actions.js');

beforeAll(async () => {
  document.body.innerHTML = `
    <div id="terms"></div><ul id="projects"></ul>
    <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    <div id="settings" class="hv-dialog"><input id="settings-field"></div>
    <div id="project-editor" class="hv-dialog"><input id="pe-field"></div>
    <div id="plugin-view" class="hv-dialog"></div>
    <div id="build-log" class="hv-dialog"></div>`;
  store = await import('../../src/store/store.js');
  keyboard = await import('../../src/app/keyboard.js');
  scopes = await import('../../src/app/key-scopes.js');
  actions = await import('../../src/app/actions.js');
});

beforeEach(() => {
  store.resetStore();
  findBox.value = false;
  renameOpen.value = false;
  choiceOpen.value = false;
  blocked.value = false;
  declines.clear();
  throws.clear();
  runCommand.mockClear();
  openQuickIdea.mockClear();
});

afterEach(() => {
  store.hiveStateView.terms.clear();
});

// jsdom is not a mac, so the platform modifier is Ctrl here unless a test
// dispatches with mac=true itself.
function key(key: string, init: KeyboardEventInit = {}) {
  return new KeyboardEvent('keydown', {
    key,
    bubbles: true,
    cancelable: true,
    ...init,
  });
}
function press(k: string, init: KeyboardEventInit = {}, mac?: boolean) {
  const e = key(k, init);
  if (mac === undefined) window.dispatchEvent(e);
  else keyboard.dispatchKey(e, mac);
  return e;
}
const ran = () => runCommand.mock.calls.map((c) => c[0]);

function activeSession() {
  store.hiveStateView.activeId = 's1';
  store.hiveStateView.sessions = [
    { id: 's1', name: 's1', projectId: 'p1' } as never,
  ];
}
function showDeadOverlay() {
  activeSession();
  store.hiveStateView.terms.set('s1', { deadOverlayShown: true } as never);
}

describe('KEY_SCOPES', () => {
  it('is consulted in the documented order', () => {
    expect(scopes.KEY_SCOPES.map((s) => s.id)).toEqual([
      'find-box',
      'inline-rename',
      'choice-dialog',
      'launcher',
      'project-editor',
      'command-palette',
      'settings',
      'worktrees',
      'quick-idea',
      'idea-inbox',
      'help-overlay',
      'help-modal',
      'whats-new',
      'plugin-view',
      'build-log',
      'blocked-tile',
      'dead-overlay',
      'app',
      'plugins',
    ]);
  });
});

describe('the ⌘I gate on the menu path is derived from the scopes', () => {
  // Hard-coded on purpose: deriving this list from KEY_SCOPES would make
  // the test agree with whatever the scopes say.
  const BLOCKERS: Array<[string, () => void]> = [
    ['inline rename', () => (renameOpen.value = true)],
    ['choice dialog', () => (choiceOpen.value = true)],
    ['launcher', () => store.openModal({ id: 'launcher', req: {} } as never)],
    [
      'project editor',
      () => store.openModal({ id: 'project-editor', editing: null }),
    ],
    ['command palette', () => store.openModal({ id: 'command-palette' })],
    ['settings', () => store.openModal({ id: 'settings' })],
    [
      'worktrees',
      () =>
        store.openModal({ id: 'worktrees', projectId: 'p', projectName: '' }),
    ],
    ['help overlay', () => store.openModal({ id: 'help' })],
    ['help modal', () => store.openModal({ id: 'help-modal' })],
    ["what's new", () => store.openModal({ id: 'whats-new' })],
    ['build log', () => store.openModal({ id: 'build-log', log: '' })],
    [
      'plugin view',
      () =>
        store.openModal({
          id: 'plugin-view',
          pluginId: 'x',
          sessionId: 's',
          props: null,
        } as never),
    ],
  ];

  it('opens the sheet when nothing is up', () => {
    actions.captureIdea();
    expect(openQuickIdea).toHaveBeenCalledOnce();
  });

  for (const [name, open] of BLOCKERS) {
    it(`is refused while the ${name} owns the keyboard`, () => {
      open();
      actions.captureIdea();
      expect(openQuickIdea).not.toHaveBeenCalled();
    });
  }
});

describe('an exclusive scope with a focus trap', () => {
  for (const [id, field] of [
    ['settings', 'settings-field'],
    ['project-editor', 'pe-field'],
  ] as const) {
    it(`${id}: leaves a non-Tab key to the dialog's own listeners`, () => {
      store.openModal(
        id === 'settings'
          ? { id: 'settings' }
          : { id: 'project-editor', editing: null },
      );
      const heard = vi.fn();
      const root = document.getElementById(id) as HTMLElement;
      root.addEventListener('keydown', heard);
      try {
        for (const k of ['Enter', 'a']) {
          const e = key(k);
          (document.getElementById(field) as HTMLElement).dispatchEvent(e);
          expect(e.defaultPrevented).toBe(false);
        }
        expect(heard).toHaveBeenCalledTimes(2);
      } finally {
        root.removeEventListener('keydown', heard);
      }
    });
  }
});

describe('find box (text input)', () => {
  it('leaves typing to the input, and nothing below sees it', () => {
    findBox.value = true;
    showDeadOverlay();
    for (const k of ['a', 'r', 'Escape', 'Enter']) {
      const e = press(k);
      expect(e.defaultPrevented).toBe(false);
    }
    expect(runCommand).not.toHaveBeenCalled();
  });

  it('lets a ⌘/Ctrl chord through to the app', () => {
    findBox.value = true;
    press('0', { ctrlKey: true });
    expect(ran()).toEqual(['zoom-reset']);
  });
});

describe('overlay scopes', () => {
  for (const id of ['plugin-view', 'build-log'] as const) {
    it(`${id} owns Escape over the dead-session overlay`, () => {
      showDeadOverlay();
      store.openModal(
        id === 'build-log'
          ? { id: 'build-log', log: '' }
          : ({
              id: 'plugin-view',
              pluginId: 'x',
              sessionId: 's1',
              props: null,
            } as never),
      );
      press('Escape');
      expect(ran()).toEqual([`${id}.close`]);
    });
  }

  it("a blocked tile's Enter wins over the dead overlay", () => {
    showDeadOverlay();
    blocked.value = true;
    press('Enter');
    expect(ran()).toEqual(['session.answer-worktree-question']);
  });

  it('a blocked tile leaves every other key to the app', () => {
    activeSession();
    blocked.value = true;
    store.hiveStateView.view = 'grid-project';
    press('Enter', { ctrlKey: true });
    expect(ran()).toEqual(['focus-active-session']);
  });

  it('the dead overlay leaves every other key to the app', () => {
    showDeadOverlay();
    press('0', { ctrlKey: true });
    expect(ran()).toEqual(['zoom-reset']);
  });

  it('C: Escape with a stray modifier still closes a dialog', () => {
    store.openModal({ id: 'settings' });
    const e = press('Escape', { shiftKey: true });
    expect(ran()).toEqual(['settings.close']);
    expect(e.defaultPrevented).toBe(true);
  });
});

describe('app chords', () => {
  it('off macOS, Ctrl+- is zoom out and Ctrl+Alt+- is back', () => {
    press('-', { ctrlKey: true }, false);
    press('-', { ctrlKey: true, altKey: true }, false);
    expect(ran()).toEqual(['zoom-out', 'nav-back']);
  });

  it('on macOS, ⌃- is back and ⌘- is zoom out', () => {
    press('-', { ctrlKey: true }, true);
    press('-', { metaKey: true }, true);
    expect(ran()).toEqual(['nav-back', 'zoom-out']);
  });

  it('a declined command leaves its key unconsumed and ends dispatch', () => {
    declines.add('switch-9');
    const e = press('9', { ctrlKey: true });
    expect(e.defaultPrevented).toBe(false);
    expect(ran()).toEqual(['switch-9']);
  });

  it('a reserved chord is left to the terminal and runs nothing', () => {
    const e = press('Z', { ctrlKey: true, shiftKey: true });
    expect(e.defaultPrevented).toBe(false);
    expect(runCommand).not.toHaveBeenCalled();
  });

  it('an unbound chord runs nothing', () => {
    const e = press('k', { ctrlKey: true });
    expect(e.defaultPrevented).toBe(false);
    expect(runCommand).not.toHaveBeenCalled();
  });

  it('a command that throws still consumes its key', () => {
    throws.add('new-session');
    const e = key('t', { ctrlKey: true });
    expect(() => keyboard.dispatchKey(e, false)).toThrow(/boom/);
    expect(e.defaultPrevented).toBe(true);
  });

  it('B: ⇧⌘E and ⇧⌘S are no longer ⌘E and ⌘S', () => {
    press('E', { ctrlKey: true, shiftKey: true });
    press('S', { ctrlKey: true, shiftKey: true });
    expect(runCommand).not.toHaveBeenCalled();
    press('e', { ctrlKey: true });
    press('s', { ctrlKey: true });
    expect(ran()).toEqual(['worktrees', 'toggle-sidebar']);
  });
});
