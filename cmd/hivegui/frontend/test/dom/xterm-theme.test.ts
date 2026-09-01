import { beforeAll, describe, expect, it, vi } from 'vitest';
import { xtermTheme } from '../../src/theme/theme';

// session-term reaches for #terms and installs observers at module
// load, so it is imported dynamically after the fixture exists — the
// same shape test/dom/session-phase.test.ts uses.
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
  };
});

describe('xtermTheme', () => {
  it('reads --term-bg/--term-fg/--accent from the root element', () => {
    document.documentElement.style.setProperty('--term-bg', '#0b0c10');
    document.documentElement.style.setProperty('--term-fg', '#dfe1ea');
    document.documentElement.style.setProperty('--accent', '#ffb454');
    document.documentElement.style.setProperty('--on-accent', '#15120a');
    const t = xtermTheme(document);
    expect(t.background).toBe('#0b0c10');
    expect(t.foreground).toBe('#dfe1ea');
    expect(t.cursor).toBe('#ffb454');
    expect(t.cursorAccent).toBe('#15120a');
  });
});

describe('applyXtermTheme', () => {
  let applyXtermTheme: typeof import('../../src/app/session-term.js').applyXtermTheme;
  let state: typeof import('../../src/app/state.js').state;

  beforeAll(async () => {
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as unknown as typeof ResizeObserver;
    window.matchMedia = ((query: string) => ({
      matches: false,
      media: query,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
      onchange: null,
      dispatchEvent: () => false,
    })) as unknown as typeof window.matchMedia;
    document.body.innerHTML =
      '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
    ({ state } = await import('../../src/app/state.js'));
    ({ applyXtermTheme } = await import('../../src/app/session-term.js'));
  });

  it('pushes the current tokens into every open terminal', () => {
    document.documentElement.style.setProperty('--term-bg', '#101010');
    document.documentElement.style.setProperty('--term-fg', '#f0f0f0');
    const a = {
      term: { options: { theme: { background: '#000000', foreground: '' } } },
    };
    // Tiles whose term is absent (the DOM-test stubs, and a tile whose
    // terminal has been disposed) must not throw.
    const b = {};
    state.terms.set('a', a as never);
    state.terms.set('b', b as never);

    applyXtermTheme();

    expect(a.term.options.theme.background).toBe('#101010');
    expect(a.term.options.theme.foreground).toBe('#f0f0f0');
    state.terms.clear();
  });
});
