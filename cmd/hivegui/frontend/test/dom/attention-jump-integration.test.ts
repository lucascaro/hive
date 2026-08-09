// @vitest-environment jsdom
//
// ⌘B / ⇧⌘B against the REAL switchTo → setActive chain.
//
// The sibling attention-jump.test.ts mocks view.js to isolate the
// return-slot logic, which means the thing ⌘B's correctness actually
// rests on — setActive clearing the target's attention flag — is
// simulated there, not executed. If setActive stopped deleting from
// state.attention, ⌘B would leave the bell pulsing forever and re-visit
// the same session on every press, and every mocked test would still be
// green. So this file wires the real modules with stub *dependencies*
// (no xterm, no Wails) and asserts the end-to-end effects.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { createScrollTrace } from '../../src/lib/scroll-debug.js';
import type { TermTile } from '../../src/app/state.js';

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

let state: typeof import('../../src/app/state.js').state;
let jumpToAttention: typeof import('../../src/app/keyboard.js').jumpToAttention;
let jumpBack: typeof import('../../src/app/keyboard.js').jumpBack;
let initView: typeof import('../../src/app/view.js').initView;
let setActive: typeof import('../../src/app/focus.js').setActive;

// Each session needs a SessionTerm-shaped stub: setActive and renderGrid
// reach for `.host.classList` to move the .attention / .active classes.
// Every other member is a no-op, but they are all spelled out so the
// stub really satisfies TermTile rather than being cast into it — a
// member added to the interface then shows up here as an error instead
// of as a runtime TypeError in whichever path first reaches for it.
function fakeTerm(): TermTile {
  return {
    host: document.createElement('div'),
    attached: true,
    needsReattach: false,
    deadOverlayShown: false,
    ensureAttached() {},
    show() {},
    hide() {},
    rebaselineReplayCols() {},
    _onBodyResize() {},
    setInfo() {},
    setProject() {},
    setDead() {},
    writeData() {},
    destroy() {},
    _closeDead() {},
    _dismissDead() {},
  };
}

// term lookups in the assertions below: every id asserted on is in
// state.sessions, so the tile exists — this turns the `| undefined`
// into a failed expect() rather than a cast that hides a real miss.
function tile(id: string): TermTile {
  const t = state.terms.get(id);
  expect(t, `no term stub for ${id}`).toBeDefined();
  return t as TermTile;
}

beforeAll(async () => {
  // view.js installs a container ResizeObserver at module load; jsdom
  // has no implementation. The grid-reflow path it drives is not what
  // this file tests, so a no-op stub is enough.
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  document.body.innerHTML = `
    <div id="app"><ul id="projects"></ul><div id="status"></div>
    <div id="terms"></div><div id="minimized-tray"></div>
    <div id="empty-state"></div></div>`;
  ({ state } = await import('../../src/app/state.js'));
  const view = await import('../../src/app/view.js');
  const focus = await import('../../src/app/focus.js');
  ({ initView } = view);
  ({ setActive } = focus);
  ({ jumpToAttention, jumpBack } = await import('../../src/app/keyboard.js'));

  // Real view.js + focus.js, with view stubbed at its injection seam.
  // ensureTerm is declared to return a TermTile; beforeEach populates
  // state.terms for every session, so tile() asserts rather than casts.
  initView({
    ensureTerm: (info) => tile(info.id),
    setActive,
    focusActiveTerm: () => {},
    // A real disabled tracer, not a hand-rolled `{ rec }` literal, so the
    // stub can't drift out of Pick<ScrollTrace, 'rec' | 'count'>.
    scrollTrace: createScrollTrace({ enabled: false }),
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
    tile('b').host.classList.add('attention');
    jumpToAttention();
    expect(tile('b').host.classList.contains('attention')).toBe(false);
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
