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
    EventsOn: (name: string, handler: (...a: unknown[]) => void) => {
      menuHandlers.set(name, handler);
    },
    WindowSetTitle: vi.fn(),
    ClipboardGetText: fn(),
  };
});

// The menu handlers keyboard.ts registers through EventsOn. On macOS
// the native accelerator swallows ⌘I before the webview, so THESE are
// the handlers that run there — the keydown branches never do.
const menuHandlers = new Map<string, (...a: unknown[]) => void>();

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

// The two idea modals. Partially mocked, unlike the four above:
// ideaInboxProjectId is the REAL one, reading the open modal entry off
// the store, because the ⌘I-from-the-inbox case below is exactly about
// which project id reaches the capture sheet.
const closeQuickIdea = vi.fn();
const openQuickIdea = vi.fn();
const closeIdeaInbox = vi.fn();
const openIdeaInbox = vi.fn();
vi.mock('../../src/app/modals/quick-idea.js', () => ({
  closeQuickIdea: () => closeQuickIdea(),
  openQuickIdea: (...a: unknown[]) => openQuickIdea(...a),
  initQuickIdea: vi.fn(),
  submitIdea: vi.fn(),
  IDEA_KINDS: ['idea', 'bug', 'feedback'],
}));
vi.mock('../../src/app/modals/idea-inbox.js', async (importOriginal) => ({
  ...(await importOriginal<
    typeof import('../../src/app/modals/idea-inbox.js')
  >()),
  closeIdeaInbox: () => closeIdeaInbox(),
  openIdeaInbox: (...a: unknown[]) => openIdeaInbox(...a),
}));

type Store = typeof import('../../src/store/store.js');
let openModal: Store['openModal'];
let resetStore: Store['resetStore'];
let state: typeof import('../../src/store/store.js').hiveStateView;

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
    <div id="quick-idea" class="hv-dialog hidden"></div>
    <div id="idea-inbox" class="hv-dialog hidden"></div>
    <div id="help-overlay" class="hv-dialog hidden"></div>
    <div id="choice-dialog" class="hv-dialog hidden"></div>
    <div id="command-palette" class="hidden"></div>`;
  ({ openModal, resetStore } = await import('../../src/store/store.js'));
  ({ hiveStateView: state } = await import('../../src/store/store.js'));
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
    name: 'quick idea',
    open: () => openModal({ id: 'quick-idea', projectId: 'p' }),
    ran: () => closeQuickIdea.mock.calls.length > 0,
  },
  {
    name: 'idea inbox',
    open: () =>
      openModal({ id: 'idea-inbox', projectId: 'p', projectName: '' }),
    ran: () => closeIdeaInbox.mock.calls.length > 0,
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
    closeQuickIdea,
    openQuickIdea,
    closeIdeaInbox,
    openIdeaInbox,
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
    const stop = (e: Event) => e.stopPropagation();
    sink.addEventListener('keydown', stop);
    sink.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
        cancelable: true,
      }),
    );
    expect(cancelInlineRename).toHaveBeenCalled();
    // The sink outlives this test — every later dispatch on #terms would
    // be swallowed by a listener that has nothing to do with them.
    sink.removeEventListener('keydown', stop);
  });
});

// ⌘I from inside the open inbox files another idea for the project the
// inbox is showing. Three things have to hold at once, and the first
// implementation got all three wrong: the inbox CLOSES (every
// .hv-dialog shares z-index 40 and #idea-inbox is later in index.html,
// so a sheet left over it would paint behind it), the sheet OPENS, and
// it opens on the INBOX's project rather than the focused session's.
// cmdOrCtrl() rejects an event carrying BOTH modifiers, so the chord
// has to be spelled with whichever one this platform treats as primary.
const primaryMod = (): KeyboardEventInit =>
  navigator.platform.toLowerCase().includes('mac') ||
  navigator.userAgent.toLowerCase().includes('mac')
    ? { metaKey: true }
    : { ctrlKey: true };

describe('⌘I inside the idea inbox', () => {
  it('closes the inbox and opens the sheet on the inbox’s project', () => {
    openModal({ id: 'idea-inbox', projectId: 'p9', projectName: 'other' });
    press('i', primaryMod());
    expect(closeIdeaInbox).toHaveBeenCalled();
    expect(openQuickIdea).toHaveBeenCalledWith('p9');
  });

  it('⇧⌘I from the capture sheet goes to the inbox on every platform', () => {
    // The gate returns unconditionally, so without its shift arm this
    // chord is swallowed by trapFocus — but only off macOS, where the
    // keydown path is the only path. On mac the native accelerator
    // reaches toggleIdeaInbox() regardless, and the two platforms
    // disagree about what ⇧⌘I does.
    openModal({ id: 'quick-idea', projectId: 'p1' });
    press('i', { ...primaryMod(), shiftKey: true });
    expect(closeQuickIdea).toHaveBeenCalled();
    expect(openIdeaInbox).toHaveBeenCalled();
  });

  it('⇧⌘I still closes the inbox rather than capturing', () => {
    openModal({ id: 'idea-inbox', projectId: 'p9', projectName: 'other' });
    press('i', { ...primaryMod(), shiftKey: true });
    expect(closeIdeaInbox).toHaveBeenCalled();
    expect(openQuickIdea).not.toHaveBeenCalled();
  });
});

// The macOS menu path. buildAppMenu binds ⌘I / ⇧⌘I as native
// accelerators, and AppKit consumes the chord before the webview sees a
// keydown — the same reason 'menu:keyboard-shortcuts' has to toggle
// rather than open. So on macOS these handlers, not the branches above,
// ARE the feature; a bare open() here would reopen the sheet over
// itself and discard the typed note.
describe('the ⌘I menu path behaves like the keydown path', () => {
  const menu = (name: string) => {
    const h = menuHandlers.get(name);
    if (!h) throw new Error(`no menu handler registered for ${name}`);
    h();
  };

  it('menu:quick-idea toggles the sheet closed when it is already open', () => {
    openModal({ id: 'quick-idea', projectId: 'p1' });
    menu('menu:quick-idea');
    expect(closeQuickIdea).toHaveBeenCalled();
    expect(openQuickIdea).not.toHaveBeenCalled();
  });

  it('menu:quick-idea from the inbox closes it and carries its project', () => {
    openModal({ id: 'idea-inbox', projectId: 'p9', projectName: 'other' });
    menu('menu:quick-idea');
    expect(closeIdeaInbox).toHaveBeenCalled();
    expect(openQuickIdea).toHaveBeenCalledWith('p9');
  });

  it('menu:idea-inbox toggles the inbox closed', () => {
    openModal({ id: 'idea-inbox', projectId: 'p9', projectName: 'other' });
    menu('menu:idea-inbox');
    expect(closeIdeaInbox).toHaveBeenCalled();
    expect(openIdeaInbox).not.toHaveBeenCalled();
  });

  it('menu:idea-inbox closes the capture sheet before opening', () => {
    openModal({ id: 'quick-idea', projectId: 'p1' });
    menu('menu:idea-inbox');
    expect(closeQuickIdea).toHaveBeenCalled();
    expect(openIdeaInbox).toHaveBeenCalled();
  });

  // The window handler's ladder returns above the ⌘I binding for every
  // layer below, so the menu — which punches through all of them on
  // macOS — must refuse for the same set, or mac gets a capture sheet
  // over a question about deleting a worktree and no other platform
  // does.
  it.each([
    ['a choice dialog', () => (choiceOpen.value = true)],
    ['an inline rename', () => (inlineRenameOpen.value = true)],
    ['the settings modal', () => openModal({ id: 'settings' })],
    [
      'the worktree browser',
      () => openModal({ id: 'worktrees', projectId: 'p', projectName: '' }),
    ],
    ['the help overlay', () => openModal({ id: 'help' })],
  ])('menu:quick-idea is a no-op under %s', (_name, open) => {
    open();
    menu('menu:quick-idea');
    expect(openQuickIdea).not.toHaveBeenCalled();
    expect(closeQuickIdea).not.toHaveBeenCalled();
  });
});
