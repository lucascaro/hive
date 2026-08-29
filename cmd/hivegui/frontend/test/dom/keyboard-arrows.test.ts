// @vitest-environment jsdom
//
// ⌘-arrow routing (src/app/keyboard.ts handleArrow).
//
// Three things are pinned here, all of which shipped broken:
//   • ⌘←/⌘→ (and ⇧⌘←/⇧⌘→) are start/end-of-line in the terminal, so in
//     focused mode the app must NOT preventDefault them.
//   • In grid mode the same keys are spatial navigation, and a vertical
//     arrow must move vertically — the menu path used to map ⌘↓ to a
//     horizontal move.
//   • ⇧⌘↑/↓ reorder inside the project and send an index into the
//     daemon's GLOBAL order list, wrapping within the project.
//
// view.ts is mocked so the assertions are about routing, not geometry:
// gridSpatialMove's real implementation needs layout jsdom doesn't have.
// The grid-floor behaviour runs against the real view.ts in the sibling
// view-floor.test.ts.
import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  beforeEach,
  type MockedFunction,
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

// Every view export keyboard.ts imports must be listed: a missing entry
// surfaces as an undefined call deep in a handler, not a clear failure.
vi.mock('../../src/app/view.js', async () => {
  const { state } = await import('../../src/app/state.js');
  return {
    switchTo: vi.fn(),
    setView: vi.fn(),
    gridSpatialMove: vi.fn(),
    shiftActiveProject: vi.fn(),
    restoreSession: vi.fn(),
    minimizeSession: vi.fn(),
    minimizeProject: vi.fn(),
    // Mirrors the real predicate (view.ts) rather than a bare vi.fn():
    // navGo / jumpToAttention branch on its return value.
    isSessionHidden: vi.fn((id: string) => {
      if (state.minimized.has(id)) return true;
      const s = state.sessions.find((x) => x.id === id);
      const pid = s?.projectId ?? s?.project_id ?? '';
      return !!pid && state.minimizedProjects.has(pid);
    }),
  };
});

type View = typeof import('../../src/app/view.js');
type Bridge = typeof import('../../src/bridge.js');

let state: typeof import('../../src/app/state.js').state;
let gridSpatialMove: MockedFunction<View['gridSpatialMove']>;
let switchTo: MockedFunction<View['switchTo']>;
let UpdateSession: MockedFunction<Bridge['UpdateSession']>;
let EventsOn: MockedFunction<Bridge['EventsOn']>;

beforeAll(async () => {
  // The keydown listener dereferences every modal before reaching the
  // shortcut chain; without these it throws before any binding runs.
  document.body.innerHTML = `
    <div id="terms"></div><ul id="projects"></ul><div id="status"></div>
    <div id="launcher" class="hidden"></div>
    <div id="project-editor" class="hidden"></div>
    <div id="command-palette" class="hidden"></div>
    <div id="help-overlay" class="hidden"></div>`;
  ({ state } = await import('../../src/app/state.js'));
  const view = await import('../../src/app/view.js');
  gridSpatialMove = vi.mocked(view.gridSpatialMove);
  switchTo = vi.mocked(view.switchTo);
  const bridge = await import('../../src/bridge.js');
  UpdateSession = vi.mocked(bridge.UpdateSession);
  EventsOn = vi.mocked(bridge.EventsOn);
  await import('../../src/app/keyboard.js');
});

// cmdOrCtrl() reads the real platform: metaKey on mac, ctrlKey elsewhere.
const primary = /mac|iphone|ipad/i.test(navigator.platform)
  ? { metaKey: true }
  : { ctrlKey: true };

function press(key: string, opts: KeyboardEventInit = {}) {
  const e = new KeyboardEvent('keydown', {
    key,
    bubbles: true,
    cancelable: true,
    ...primary,
    ...opts,
  });
  window.dispatchEvent(e);
  return e;
}

beforeEach(() => {
  gridSpatialMove.mockClear();
  switchTo.mockClear();
  UpdateSession.mockClear();
  // Global display order: [a0, a1, b0, b1, b2] — indices 0..4, which is
  // the index space the daemon's Update expects.
  state.projects = [{ id: 'A' }, { id: 'B' }];
  state.sessions = [
    { id: 'a0', project_id: 'A', order: 0 },
    { id: 'a1', project_id: 'A', order: 1 },
    { id: 'b0', project_id: 'B', order: 2 },
    { id: 'b1', project_id: 'B', order: 3 },
    { id: 'b2', project_id: 'B', order: 4 },
  ];
  state.activeId = 'b1';
  state.view = 'single';
  state.minimized = new Set();
  state.attention = new Set();
});

describe('horizontal arrows in focused mode belong to the terminal', () => {
  for (const key of ['ArrowLeft', 'ArrowRight']) {
    for (const shift of [false, true]) {
      it(`${shift ? 'shift+' : ''}cmd+${key} is not consumed`, () => {
        const e = press(key, { shiftKey: shift });
        expect(e.defaultPrevented).toBe(false);
        expect(state.activeId).toBe('b1');
        expect(switchTo).not.toHaveBeenCalled();
        expect(UpdateSession).not.toHaveBeenCalled();
        expect(gridSpatialMove).not.toHaveBeenCalled();
      });
    }
  }
});

describe('arrows in grid mode are spatial navigation', () => {
  beforeEach(() => {
    state.view = 'grid-all';
  });

  it('cmd+ArrowLeft is consumed and moves the active tile', () => {
    const e = press('ArrowLeft');
    expect(e.defaultPrevented).toBe(true);
    expect(gridSpatialMove).toHaveBeenCalledWith(-1, 0);
  });

  it('cmd+ArrowDown moves DOWN, not right', () => {
    press('ArrowDown');
    expect(gridSpatialMove).toHaveBeenCalledWith(0, +1);
  });

  it('cmd+ArrowRight moves right', () => {
    press('ArrowRight');
    expect(gridSpatialMove).toHaveBeenCalledWith(+1, 0);
  });
});

describe('shift+cmd vertical arrows reorder within the project', () => {
  it('moves down to the next sibling’s global index', () => {
    press('ArrowDown', { shiftKey: true });
    // b1 → swaps with b2, whose current global index is 4.
    expect(UpdateSession).toHaveBeenCalledWith('b1', '', '', 4);
  });

  it('moves up to the previous sibling’s global index', () => {
    press('ArrowUp', { shiftKey: true });
    expect(UpdateSession).toHaveBeenCalledWith('b1', '', '', 2);
  });

  it("wraps from a project's last session to its first", () => {
    state.activeId = 'b2';
    press('ArrowDown', { shiftKey: true });
    expect(UpdateSession).toHaveBeenCalledWith('b2', '', '', 2);
  });

  it("wraps from a project's first session to its last", () => {
    state.activeId = 'b0';
    press('ArrowUp', { shiftKey: true });
    expect(UpdateSession).toHaveBeenCalledWith('b0', '', '', 4);
  });

  it('never targets a session in another project', () => {
    // a0/a1 are the whole of project A: down from a1 wraps to a0 (0),
    // it must not walk into project B.
    state.activeId = 'a1';
    press('ArrowDown', { shiftKey: true });
    expect(UpdateSession).toHaveBeenCalledWith('a1', '', '', 0);
  });
});

describe('Session-menu arrow actions', () => {
  const fire = (name: string) => {
    const call = EventsOn.mock.calls.find(([n]) => n === name);
    if (!call) throw new Error(`no handler registered for ${name}`);
    (call[1] as () => void)();
  };

  it('menu:move-session-forward reorders while in grid mode', () => {
    state.view = 'grid-all';
    fire('menu:move-session-forward');
    expect(UpdateSession).toHaveBeenCalledWith('b1', '', '', 4);
    expect(gridSpatialMove).not.toHaveBeenCalled();
  });

  it('menu:next-session navigates vertically in grid mode', () => {
    state.view = 'grid-all';
    fire('menu:next-session');
    expect(gridSpatialMove).toHaveBeenCalledWith(0, +1);
  });

  it('menu:next-session switches session in focused mode', () => {
    fire('menu:next-session');
    expect(switchTo).toHaveBeenCalledWith('b2');
  });
});
