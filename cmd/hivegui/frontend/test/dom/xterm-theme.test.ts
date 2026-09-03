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

describe('ANSI palette', () => {
  it("maps --ansi-0..15 onto xterm's sixteen colour keys", () => {
    const root = document.documentElement;
    const names = [
      'black',
      'red',
      'green',
      'yellow',
      'blue',
      'magenta',
      'cyan',
      'white',
      'brightBlack',
      'brightRed',
      'brightGreen',
      'brightYellow',
      'brightBlue',
      'brightMagenta',
      'brightCyan',
      'brightWhite',
    ] as const;
    // Distinct sentinel per slot: an off-by-one in the mapping is the
    // failure mode here, and a uniform value would not catch it.
    names.forEach((_, i) => {
      root.style.setProperty(
        `--ansi-${i}`,
        `#0000${i.toString(16)}${i.toString(16)}`,
      );
    });
    const t = xtermTheme(document) as Record<string, string>;
    names.forEach((name, i) => {
      expect(t[name], name).toBe(`#0000${i.toString(16)}${i.toString(16)}`);
    });
    for (let i = 0; i < names.length; i++) {
      root.style.removeProperty(`--ansi-${i}`);
    }
  });

  it('omits a slot the preset does not define rather than sending an empty string', () => {
    for (let i = 0; i < 16; i++) {
      document.documentElement.style.removeProperty(`--ansi-${i}`);
    }
    const t = xtermTheme(document) as Record<string, string>;
    // jsdom resolves no stylesheet, so every --ansi-* is empty here;
    // xterm must keep its own defaults instead of being handed ''.
    expect('black' in t).toBe(false);
    expect(t.background).toBeDefined();
  });
});

describe('applyXtermTheme', () => {
  let applyXtermTheme: typeof import('../../src/app/session-term.js').applyXtermTheme;
  let state: typeof import('../../src/store/store.js').hiveStateView;

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
    ({ hiveStateView: state } = await import('../../src/store/store.js'));
    ({ applyXtermTheme } = await import('../../src/app/session-term.js'));
  });

  it('pushes the current tokens into every open terminal', () => {
    document.documentElement.style.setProperty('--term-bg', '#101010');
    document.documentElement.style.setProperty('--term-fg', '#f0f0f0');
    // --font-mono travels with the colours: `terminal` and `native-*` name
    // different families, and xterm has no cascade to pick that up itself.
    document.documentElement.style.setProperty(
      '--font-mono',
      'Iosevka, monospace',
    );
    let refits = 0;
    const a = {
      term: {
        options: {
          fontFamily: 'Menlo',
          theme: { background: '#000000', foreground: '' },
        },
      },
      _onBodyResize: () => {
        refits++;
      },
    };
    // Tiles whose term is absent (the DOM-test stubs, and a tile whose
    // terminal has been disposed) must not throw.
    const b = {
      _onBodyResize: () => {
        refits++;
      },
    };
    state.terms.set('a', a as never);
    state.terms.set('b', b as never);

    applyXtermTheme();

    expect(a.term.options.theme.background).toBe('#101010');
    expect(a.term.options.theme.foreground).toBe('#f0f0f0');
    expect(a.term.options.fontFamily).toBe('Iosevka, monospace');
    // A font change moves the character cell, so (cols, rows) must be
    // recomputed and the PTY told — the body box does not change, so the
    // ResizeObserver will not do it for us. Both tiles refit, including
    // the one with no terminal.
    expect(refits).toBe(2);
    state.terms.clear();
  });
});
