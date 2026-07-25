// @vitest-environment jsdom
//
// ⌘B / ⇧⌘B against the REAL switchTo → setActive chain.
//
// The sibling attention-jump.test.js mocks view.js to isolate the
// return-slot logic, which means the thing ⌘B's correctness actually
// rests on — setActive clearing the target's attention flag — is
// simulated there, not executed. If setActive stopped deleting from
// state.attention, ⌘B would leave the bell pulsing forever and re-visit
// the same session on every press, and every mocked test would still be
// green. So this file wires the real modules with stub *dependencies*
// (no xterm, no Wails) and asserts the end-to-end effects.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';

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

let state, jumpToAttention, jumpBack, initView, initFocus, setActive;

// Each session needs a SessionTerm-shaped stub: setActive and renderGrid
// reach for `.host.classList` to move the .attention / .active classes.
function fakeTerm() {
  return {
    host: document.createElement('div'),
    ensureAttached() {},
    show() {},
    hide() {},
    fit() {},
    focus() {},
  };
}

beforeAll(async () => {
  // view.js installs a container ResizeObserver at module load; jsdom
  // has no implementation. The grid-reflow path it drives is not what
  // this file tests, so a no-op stub is enough.
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  document.body.innerHTML = `
    <div id="app"><ul id="projects"></ul><div id="status"></div>
    <div id="terms"></div><div id="minimized-tray"></div>
    <div id="empty-state"></div></div>`;
  ({ state } = await import('../../src/app/state.js'));
  const view = await import('../../src/app/view.js');
  const focus = await import('../../src/app/focus.js');
  ({ initView } = view);
  ({ initFocus, setActive } = focus);
  ({ jumpToAttention, jumpBack } = await import('../../src/app/keyboard.js'));

  // Real view.js + focus.js, stubbed at their injection seams.
  initFocus({ ensureTerm: (info) => state.terms.get(info.id) });
  initView({
    ensureTerm: (info) => state.terms.get(info.id),
    setActive,
    focusActiveTerm: () => {},
    scrollTrace: {
      rec: Object.assign(() => {}, { enabled: false }),
      count: () => {},
    },
  });
});

beforeEach(() => {
  state.projects = [{ id: 'p1' }, { id: 'p2' }];
  state.sessions = [
    { id: 'a', project_id: 'p1', order: 0 },
    { id: 'b', project_id: 'p1', order: 1 },
    { id: 'z', project_id: 'p2', order: 0 },
  ];
  state.terms = new Map(state.sessions.map((s) => [s.id, fakeTerm()]));
  state.activeId = 'a';
  state.attention = new Set();
  state.attentionReturnId = null;
  state.attentionRestored = new Set();
  state.minimized = new Set();
  state.view = 'single';
  state.currentProjectId = 'p1';
});

describe('⌘B through the real switchTo/setActive chain', () => {
  it('clears the flag on the session it lands on', () => {
    state.attention = new Set(['b']);
    jumpToAttention();
    expect(state.activeId).toBe('b');
    // The assertion the mocked suite cannot make: setActive really ran.
    expect(state.attention.has('b')).toBe(false);
  });

  it('drops the .attention class from the landed-on tile', () => {
    state.attention = new Set(['b']);
    state.terms.get('b').host.classList.add('attention');
    jumpToAttention();
    expect(state.terms.get('b').host.classList.contains('attention')).toBe(
      false,
    );
  });

  it('syncs currentProjectId when the flagged session is in another project', () => {
    state.attention = new Set(['z']);
    jumpToAttention();
    expect(state.activeId).toBe('z');
    expect(state.currentProjectId).toBe('p2');
  });

  it('retargets the grid scope when jumping across projects in grid-project view', () => {
    state.view = 'grid-project';
    state.gridProjectId = 'p1';
    state.attention = new Set(['z']);
    jumpToAttention();
    expect(state.gridProjectId).toBe('p2');
  });

  it('does not re-visit a session whose flag it already cleared', () => {
    state.attention = new Set(['b']);
    jumpToAttention(); // a → b, flag cleared for real
    jumpToAttention(); // nothing left to jump to
    expect(state.activeId).toBe('b');
  });

  it('clears a stale flag on the active session instead of claiming none exist', () => {
    // onSessionDeath flags the active session unconditionally; nextAttentionId
    // skips the active session, so the row would pulse forever otherwise.
    state.attention = new Set(['a']);
    jumpToAttention();
    expect(state.activeId).toBe('a');
    expect(state.attention.has('a')).toBe(false);
  });
});

describe('minimized sessions round-trip', () => {
  it('restores a minimized flagged session on the way in', () => {
    state.minimized = new Set(['b']);
    state.attention = new Set(['b']);
    jumpToAttention();
    expect(state.minimized.has('b')).toBe(false);
    expect(state.activeId).toBe('b');
  });

  it('re-minimizes it when you jump back', () => {
    state.minimized = new Set(['b']);
    state.attention = new Set(['b']);
    jumpToAttention(); // a → b, b restored
    jumpBack(); // back to a, b returns to the tray
    expect(state.activeId).toBe('a');
    expect(state.minimized.has('b')).toBe(true);
    expect(state.attentionRestored.size).toBe(0);
  });

  it('leaves a session alone if it was not minimized to begin with', () => {
    state.attention = new Set(['b']);
    jumpToAttention();
    jumpBack();
    expect(state.minimized.has('b')).toBe(false);
  });

  it('re-minimizes every session restored across a multi-hop round', () => {
    state.minimized = new Set(['b', 'z']);
    state.attention = new Set(['b', 'z']);
    jumpToAttention(); // a → b (restored)
    jumpToAttention(); // b → z (restored)
    jumpBack(); // home to a; both go back in the tray
    expect(state.activeId).toBe('a');
    expect(state.minimized.has('b')).toBe(true);
    expect(state.minimized.has('z')).toBe(true);
  });

  it('does not re-minimize a restored session that was killed while away', () => {
    state.minimized = new Set(['b']);
    state.attention = new Set(['b']);
    jumpToAttention();
    state.sessions = state.sessions.filter((s) => s.id !== 'b'); // b dies
    jumpBack();
    // A dead id in state.minimized would strand a chip in the tray.
    expect(state.minimized.has('b')).toBe(false);
  });

  it('keeps you visible when the anchor died: releases the round, no self-minimize', () => {
    state.minimized = new Set(['b']);
    state.attention = new Set(['b']);
    jumpToAttention(); // anchor = a, b restored, now sitting in b
    state.sessions = state.sessions.filter((s) => s.id !== 'a'); // anchor dies
    jumpBack(); // "nowhere to jump back to"
    // b must NOT go back in the tray — it is the session under the user's
    // cursor and there is nowhere to send them. Minimizing it here would
    // hide the tile they are looking at.
    expect(state.activeId).toBe('b');
    expect(state.minimized.has('b')).toBe(false);
    expect(state.attentionReturnId).toBeNull();
    expect(state.attentionRestored.size).toBe(0);
  });
});
