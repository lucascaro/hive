// @vitest-environment jsdom
//
// Session back/forward (Ctrl+- / Ctrl+Shift+-) end-to-end through the
// real recording hook: app/focus.js setActive → lib/nav-history.
//
// The hook lives in setActive rather than switchTo precisely because
// four selection paths (tile mousedown, gridSpatialMove,
// shiftActiveProject, minimizeSession) never call switchTo. The
// "records a bare setActive" case below is the regression guard for
// that decision — a switchTo-based implementation passes every other
// test in this file and silently loses clicks on grid tiles.
import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  beforeEach,
  afterEach,
  type MockedFunction,
} from 'vitest';
import type { SessionInfo } from '../../src/app/state.js';

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

// The real switchTo owns xterm/DOM work we don't need here, but it
// MUST still funnel through setActive — that is the contract this
// feature depends on (view.js:51). The stub preserves exactly that
// edge and nothing else.
vi.mock('../../src/app/view.js', async () => {
  const { setActive } = await import('../../src/app/focus.js');
  const { state } = await import('../../src/app/state.js');
  return {
    switchTo: vi.fn((id: string | null) => setActive(id)),
    setView: vi.fn(),
    gridSpatialMove: vi.fn(),
    shiftActiveProject: vi.fn(),
    // Mirrors the real restoreSession (view.js): un-minimize, then switchTo.
    restoreSession: vi.fn((id: string | null) => {
      if (id) state.minimized.delete(id);
      setActive(id);
    }),
    minimizeSession: vi.fn(),
  };
});

type View = typeof import('../../src/app/view.js');
type Focus = typeof import('../../src/app/focus.js');
type Keyboard = typeof import('../../src/app/keyboard.js');

let state: typeof import('../../src/app/state.js').state;
let setActive: Focus['setActive'];
let withoutNavHistory: Focus['withoutNavHistory'];
let navBack: Keyboard['navBack'];
let navForward: Keyboard['navForward'];
// vi.mocked over a hand-written signature: view.js is vi.mock'd above, so
// the mock types stay pinned to the real exports.
let switchTo: MockedFunction<View['switchTo']>;
let restoreSession: MockedFunction<View['restoreSession']>;
let isMac: boolean;

const session = (id: string): SessionInfo => ({
  id,
  name: id,
  projectId: 'p1',
  order: 0,
});

beforeAll(async () => {
  // The keydown handler dereferences every modal element without null
  // guards; keyboard.ts throws on import-time wiring without them.
  document.body.innerHTML = `
    <div id="terms"></div><ul id="projects"></ul><div id="status"></div>
    <div id="launcher" class="hidden"></div>
    <div id="project-editor" class="hidden"></div>
    <div id="command-palette" class="hidden"></div>
    <div id="help-overlay" class="hidden"></div>`;
  ({ state } = await import('../../src/app/state.js'));
  ({ setActive, withoutNavHistory } = await import('../../src/app/focus.js'));
  const view = await import('../../src/app/view.js');
  switchTo = vi.mocked(view.switchTo);
  restoreSession = vi.mocked(view.restoreSession);
  const kb = await import('../../src/app/keyboard.js');
  ({ navBack, navForward } = kb);
  // main.js injects the focus pipeline into keyboard.ts so the modules
  // stay acyclic; this harness has to do the same or the suppression
  // that stops back/forward ping-ponging is never wired.
  kb.initKeyboard({
    withoutNavHistory,
    bumpFontSize: () => {},
    resetFontSize: () => {},
    focusActiveTerm: () => {},
  });
  ({ isMac } = await import('../../src/lib/platform.js'));
});

// The nowhere-to-go paths call the real flashStatus, which arms a
// 2500ms revert timer against a document later torn down.
beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.runOnlyPendingTimers();
  vi.useRealTimers();
});

beforeEach(() => {
  switchTo.mockClear();
  state.sessions = [session('a'), session('b'), session('c'), session('d')];
  state.activeId = null;
  state.nav.back.length = 0;
  state.nav.fwd.length = 0;
  state.minimized.clear();
  restoreSession.mockClear();
});

describe('recording', () => {
  it('records every switch, in order', () => {
    switchTo('a');
    switchTo('b');
    switchTo('c');
    expect(state.activeId).toBe('c');
    expect(state.nav.back).toEqual(['a', 'b']);
  });

  it('records a bare setActive — the tile-mousedown / grid-arrow path', () => {
    // session-term.js:386 and view.js:262 call setActive directly and
    // never touch switchTo. Losing these is the failure mode this
    // design exists to prevent, so assert on setActive itself.
    setActive('a');
    setActive('b');
    expect(state.nav.back).toEqual(['a']);
    expect(state.activeId).toBe('b');
  });

  it('does not record re-selecting the already-active session', () => {
    switchTo('a');
    setActive('a');
    setActive('a');
    expect(state.nav.back).toEqual([]);
  });
});

describe('navBack / navForward', () => {
  it('walks back through visited sessions, then forward again', () => {
    switchTo('a');
    switchTo('b');
    switchTo('c');

    navBack();
    expect(state.activeId).toBe('b');
    navBack();
    expect(state.activeId).toBe('a');
    navForward();
    expect(state.activeId).toBe('b');
    navForward();
    expect(state.activeId).toBe('c');
  });

  it('does not record the replay — no ping-pong between two sessions', () => {
    // Without the withoutNavHistory suppression, navBack would push the
    // session it just left, and repeated navBack would oscillate a↔b
    // forever instead of walking further back.
    switchTo('a');
    switchTo('b');
    switchTo('c');

    navBack();
    navBack();
    navBack();
    expect(state.activeId).toBe('a'); // stopped, did not bounce back to b
    expect(state.nav.back).toEqual([]);
  });

  it('truncates the forward branch when you navigate somewhere new', () => {
    switchTo('a');
    switchTo('b');
    switchTo('c');
    navBack(); // at b, forward holds c

    switchTo('d');
    expect(state.nav.fwd).toEqual([]);
    navForward();
    expect(state.activeId).toBe('d'); // nowhere forward to go
  });

  it('skips a session killed while you were away', () => {
    switchTo('a');
    switchTo('b');
    switchTo('c');
    state.sessions = state.sessions.filter((s) => s.id !== 'b');

    navBack();
    expect(state.activeId).toBe('a');
  });

  it('does nothing when there is no history', () => {
    switchTo('a');
    navBack();
    expect(state.activeId).toBe('a');
    expect(switchTo).toHaveBeenCalledTimes(1); // only the initial switch
  });

  it('does nothing when there is nothing forward', () => {
    switchTo('a');
    switchTo('b');
    navForward();
    expect(state.activeId).toBe('b');
  });

  it('is reachable from a real keydown, and swallows the event', () => {
    // Placement guard. keyboard.ts gates most bindings behind
    // cmdOrCtrl(), which rejects plain Ctrl on macOS — a dispatch added
    // after that gate would never fire there. This asserts the binding
    // is wired ahead of it, and that preventDefault runs so the chord
    // never reaches xterm's handler.
    switchTo('a');
    switchTo('b');

    const e = new KeyboardEvent('keydown', {
      key: '-',
      code: 'Minus',
      ctrlKey: true,
      altKey: !isMac,
      bubbles: true,
      cancelable: true,
    });
    window.dispatchEvent(e);

    expect(state.activeId).toBe('a');
    expect(e.defaultPrevented).toBe(true);
  });

  it('leaves the platform zoom chord alone', () => {
    // On Windows/Linux plain Ctrl+- is bumpFontSize(-1); on macOS it is
    // ours. Assert the chord that must NOT navigate on this platform.
    switchTo('a');
    switchTo('b');

    // mac: ⌘- is zoom; non-mac: Ctrl+- is zoom.
    const zoom = new KeyboardEvent('keydown', {
      key: '-',
      code: 'Minus',
      metaKey: isMac,
      ctrlKey: !isMac,
      altKey: false,
      bubbles: true,
      cancelable: true,
    });
    window.dispatchEvent(zoom);

    expect(state.activeId).toBe('b'); // did not navigate
  });

  it('un-minimizes a session it goes back into', () => {
    // gridScopeSessions filters state.minimized, so a bare switchTo
    // would make the session active with no tile: sidebar selection
    // moves, nothing appears, and keyboard focus lands on <body> where
    // keystrokes are silently dropped. ⌘B already restores on the way
    // in (keyboard.ts jumpToAttention); back/forward must match.
    switchTo('a');
    state.minimized.add('a');
    switchTo('b');
    switchTo('c');

    navBack();
    expect(state.activeId).toBe('b');
    navBack();
    expect(state.activeId).toBe('a');
    expect(state.minimized.has('a')).toBe(false);
    expect(restoreSession).toHaveBeenCalledWith('a');
  });

  it('un-minimizes on the forward leg too', () => {
    switchTo('a');
    switchTo('b');
    state.minimized.add('b');
    navBack();
    expect(state.activeId).toBe('a');

    navForward();
    expect(state.activeId).toBe('b');
    expect(state.minimized.has('b')).toBe(false);
  });

  it('leaves a non-minimized target alone', () => {
    switchTo('a');
    switchTo('b');
    navBack();
    expect(restoreSession).not.toHaveBeenCalled();
  });

  it('carries the current project across a back into another project', () => {
    // setActive syncs currentProjectId from the target session, so a
    // back into another project must move the sidebar's project too.
    state.sessions = [
      { id: 'a', name: 'a', projectId: 'p1', order: 0 },
      { id: 'z', name: 'z', projectId: 'p2', order: 0 },
    ];
    switchTo('a');
    switchTo('z');
    expect(state.currentProjectId).toBe('p2');

    navBack();
    expect(state.activeId).toBe('a');
    expect(state.currentProjectId).toBe('p1');
  });
});
