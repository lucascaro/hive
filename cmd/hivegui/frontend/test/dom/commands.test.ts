// @vitest-environment jsdom
//
// The command bus (spec 478): one id per action, shared by the key
// bindings, the native menu and the command palette.
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

// Menu handlers, captured the way the Wails runtime would deliver them.
const menuHandlers = new Map<string, (...a: unknown[]) => void>();
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
const closeActiveSession = vi.fn();
vi.mock('../../src/app/undo-close.js', () => ({
  closeActiveSession: () => closeActiveSession(),
  reopenLastClosedSession: vi.fn(),
}));
const openQuickIdea = vi.fn();
vi.mock('../../src/app/modals/quick-idea.js', () => ({
  openQuickIdea: (...a: unknown[]) => openQuickIdea(...a),
  closeQuickIdea: vi.fn(),
  initQuickIdea: vi.fn(),
  submitIdea: vi.fn(),
  IDEA_KINDS: ['idea', 'bug', 'feedback'],
}));

type Registry = typeof import('../../src/app/command-registry.js');
let registry: Registry;
let commands: typeof import('../../src/app/commands.js');
let scopes: typeof import('../../src/app/key-scopes.js');
let palette: typeof import('../../src/app/modals/command-palette.js');
let chord: typeof import('../../src/lib/chord.js');
let store: typeof import('../../src/store/store.js');

beforeAll(async () => {
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  store = await import('../../src/store/store.js');
  await import('../../src/app/keyboard.js');
  registry = await import('../../src/app/command-registry.js');
  commands = await import('../../src/app/commands.js');
  scopes = await import('../../src/app/key-scopes.js');
  palette = await import('../../src/app/modals/command-palette.js');
  chord = await import('../../src/lib/chord.js');
});

beforeEach(() => {
  store.resetStore();
  closeActiveSession.mockClear();
  openQuickIdea.mockClear();
});

describe('every id resolves', () => {
  it('each menu event runs a registered command', () => {
    for (const [event, id] of Object.entries(commands.MENU_COMMANDS)) {
      expect(registry.findCommand(id), `${event} → ${id}`).toBeDefined();
      expect(menuHandlers.has(event), `${event} not subscribed`).toBe(true);
    }
  });

  it('each key binding names a registered command, on both platforms', () => {
    for (const scope of scopes.KEY_SCOPES) {
      if (scope.id === 'plugins') continue;
      for (const b of scope.bindings()) {
        for (const mac of [true, false]) {
          // Every chord string parses, too: a typo throws here, not at a
          // user's keypress.
          for (const s of chord.chordsFor(b.keys, mac))
            chord.parseChord(s, mac);
        }
        if (b.command !== null) {
          expect(
            registry.findCommand(b.command),
            `${scope.id} → ${b.command}`,
          ).toBeDefined();
        }
      }
    }
  });
});

describe('the palette', () => {
  it('lists the same commands, in the same order, as before the bus', () => {
    expect(palette.paletteCommands().map((c) => c.id)).toEqual([
      'new-project',
      'new-session',
      'new-session-worktree',
      'duplicate-session',
      'duplicate-session-choose-tool',
      'restart-session',
      'take-over-session',
      'hand-back-session',
      'delete-project',
      'worktrees',
      'whats-new',
      'help',
      'quick-idea',
      'idea-inbox',
      'close-session',
      'reopen-closed-session',
      'new-window',
      'open-os-terminal',
      'close-window',
      'toggle-sidebar',
      'toggle-project-grid',
      'toggle-all-grid',
      'toggle-activity',
      'activity-grid',
      'find-in-session',
      'focus-active-session',
      'zoom-in',
      'zoom-out',
      'zoom-reset',
      'next-session',
      'prev-session',
      'nav-back',
      'nav-forward',
      'next-attention',
      'jump-back',
      'move-forward',
      'move-backward',
      'next-project',
      'prev-project',
      'keyboard-shortcuts',
      'settings',
      'reload-gui',
      'restart-hive',
      ...Array.from({ length: 9 }, (_, i) => `switch-${i + 1}`),
    ]);
  });

  it('lists plugin commands after every core one, whatever registered first', () => {
    const offPlugin = registry.registerCommandSource(
      () => [{ id: 'plugin:x:y', title: 'Plugin Y', run: () => {} }],
      'plugins',
    );
    const offCore = registry.registerCommandSource(
      () => [{ id: 'late-core', title: 'Late Core', run: () => {} }],
      'core',
    );
    try {
      const ids = palette.paletteCommands().map((c) => c.id);
      expect(ids.indexOf('late-core')).toBeLessThan(ids.indexOf('plugin:x:y'));
      expect(ids.at(-1)).toBe('plugin:x:y');
    } finally {
      offPlugin();
      offCore();
    }
    expect(registry.findCommand('plugin:x:y')).toBeUndefined();
  });
});

describe('one command per action', () => {
  it('close-session: the key, the menu item and the palette row all close', () => {
    window.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'w',
        ctrlKey: true,
        cancelable: true,
      }),
    );
    menuHandlers.get('menu:close-session')?.();
    palette
      .paletteCommands()
      .find((c) => c.id === 'close-session')
      ?.run();
    expect(closeActiveSession).toHaveBeenCalledTimes(3);
  });

  it("E: the palette's Capture Idea is gated like the key and the menu", () => {
    store.openModal({ id: 'settings' });
    palette
      .paletteCommands()
      .find((c) => c.id === 'quick-idea')
      ?.run();
    menuHandlers.get('menu:quick-idea')?.();
    expect(openQuickIdea).not.toHaveBeenCalled();
    store.resetStore();
    palette
      .paletteCommands()
      .find((c) => c.id === 'quick-idea')
      ?.run();
    expect(openQuickIdea).toHaveBeenCalledOnce();
  });
});
