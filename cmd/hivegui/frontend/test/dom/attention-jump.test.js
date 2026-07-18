// @vitest-environment jsdom
//
// Return-slot semantics for ⌘B / ⇧⌘B (src/app/keyboard.js).
//
// The slot is written only when empty, so touring several flagged
// sessions with repeated ⌘B keeps the ORIGINAL anchor — that
// write-once rule is the whole feature, and a naive "remember the last
// session" implementation passes casual manual testing while failing
// the two-hop case. Locked down here.
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

  it('keeps the original anchor across a multi-hop tour', () => {
    state.attention = new Set(['b', 'c']);
    jumpToAttention();          // a → b, anchor = a
    state.activeId = 'b';       // switchTo is mocked; mirror what it would do
    state.attention.delete('b');
    jumpToAttention();          // b → c, anchor must STILL be a
    expect(switchTo).toHaveBeenLastCalledWith('c');
    expect(state.attentionReturnId).toBe('a');
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
