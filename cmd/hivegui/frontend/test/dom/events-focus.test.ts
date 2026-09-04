// @vitest-environment jsdom
//
// Regression test for the window-focus handler registered by
// wireDaemonEvents (src/app/events.ts). The stage-3/4 modularization
// once mangled `refocusActiveTerm()` into `redeps.focusActiveTerm()`
// during the verbatim move — a ReferenceError on every window focus
// that silently killed both attention-clearing and xterm refocus.
// This test drives the real handler through a real DOM 'focus' event
// so any missed deps.* substitution in that path throws here.
import { describe, it, expect, vi, beforeAll } from 'vitest';
import * as bridge from '../../src/bridge.js';
import { createScrollTrace } from '../../src/lib/scroll-debug.js';
import * as store from '../../src/store/store.js';

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
let onSessionBell: typeof import('../../src/app/events.js').onSessionBell;
let noteUserInput: typeof import('../../src/app/events.js').noteUserInput;

beforeAll(async () => {
  // dom.ts dereferences #terms at import time; give it the singletons.
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  ({ hiveStateView: state } = await import('../../src/store/store.js'));
  ({ wireDaemonEvents, onSessionBell, noteUserInput } = await import(
    '../../src/app/events.js'
  ));
});

const switchTo = vi.fn();
const checkForUpdates = vi.fn();

describe('wireDaemonEvents window-focus handler', () => {
  it('clears active-session attention and refocuses the active term', () => {
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

    state.activeId = 'sess-1';
    store.addAttention('sess-1');

    // A listener that throws would swallow the rest of the handler and
    // surface as a jsdom uncaught error; assert the spy actually ran.
    window.dispatchEvent(new Event('focus'));

    expect(refocusActiveTerm).toHaveBeenCalledTimes(1);
    expect(state.attention.has('sess-1')).toBe(false);
  });
});

// Attention now lives on the daemon, so clearing it locally is only
// half the job: another window, and the menu bar, are still showing
// the flag until the daemon is told.
describe('attention is reported to the daemon', () => {
  it('tells the daemon when the window focus clears a session', () => {
    const setAttention = vi.mocked(bridge.SetSessionAttention);
    setAttention.mockClear();

    state.activeId = 'sess-2';
    store.addAttention('sess-2');
    window.dispatchEvent(new Event('focus'));

    expect(setAttention).toHaveBeenCalledWith('sess-2', false);
  });

  // The local copy can be behind — another window may have seen the
  // bell first. Clearing only when this window happened to know about
  // it would strand the flag everywhere else.
  it('reports the clear even when this window never saw the bell', () => {
    const setAttention = vi.mocked(bridge.SetSessionAttention);
    setAttention.mockClear();

    state.activeId = 'sess-3';
    // deliberately no addAttention
    window.dispatchEvent(new Event('focus'));

    expect(setAttention).toHaveBeenCalledWith('sess-3', false);
  });
});

// The daemon is the source of truth. A window that was closed, never
// attached, or has just reloaded learns what rang from the snapshot and
// the attention event — the local xterm BEL path can only ever see the
// sessions this window has open.
describe('attention arrives from the daemon', () => {
  function emit(event: string, payload: unknown) {
    for (const call of vi.mocked(bridge.EventsOn).mock.calls) {
      if (call[0] === event) (call[1] as (p: unknown) => void)(payload);
    }
  }

  it('seeds the set from the session snapshot', () => {
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

    expect(state.attention.has('a')).toBe(true);
    expect(state.attention.has('b')).toBe(false);
    expect(state.attention.has('c')).toBe(true);
  });

  it('follows the attention event in both directions', () => {
    emit(
      'session:list',
      JSON.stringify({ sessions: [{ id: 'a' }, { id: 'b' }] }),
    );
    expect(state.attention.has('a')).toBe(false);

    emit(
      'session:event',
      JSON.stringify({
        kind: 'attention',
        session: { id: 'a', needs_attention: true },
      }),
    );
    expect(state.attention.has('a')).toBe(true);

    // Another client reported that the user looked.
    emit(
      'session:event',
      JSON.stringify({ kind: 'attention', session: { id: 'a' } }),
    );
    expect(state.attention.has('a')).toBe(false);
  });

  // A snapshot is authoritative: a flag the daemon no longer reports
  // must not survive in this window's copy.
  it('drops a stale local flag on the next snapshot', () => {
    store.addAttention('a');
    emit('session:list', JSON.stringify({ sessions: [{ id: 'a' }] }));
    expect(state.attention.has('a')).toBe(false);
  });
});

// The menu bar has no window of its own, so these are the only routes
// it has into a GUI: the daemon relays the verb and this window acts.
describe('relayed client commands', () => {
  function emit(event: string, payload: unknown) {
    for (const call of vi.mocked(bridge.EventsOn).mock.calls) {
      if (call[0] === event) (call[1] as (p: unknown) => void)(payload);
    }
  }

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

// The bell a session rings while you are already looking at it.
//
// This window raises nothing locally and posts no notification — there
// is nobody to notify. The daemon still raises the flag, and it reaches
// this window through the `attention` event like any other, so the row
// does light up. That is the point: a bell is a request, and a focused
// window is not an answer to it.
describe('a bell on the session you are already watching', () => {
  it('posts no notification and adds no local flag', () => {
    const setAttention = vi.mocked(bridge.SetSessionAttention);
    setAttention.mockClear();
    vi.spyOn(document, 'hasFocus').mockReturnValue(true);

    state.activeId = 'sess-4';
    store.clearAttentionFor('sess-4');
    onSessionBell({ id: 'sess-4', name: 'active' });

    expect(state.attention.has('sess-4')).toBe(false);
    // And it must NOT tell the daemon the user looked: they have not
    // done anything yet.
    expect(setAttention).not.toHaveBeenCalled();
    vi.mocked(document.hasFocus).mockRestore();
  });

  // What this window DOES decide is whether to interrupt the person.
  // A bell on a background session notifies; one on the session they
  // are watching does not. Neither touches the flag — that is the
  // daemon's, and arrives on the `attention` event.
  it('notifies for a background session and not for the watched one', () => {
    const notify = vi.mocked(bridge.Notify);
    vi.spyOn(document, 'hasFocus').mockReturnValue(true);
    state.activeId = 'sess-4';

    notify.mockClear();
    onSessionBell({ id: 'sess-5', name: 'background' });
    expect(notify).toHaveBeenCalled();
    expect(state.attention.has('sess-5')).toBe(false);

    notify.mockClear();
    onSessionBell({ id: 'sess-4', name: 'active' });
    expect(notify).not.toHaveBeenCalled();

    vi.mocked(document.hasFocus).mockRestore();
  });
});

// Typing into a session is what answers its request. This is the
// regression that made a bell stick forever: nothing else fires when
// the session is already active and the window already focused, so
// without this the flag had no way out.
describe('typing into a session clears its attention', () => {
  it('tells the daemon and drops the local flag', () => {
    const setAttention = vi.mocked(bridge.SetSessionAttention);
    setAttention.mockClear();

    store.addAttention('sess-6');
    noteUserInput('sess-6');

    expect(state.attention.has('sess-6')).toBe(false);
    expect(setAttention).toHaveBeenCalledWith('sess-6', false);
  });

  it('costs nothing for a session that was not asking', () => {
    const setAttention = vi.mocked(bridge.SetSessionAttention);
    setAttention.mockClear();

    store.clearAttentionFor('sess-7');
    noteUserInput('sess-7');

    expect(setAttention).not.toHaveBeenCalled();
  });
});
