// @vitest-environment jsdom
//
// #451: a session parked on a worktree decision must be answerable
// from the keyboard. The tile's "Answer…" button is mouse-only, Escape
// deliberately dismisses without answering, and xterm's textarea
// swallows Tab — so without an explicit route a keyboard-only user is
// stuck on a session that cannot proceed and cannot be answered.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';

const raiseWorktreeChoice = vi.fn();

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
    ListAgents: fn(),
    ListCustomAgents: fn(),
    SaveCustomAgents: fn(),
    GetAgentSettings: fn(),
    SaveAgentSettings: fn(),
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
    ClipboardGetText: fn(),
    ResolvePrompt: fn(),
    ResolveWorktreeChoice: fn(),
    StateDirID: fn(),
    LogFrontend: vi.fn(),
    EventsOn: vi.fn(),
    WindowSetTitle: vi.fn(),
  };
});

// view.ts pulls xterm and the whole render graph into the import
// chain, which jsdom cannot stand up (canvas, ResizeObserver). Stub it
// the way nav-history.test.ts does — this test is about key routing.
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

// keyboard.ts imports raiseWorktreeChoice from events.ts; stub the
// module so this test pins the ROUTING, not the dialog.
vi.mock('../../src/app/events.js', () => ({
  clearAttention: vi.fn(),
  raiseWorktreeChoice,
}));

let store: typeof import('../../src/store/store.js');

beforeAll(async () => {
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  store = await import('../../src/store/store.js');
  const kb = await import('../../src/app/keyboard.js');
  kb.initKeyboard({
    withoutNavHistory: (fn: () => void) => fn(),
    bumpFontSize: () => {},
    resetFontSize: () => {},
    focusActiveTerm: () => {},
  });
});

function press(key: string) {
  window.dispatchEvent(
    new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }),
  );
}

describe('blocked tile keyboard route', () => {
  beforeEach(() => {
    raiseWorktreeChoice.mockClear();
  });

  it('Enter re-raises the question for a parked session', () => {
    store.setSessions([
      {
        id: 's-blocked',
        name: 'stale-brook',
        alive: false,
        phase: 'blocked',
      },
    ]);
    store.setActiveId('s-blocked');

    press('Enter');

    expect(raiseWorktreeChoice).toHaveBeenCalledWith('s-blocked');
  });

  it('Enter does nothing for an ordinary session', () => {
    store.setSessions([
      { id: 's-ready', name: 'ready', alive: true, phase: '' },
    ]);
    store.setActiveId('s-ready');

    press('Enter');

    expect(raiseWorktreeChoice).not.toHaveBeenCalled();
  });
});
