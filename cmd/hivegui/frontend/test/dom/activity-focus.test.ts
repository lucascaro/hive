// @vitest-environment jsdom
//
// Spec 416 phase 4: the activity grid hides every terminal, so focus code
// must never drive a hidden terminal's textarea, and entering the grid
// must drop focus that is already there. Chromium blurs a focused
// textarea under visibility:hidden on its own, which makes Playwright
// blind to both rules; WKWebView (the macOS app) does not reliably. jsdom
// applies no CSS at all, so the rules are observable here.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import * as store from '../../src/store/store.js';
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
    SetSessionAttention: vi.fn(() => Promise.resolve()),
    KillSessionAndWorktree: fn(),
    RestartSession: fn(),
    UpdateSession: fn(),
    RestoreSession: fn(),
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
    LogFrontend: vi.fn(),
    EventsOn: vi.fn(),
    WindowSetTitle: vi.fn(),
    ClipboardGetText: fn(),
    GetActivity: fn(),
  };
});

let focus: typeof import('../../src/app/focus.js');
let textarea: HTMLTextAreaElement;

const settle = () => new Promise((r) => setTimeout(r, 50));

beforeAll(async () => {
  globalThis.requestAnimationFrame = ((cb: FrameRequestCallback) =>
    setTimeout(() => cb(performance.now()), 0)) as typeof requestAnimationFrame;
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  focus = await import('../../src/app/focus.js');
  const { setTerm } = await import('../../src/store/terms.js');
  const host = document.createElement('div');
  host.className = 'term-host';
  textarea = document.createElement('textarea');
  textarea.className = 'xterm-helper-textarea';
  host.appendChild(textarea);
  document.getElementById('terms')?.appendChild(host);
  setTerm('s1', {
    host,
    term: { focus: () => textarea.focus() },
  } as unknown as TermTile);
});

beforeEach(() => {
  store.setSessions([{ id: 's1', name: 'one', order: 0 }] as never);
  store.setActiveId('s1');
  store.setView('grid-project', false);
  store.setActivityGrid(false);
  // Disarms the previous test's focus guard, which would otherwise put
  // focus straight back on this blur.
  focus.setFocusedTile(null);
  textarea.blur();
});

describe('activity grid focus', () => {
  it('control: a normal grid drives focus into the active terminal', async () => {
    focus.setFocusedTile('s1');
    await settle();
    expect(document.activeElement).toBe(textarea);
  });

  it('never drives focus into a terminal while the activity grid is shown', async () => {
    store.setActivityGrid(true);
    focus.setFocusedTile('s1');
    await settle();
    expect(document.activeElement).not.toBe(textarea);
  });

  it('blurTerminals drops focus, and an armed guard does not restore it', async () => {
    focus.setFocusedTile('s1');
    await settle();
    expect(document.activeElement).toBe(textarea);
    // The guard is armed for 500ms after that drive; a bare blur would be
    // undone by it.
    focus.setFocusedTile('s1');
    store.setActivityGrid(true);
    focus.blurTerminals();
    await settle();
    expect(document.activeElement).not.toBe(textarea);
  });
});
