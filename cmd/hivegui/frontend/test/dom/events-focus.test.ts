// @vitest-environment jsdom
//
// Regression test for the window-focus handler registered by
// wireDaemonEvents (src/app/events.ts). The stage-3/4 modularization
// once mangled `refocusActiveTerm()` into `redeps.focusActiveTerm()`
// during the verbatim move — a ReferenceError on every window focus
// that silently killed xterm refocus. This test drives the real handler
// through a real DOM 'focus' event so any missed deps.* substitution in
// that path throws here.
import { describe, it, expect, vi, beforeAll } from 'vitest';
import * as bridge from '../../src/bridge.js';
import { createScrollTrace } from '../../src/lib/scroll-debug.js';

// The bridge re-exports the Wails runtime, which doesn't exist under
// vitest (the vite-plugin substitution only applies to the Playwright
// harnesses). Mock the full surface so the whole src/app import graph
// (events → sidebar → modals) resolves.
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
    SetSessionAttention: fn(),
    RestartSession: fn(),
    UpdateSession: fn(),
    ListAgents: fn(),
    ListCustomAgents: fn(),
    SaveCustomAgents: fn(),
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

let state: typeof import('../../src/store/store.js').hiveStateView;
let wireDaemonEvents: typeof import('../../src/app/events.js').wireDaemonEvents;
let noteUserInput: typeof import('../../src/app/events.js').noteUserInput;

beforeAll(async () => {
  // dom.ts dereferences #terms at import time; give it the singletons.
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  ({ hiveStateView: state } = await import('../../src/store/store.js'));
  ({ wireDaemonEvents, noteUserInput } = await import(
    '../../src/app/events.js'
  ));
});

const switchTo = vi.fn();
const checkForUpdates = vi.fn();

function emit(event: string, payload: unknown) {
  for (const call of vi.mocked(bridge.EventsOn).mock.calls) {
    if (call[0] === event) (call[1] as (p: unknown) => void)(payload);
  }
}

describe('wireDaemonEvents window-focus handler', () => {
  it('refocuses the active term, and clears nothing locally', () => {
    // The frozen transition table (docs/exec-plans/active/
    // 336-session-state-model.md) allows exactly two client-driven
    // clears — a keystroke and switching TO a session — and a window
    // regaining focus is neither. An earlier version of this handler
    // did clear here too; that is gone.
    const refocusActiveTerm = vi.fn();
    wireDaemonEvents({
      switchTo,
      enforceViewFloor: vi.fn(),
      updateAppTitle: vi.fn(),
      focusActiveTerm: vi.fn(),
      refocusActiveTerm,
      isDaemonRestarting: () => false,
      checkForUpdates,
      // A real disabled tracer, not a hand-rolled `{ rec }` literal: the
      // pty:data path also reads .count()/.counters, which the literal
      // never had (wave 5b's view.ts lesson).
      scrollTrace: createScrollTrace({ enabled: false }),
    });

    const setAttention = vi.mocked(bridge.SetSessionAttention);
    setAttention.mockClear();
    emit(
      'session:list',
      JSON.stringify({
        sessions: [{ id: 'sess-1', needs_attention: true }],
      }),
    );
    state.activeId = 'sess-1';

    // A listener that throws would swallow the rest of the handler and
    // surface as a jsdom uncaught error; assert the spy actually ran.
    window.dispatchEvent(new Event('focus'));

    expect(refocusActiveTerm).toHaveBeenCalledTimes(1);
    expect(setAttention).not.toHaveBeenCalled();
    expect(state.sessions.find((s) => s.id === 'sess-1')?.needs_attention).toBe(
      true,
    );
  });
});

// The daemon is the source of truth. A window that was closed, never
// attached, or has just reloaded learns what rang from the snapshot and
// the attention event — this window keeps no copy of its own.
describe('needs_attention arrives from the daemon', () => {
  it('is read straight off the session snapshot', () => {
    emit(
      'session:list',
      JSON.stringify({
        sessions: [
          { id: 'a', needs_attention: true },
          { id: 'b' },
          { id: 'c', needs_attention: true },
        ],
      }),
    );

    const flag = (id: string) =>
      state.sessions.find((s) => s.id === id)?.needs_attention === true;
    expect(flag('a')).toBe(true);
    expect(flag('b')).toBe(false);
    expect(flag('c')).toBe(true);
  });

  it('follows the attention event in both directions', () => {
    emit(
      'session:list',
      JSON.stringify({ sessions: [{ id: 'a' }, { id: 'b' }] }),
    );
    expect(state.sessions.find((s) => s.id === 'a')?.needs_attention).not.toBe(
      true,
    );

    emit(
      'session:event',
      JSON.stringify({
        kind: 'attention',
        session: { id: 'a', needs_attention: true },
      }),
    );
    expect(state.sessions.find((s) => s.id === 'a')?.needs_attention).toBe(
      true,
    );

    // Another client reported that the user looked.
    emit(
      'session:event',
      JSON.stringify({ kind: 'attention', session: { id: 'a' } }),
    );
    expect(state.sessions.find((s) => s.id === 'a')?.needs_attention).not.toBe(
      true,
    );
  });
});

// The menu bar has no window of its own, so these are the only routes
// it has into a GUI: the daemon relays the verb and this window acts.
describe('relayed client commands', () => {
  it('focuses a session the menu bar named', () => {
    emit('session:list', JSON.stringify({ sessions: [{ id: 'sess-x' }] }));
    emit(
      'client:command',
      JSON.stringify({ cmd: 'focus_session', session_id: 'sess-x' }),
    );

    expect(switchTo).toHaveBeenCalledWith('sess-x');
  });

  // The menu bar's list can be a moment stale. Switching to a session
  // that has gone would leave the user staring at an empty pane.
  it('ignores a focus for a session that no longer exists', () => {
    emit('session:list', JSON.stringify({ sessions: [{ id: 'sess-x' }] }));
    switchTo.mockClear();
    emit(
      'client:command',
      JSON.stringify({ cmd: 'focus_session', session_id: 'ghost' }),
    );

    expect(switchTo).not.toHaveBeenCalled();
  });

  it('runs the update check on check_update', () => {
    checkForUpdates.mockClear();
    emit('client:command', JSON.stringify({ cmd: 'check_update' }));
    expect(checkForUpdates).toHaveBeenCalledTimes(1);
  });

  it('ignores a malformed relay payload', () => {
    switchTo.mockClear();
    checkForUpdates.mockClear();
    emit('client:command', 'not json');
    expect(switchTo).not.toHaveBeenCalled();
    expect(checkForUpdates).not.toHaveBeenCalled();
  });
});

// The notification is the one thing this window decides locally, on the
// false→true EDGE of needs_attention — not on the raw PTY bell byte,
// which no longer decides anything (see the frozen transition table).
describe('the desktop notification edge', () => {
  it('posts no notification and adds no local flag for a session already watched', () => {
    const notify = vi.mocked(bridge.Notify);
    notify.mockClear();
    vi.spyOn(document, 'hasFocus').mockReturnValue(true);

    emit('session:list', JSON.stringify({ sessions: [{ id: 'sess-4' }] }));
    state.activeId = 'sess-4';
    emit(
      'session:event',
      JSON.stringify({
        kind: 'attention',
        session: { id: 'sess-4', needs_attention: true },
      }),
    );

    // The row still lights up — the daemon raised the flag regardless —
    // but there is nobody to notify.
    expect(state.sessions.find((s) => s.id === 'sess-4')?.needs_attention).toBe(
      true,
    );
    expect(notify).not.toHaveBeenCalled();
    vi.mocked(document.hasFocus).mockRestore();
  });

  it('notifies for a background session and not for the watched one', () => {
    const notify = vi.mocked(bridge.Notify);
    vi.spyOn(document, 'hasFocus').mockReturnValue(true);

    emit(
      'session:list',
      JSON.stringify({
        sessions: [{ id: 'sess-4' }, { id: 'sess-5' }],
      }),
    );
    state.activeId = 'sess-4';

    notify.mockClear();
    emit(
      'session:event',
      JSON.stringify({
        kind: 'attention',
        session: { id: 'sess-5', needs_attention: true },
      }),
    );
    expect(notify).toHaveBeenCalled();

    notify.mockClear();
    emit(
      'session:event',
      JSON.stringify({
        kind: 'attention',
        session: { id: 'sess-4', needs_attention: true },
      }),
    );
    expect(notify).not.toHaveBeenCalled();

    vi.mocked(document.hasFocus).mockRestore();
  });
});

// Typing into a session is what answers its request. This is the
// regression that made a bell stick forever: nothing else fires when
// the session is already active and the window already focused, so
// without this the flag had no way out.
describe('typing into a session clears its attention', () => {
  it('tells the daemon when the session needs attention', () => {
    const setAttention = vi.mocked(bridge.SetSessionAttention);
    setAttention.mockClear();

    emit(
      'session:list',
      JSON.stringify({ sessions: [{ id: 'sess-6', needs_attention: true }] }),
    );
    noteUserInput('sess-6');

    expect(setAttention).toHaveBeenCalledWith('sess-6', false);
  });

  it('costs nothing for a session that was not asking', () => {
    const setAttention = vi.mocked(bridge.SetSessionAttention);
    setAttention.mockClear();

    emit('session:list', JSON.stringify({ sessions: [{ id: 'sess-7' }] }));
    noteUserInput('sess-7');

    expect(setAttention).not.toHaveBeenCalled();
  });
});
