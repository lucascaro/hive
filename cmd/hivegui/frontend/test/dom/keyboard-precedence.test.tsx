// @vitest-environment jsdom
//
// The modal precedence ladder in src/app/keyboard.ts.
//
// Phase 4 replaced every `.hidden`-class query in that ladder with a
// store read. The order is the part that must not move: each layer
// returns, so whichever gate matches first owns the key and nothing
// below it ever runs. Getting that order wrong is invisible until two
// modals are open at once — a delete question over the worktree browser,
// a rename inside it — and then Escape closes the wrong thing, or
// destroys something.
//
// Table-driven over all nine layers. Each case opens its layer AND every
// layer below it, then presses Escape and asserts exactly one handler
// ran. Pinning it this way is what makes a reordered ladder fail: a gate
// that moved down is shadowed by the one that took its place.
import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  beforeEach,
  afterEach,
} from 'vitest';

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

vi.mock('../../src/app/view.js', () => ({
  switchTo: vi.fn(),
  setView: vi.fn(),
  gridSpatialMove: vi.fn(),
  shiftActiveProject: vi.fn(),
  restoreSession: vi.fn(),
  minimizeSession: vi.fn(),
  minimizeProject: vi.fn(),
  isSessionHidden: vi.fn(() => false),
}));

// The two layers that are not modals answer through their own modules,
// so they are faked here rather than driven through the store.
const inlineRenameOpen = { value: false };
const cancelInlineRename = vi.fn(() => true);
vi.mock('../../src/app/inline-rename.js', () => ({
  inlineRenameActive: () => inlineRenameOpen.value,
  cancelInlineRename: () => cancelInlineRename(),
  beginInlineRename: vi.fn(),
}));

const choiceOpen = { value: false };
const dismissChoiceDialog = vi.fn(() => true);
vi.mock('../../src/app/modals/choice-dialog.js', () => ({
  choiceDialogOpen: () => choiceOpen.value,
  dismissChoiceDialog: () => dismissChoiceDialog(),
  openChoiceDialog: vi.fn(),
  resolveChoiceDialog: vi.fn(),
}));

// The three close functions the ladder calls. Their open halves are not
// mocked: the store is the real thing, so a layer is opened exactly as
// the app opens it.
const closeSettings = vi.fn();
const closeWorktrees = vi.fn();
const closeHelpOverlay = vi.fn();
const closeCommandPalette = vi.fn();
vi.mock('../../src/app/modals/settings.js', () => ({
  closeSettings: () => closeSettings(),
  openSettings: vi.fn(),
  settingsEl: null,
}));
vi.mock('../../src/app/modals/worktrees.js', () => ({
  closeWorktrees: () => closeWorktrees(),
  openWorktrees: vi.fn(),
}));
vi.mock('../../src/app/modals/command-palette.js', () => ({
  closeCommandPalette: () => closeCommandPalette(),
  openCommandPalette: vi.fn(),
  paletteCommands: () => [],
}));
vi.mock('../../src/app/modals/help-overlay.js', () => ({
  closeHelpOverlay: () => closeHelpOverlay(),
  openHelpOverlay: vi.fn(),
  toggleHelpOverlay: vi.fn(),
}));

type Store = typeof import('../../src/store/store.js');
let openModal: Store['openModal'];
let resetStore: Store['resetStore'];
let state: typeof import('../../src/app/state.js').state;

// The dead-session overlay is the ninth layer: not a modal, but it does
// claim Enter/Escape from the active tile before the app bindings.
const dismissDead = vi.fn();

beforeAll(async () => {
  document.body.innerHTML = `
    <div id="terms"></div><ul id="projects"></ul>
    <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    <div id="launcher" class="hidden"></div>
    <div id="settings" class="hv-dialog hidden"></div>
    <div id="worktrees" class="hv-dialog hidden"></div>
    <div id="project-editor" class="hv-dialog hidden"></div>
    <div id="help-overlay" class="hv-dialog hidden"></div>
    <div id="choice-dialog" class="hv-dialog hidden"></div>
    <div id="command-palette" class="hidden"></div>`;
  ({ openModal, resetStore } = await import('../../src/store/store.js'));
  ({ state } = await import('../../src/app/state.js'));
  await import('../../src/app/keyboard.js');
});

function press(key: string, opts: KeyboardEventInit = {}) {
  const e = new KeyboardEvent('keydown', {
    key,
    bubbles: true,
    cancelable: true,
    ...opts,
  });
  window.dispatchEvent(e);
  return e;
}

// One handler per layer, in ladder order. `open` puts the layer up; `ran`
// reports whether that layer's Escape handler fired. A `passive` layer
// owns the keyboard without acting on Escape here — its own listener
// does — so what it must prove is that nothing BELOW it acted.
const LAYERS: {
  name: string;
  open: () => void;
  ran: () => boolean;
  passive?: boolean;
}[] = [
  {
    name: 'inline rename',
    open: () => {
      inlineRenameOpen.value = true;
    },
    ran: () => cancelInlineRename.mock.calls.length > 0,
  },
  {
    name: 'choice dialog',
    open: () => {
      choiceOpen.value = true;
    },
    ran: () => dismissChoiceDialog.mock.calls.length > 0,
  },
  // The launcher, the project editor and the command palette handle
  // their own keys from listeners on their own roots; the window handler
  // must bail out for them and consume nothing.
  {
    name: 'launcher',
    open: () =>
      openModal({
        id: 'launcher',
        req: {
          projectId: null,
          useWorktree: false,
          duplicateFrom: null,
          duplicateCwd: '',
          worktreePath: '',
          continueConversation: false,
        },
      }),
    ran: () => false,
    passive: true,
  },
  {
    name: 'project editor',
    open: () => openModal({ id: 'project-editor', editing: null }),
    ran: () => false,
    passive: true,
  },
  // Not passive for Escape: the palette's own listener lives on
  // #command-palette and only sees keys typed inside it, so the window
  // handler owns the close — otherwise anything that moves focus out
  // leaves the palette with no way to shut.
  {
    name: 'command palette',
    open: () => openModal({ id: 'command-palette' }),
    ran: () => closeCommandPalette.mock.calls.length > 0,
  },
  {
    name: 'settings',
    open: () => openModal({ id: 'settings' }),
    ran: () => closeSettings.mock.calls.length > 0,
  },
  {
    name: 'worktrees',
    open: () => openModal({ id: 'worktrees', projectId: 'p', projectName: '' }),
    ran: () => closeWorktrees.mock.calls.length > 0,
  },
  {
    name: 'help overlay',
    open: () => openModal({ id: 'help' }),
    ran: () => closeHelpOverlay.mock.calls.length > 0,
  },
  {
    name: 'dead-session overlay',
    open: () => {
      state.activeId = 'a';
      state.terms.set('a', {
        deadOverlayShown: true,
        _dismissDead: dismissDead,
        _closeDead: vi.fn(),
      } as never);
    },
    ran: () => dismissDead.mock.calls.length > 0,
  },
];

beforeEach(() => {
  resetStore();
  inlineRenameOpen.value = false;
  choiceOpen.value = false;
  state.activeId = null;
  state.terms.clear();
  for (const m of [
    cancelInlineRename,
    dismissChoiceDialog,
    closeSettings,
    closeWorktrees,
    closeHelpOverlay,
    closeCommandPalette,
    dismissDead,
  ]) {
    m.mockClear();
  }
});

afterEach(() => {
  state.terms.clear();
});

describe('every layer wins over the ones below it', () => {
  LAYERS.forEach((layer, i) => {
    it(`${layer.name} owns Escape over the ${LAYERS.length - i - 1} layer(s) below`, () => {
      // Open this layer and everything under it. Only the topmost may
      // act on the key.
      for (const l of LAYERS.slice(i)) l.open();
      const e = press('Escape');

      if (layer.passive) {
        // A passive layer owns the keyboard by NOT acting: its own
        // listener handles the key, so this handler must leave the event
        // alone — and, asserted below, must not let a lower layer act.
        expect(e.defaultPrevented).toBe(false);
      } else {
        expect(layer.ran()).toBe(true);
      }
      for (const below of LAYERS.slice(i + 1)) {
        expect(
          below.ran(),
          `${below.name} acted while ${layer.name} was open`,
        ).toBe(false);
      }
    });
  });
});

describe('a layer that owns its own keys consumes nothing here', () => {
  for (const name of ['launcher', 'project editor']) {
    it(`${name}: Escape is left to the modal's own listener`, () => {
      const layer = LAYERS.find((l) => l.name === name);
      layer?.open();
      const e = press('Escape');
      expect(e.defaultPrevented).toBe(false);
      for (const l of LAYERS) expect(l.ran()).toBe(false);
    });
  }
});

describe('with nothing open the key reaches the app bindings', () => {
  it('Escape is not consumed by any modal gate', () => {
    press('Escape');
    for (const l of LAYERS) expect(l.ran()).toBe(false);
  });
});

// The gate has to see the key BEFORE the element it is typed into does:
// the inline-rename input calls stopPropagation, so a bubble-phase
// listener would never learn the rename was open.
describe('the handler is registered capture-phase', () => {
  it('sees a key that a bubbling listener stops', () => {
    inlineRenameOpen.value = true;
    const sink = document.getElementById('terms') as HTMLElement;
    sink.addEventListener('keydown', (e) => e.stopPropagation());
    sink.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
        cancelable: true,
      }),
    );
    expect(cancelInlineRename).toHaveBeenCalled();
  });
});
