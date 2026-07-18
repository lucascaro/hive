// @vitest-environment jsdom
//
// Return-slot semantics for ⌘B / ⇧⌘B (src/app/keyboard.js).
//
// ⇧⌘B means "back to the work I was doing before the FIRST ⌘B", so the
// anchor is written only when the slot is empty and released on use.
// The cases below pin that down against the tempting simplification of
// re-anchoring on every jump — which would walk the return target
// forward and strand the user one interruption short of their work.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';

vi.mock('../../src/bridge.js', () => {
  const fn = () => vi.fn(() => Promise.resolve());
  return {
    ConnectControl: fn(), OpenSession: fn(), CloseAttach: fn(),
    WriteStdin: fn(), ResizeSession: fn(), RequestScrollbackReplay: fn(),
    CreateSession: fn(), DuplicateSession: fn(), KillSession: fn(),
    RestartSession: fn(), UpdateSession: fn(), ListAgents: fn(),
    CreateProject: fn(), KillProject: fn(), UpdateProject: fn(),
    LaunchDir: fn(), PickDirectory: fn(), OpenNewWindow: fn(), CloseWindow: fn(),
    IsGitRepo: fn(), OpenURL: fn(), OpenTerminalAt: fn(),
    Notify: fn(), Confirm: fn(), RestartDaemon: fn(),
    CheckForUpdate: fn(), SetClipboardText: fn(),
    EventsOn: vi.fn(), WindowSetTitle: vi.fn(), ClipboardGetText: fn(),
  };
});

// switchTo owns real DOM/xterm work; the slot logic is what's under test.
vi.mock('../../src/app/view.js', () => ({
  switchTo: vi.fn(),
  setView: vi.fn(),
  gridSpatialMove: vi.fn(),
  shiftActiveProject: vi.fn(),
}));

let state, jumpToAttention, jumpBack, switchTo;

beforeAll(async () => {
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"></div>';
  ({ state } = await import('../../src/app/state.js'));
  ({ switchTo } = await import('../../src/app/view.js'));
  ({ jumpToAttention, jumpBack } = await import('../../src/app/keyboard.js'));
});

beforeEach(() => {
  switchTo.mockClear();
  state.projects = [{ id: 'p1' }];
  state.sessions = [
    { id: 'a', project_id: 'p1', order: 0 },
    { id: 'b', project_id: 'p1', order: 1 },
    { id: 'c', project_id: 'p1', order: 2 },
  ];
  state.activeId = 'a';
  state.attention = new Set();
  state.attentionReturnId = null;
});

describe('jumpToAttention', () => {
  it('switches to the flagged session and anchors where you came from', () => {
    state.attention = new Set(['c']);
    jumpToAttention();
    expect(switchTo).toHaveBeenCalledWith('c');
    expect(state.attentionReturnId).toBe('a');
  });

  it('keeps the original anchor across a multi-hop round of bells', () => {
    state.attention = new Set(['b', 'c']);
    jumpToAttention();          // a → b, anchor = a
    state.activeId = 'b';       // switchTo is mocked; mirror what it would do
    state.attention.delete('b');
    jumpToAttention();          // b → c, anchor must STILL be a
    expect(switchTo).toHaveBeenLastCalledWith('c');
    expect(state.attentionReturnId).toBe('a');
  });

  it('re-anchors on the next ⌘B after ⇧⌘B released the slot', () => {
    state.attention = new Set(['b']);
    jumpToAttention();          // a → b, anchor = a
    state.activeId = 'b';
    jumpBack();                 // back to a, anchor released
    state.activeId = 'c';       // user goes to work in c
    state.attention = new Set(['b']);
    jumpToAttention();          // c → b, fresh anchor = c
    expect(state.attentionReturnId).toBe('c');
  });

  it('releases the anchor when the jump lands back on it', () => {
    // The anchored session rings its own bell mid-round: ⌘B takes you
    // home, so ⇧⌘B must not stay armed pointing at where you already are.
    state.attention = new Set(['b']);
    jumpToAttention();          // a → b, anchor = a
    state.activeId = 'b';
    state.attention = new Set(['a']);
    jumpToAttention();          // b → a, which IS the anchor
    expect(switchTo).toHaveBeenLastCalledWith('a');
    expect(state.attentionReturnId).toBeNull();
  });

  it('does not switch or anchor when nothing needs attention', () => {
    jumpToAttention();
    expect(switchTo).not.toHaveBeenCalled();
    expect(state.attentionReturnId).toBeNull();
  });
});

describe('jumpBack', () => {
  it('returns to the anchor and releases it', () => {
    state.attentionReturnId = 'a';
    state.activeId = 'c';
    jumpBack();
    expect(switchTo).toHaveBeenCalledWith('a');
    expect(state.attentionReturnId).toBeNull();
  });

  it('is a no-op with no anchor set', () => {
    jumpBack();
    expect(switchTo).not.toHaveBeenCalled();
  });

  it('does not switch to an anchor whose session was killed', () => {
    state.attentionReturnId = 'gone';
    jumpBack();
    expect(switchTo).not.toHaveBeenCalled();
    expect(state.attentionReturnId).toBeNull();
  });
});
