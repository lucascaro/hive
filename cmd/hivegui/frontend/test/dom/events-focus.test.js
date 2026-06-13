// @vitest-environment jsdom
//
// Regression test for the window-focus handler registered by
// wireDaemonEvents (src/app/events.js). The stage-3/4 modularization
// once mangled `refocusActiveTerm()` into `redeps.focusActiveTerm()`
// during the verbatim move — a ReferenceError on every window focus
// that silently killed both attention-clearing and xterm refocus.
// This test drives the real handler through a real DOM 'focus' event
// so any missed deps.* substitution in that path throws here.
import { describe, it, expect, vi, beforeAll } from 'vitest';

// The bridge re-exports the Wails runtime, which doesn't exist under
// vitest (the vite-plugin substitution only applies to the Playwright
// harnesses). Mock the full surface so the whole src/app import graph
// (events → sidebar → modals) resolves.
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

let state, wireDaemonEvents;

beforeAll(async () => {
  // dom.js dereferences #terms at import time; give it the singletons.
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"></div>';
  ({ state } = await import('../../src/app/state.js'));
  ({ wireDaemonEvents } = await import('../../src/app/events.js'));
});

describe('wireDaemonEvents window-focus handler', () => {
  it('clears active-session attention and refocuses the active term', () => {
    const refocusActiveTerm = vi.fn();
    wireDaemonEvents({
      switchTo: vi.fn(),
      renderMinimizedTray: vi.fn(),
      updateAppTitle: vi.fn(),
      focusActiveTerm: vi.fn(),
      refocusActiveTerm,
      isDaemonRestarting: () => false,
      scrollTrace: { rec: Object.assign(() => {}, { enabled: false }) },
    });

    state.activeId = 'sess-1';
    state.attention.add('sess-1');

    // A listener that throws would swallow the rest of the handler and
    // surface as a jsdom uncaught error; assert the spy actually ran.
    window.dispatchEvent(new Event('focus'));

    expect(refocusActiveTerm).toHaveBeenCalledTimes(1);
    expect(state.attention.has('sess-1')).toBe(false);
  });
});
