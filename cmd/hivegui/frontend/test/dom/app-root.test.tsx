// @vitest-environment jsdom
//
// The single React root (src/components/App.tsx), mounted against the
// REAL index.html rather than a hand-built fixture. That is the point of
// the file: App renders every region as a portal into an element
// index.html owns, so a fixture that lists those ids by hand proves
// nothing — it would drift with the document and keep passing.
//
// Two things are asserted, and they are the two failure modes the single
// root introduced:
//
//   1. Every portal target named by App still exists in index.html. A
//      missing one throws at mount (App uses mustEl, which names the id),
//      and with one root that takes the whole tree down rather than the
//      one island that used to be skipped.
//   2. No id occurs twice after mount. A portal APPENDS into its
//      container, where the island root it replaced CLEARED that
//      container first. index.html seeds three targets with pre-paint
//      markup, and main.tsx empties exactly those three before the first
//      commit — a hardcoded list with nothing keeping it in sync with the
//      document. Add pre-paint markup to a fourth target and the ids
//      silently double; without this test the only detector is an
//      unrelated Playwright strict-mode locator failure.
import {
  describe,
  it,
  expect,
  beforeAll,
  beforeEach,
  afterEach,
  vi,
} from 'vitest';
import { render, cleanup } from '@testing-library/react';
import { resetStore } from '../../src/store/store.js';
import indexHtml from '../../index.html?raw';

// The whole Wails bridge, stubbed. App's import graph reaches
// app/keyboard.ts and app/events.ts, both of which call EventsOn at
// module scope; without a stub they dereference window.runtime, which
// only exists inside a Wails webview. This file asserts nothing about
// bridge calls — the stub exists so the graph can be imported at all.
vi.mock('../../src/bridge.js', () => ({
  ApplyUpdateAndRestart: vi.fn(),
  CheckForUpdate: vi.fn(),
  ClipboardGetText: vi.fn(),
  CloseAttach: vi.fn(),
  CloseWindow: vi.fn(),
  Confirm: vi.fn(),
  ConnectControl: vi.fn(),
  CreateProject: vi.fn(),
  CreateSession: vi.fn(),
  CreateWorktree: vi.fn(),
  DeleteBranch: vi.fn(),
  DuplicateSession: vi.fn(),
  EventsOn: vi.fn(),
  GetUpdateSettings: vi.fn(),
  IsGitRepo: vi.fn(),
  KillProject: vi.fn(),
  KillSession: vi.fn(),
  KillSessionAndWorktree: vi.fn(),
  LaunchDir: vi.fn(),
  ListAgents: vi.fn(),
  ListClosedSessions: vi.fn(),
  ListCustomAgents: vi.fn(),
  ListWorktrees: vi.fn(),
  LogFrontend: vi.fn(),
  Notify: vi.fn(),
  OpenNewWindow: vi.fn(),
  OpenSession: vi.fn(),
  OpenTerminalAt: vi.fn(),
  OpenURL: vi.fn(),
  PickDirectory: vi.fn(),
  RemoveWorktree: vi.fn(),
  RenameWorktree: vi.fn(),
  RequestScrollbackReplay: vi.fn(),
  ResizeSession: vi.fn(),
  RestartDaemon: vi.fn(),
  RestartSession: vi.fn(),
  RestoreSession: vi.fn(),
  SaveCustomAgents: vi.fn(),
  SaveUpdateSettings: vi.fn(),
  SetClipboardText: vi.fn(),
  SetDebugTrace: vi.fn(),
  SourceRepoStatusFor: vi.fn(),
  StartUpdate: vi.fn(),
  UpdateProject: vi.fn(),
  UpdateSession: vi.fn(),
  UpdateStatus: vi.fn(),
  WindowSetTitle: vi.fn(),
  WriteStdin: vi.fn(),
}));

// Imported dynamically, after the document exists: App's module graph
// reaches app/dom.ts, which resolves #terms with mustEl at module scope
// and throws on an empty document. Every dom suite that touches that
// graph does the same.
let App: typeof import('../../src/components/App.js').App;

// The real document, pulled in with Vite's `?raw` — the same idiom
// src/ui/icon.ts uses for the sprite. No node:fs: the dom project's
// tsconfig has only vite/client types, and a path resolved at runtime
// would be one more thing that can drift.

// The ids main.tsx empties before the first commit. Duplicated here on
// purpose: if the two lists disagree, the duplicate-id assertion below
// fails, which is exactly the drift this file exists to catch.
const PRE_PAINT_SEEDED = ['status', 'boot-state', 'sidebar-hints'];

function loadIndexBody(): void {
  // Parsed, not sliced-and-regexed. A regex that strips <script> is
  // hand-rolled HTML sanitization — CodeQL flags the shape (js/bad-tag-filter,
  // js/incomplete-multi-character-sanitization) and it is right to: the
  // pattern is wrong for upper-case tags and for anything nested. DOMParser
  // does the real thing, and removing the parsed <script> elements is exact.
  //
  // The scripts have to go: index.html carries the pre-paint theme stamp and
  // the module entry point, and jsdom would try to run and fetch them. Booting
  // the real composition root is not what this file tests.
  const parsed = new DOMParser().parseFromString(indexHtml, 'text/html');
  for (const script of parsed.querySelectorAll('script')) script.remove();
  document.body.replaceChildren(
    ...Array.from(parsed.body.childNodes, (n) => document.importNode(n, true)),
  );
}

function idCounts(): Map<string, number> {
  const counts = new Map<string, number>();
  for (const el of document.querySelectorAll('[id]')) {
    counts.set(el.id, (counts.get(el.id) ?? 0) + 1);
  }
  return counts;
}

beforeAll(async () => {
  // grid-layout.ts constructs one at module scope; jsdom has no
  // ResizeObserver. The same stub every grid-touching dom suite installs.
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  // index.html carries #terms, so loading the body first is enough.
  loadIndexBody();
  ({ App } = await import('../../src/components/App.js'));
});

beforeEach(() => {
  resetStore();
  loadIndexBody();
});

afterEach(cleanup);

describe('App — the single React root', () => {
  it('mounts against index.html without throwing', () => {
    expect(() => render(<App />, { container: mustReactRoot() })).not.toThrow();
  });

  it('leaves every id in the document exactly once', () => {
    // main.tsx clears the seeded containers before the first commit; do
    // the same here, since that step is part of mounting the root.
    for (const id of PRE_PAINT_SEEDED) {
      document.getElementById(id)?.replaceChildren();
    }
    render(<App />, { container: mustReactRoot() });

    const duplicated = [...idCounts()].filter(([, n]) => n > 1);
    expect(
      duplicated,
      `ids present more than once after mount: ${duplicated
        .map(([id, n]) => `#${id}×${n}`)
        .join(', ')}`,
    ).toEqual([]);
  });

  it('duplicates ids when a seeded container is not cleared first', () => {
    // The negative case, so the assertion above cannot pass vacuously:
    // skip the clear and the portal appends beside the pre-paint markup.
    render(<App />, { container: mustReactRoot() });

    const duplicated = [...idCounts()]
      .filter(([, n]) => n > 1)
      .map(([id]) => id);
    expect(duplicated).toContain('status-text');
  });
});

// #react-root is index.html's own container for the root, so RTL renders
// into it rather than appending a container of its own — otherwise the
// tree would sit outside #app and the portals would still fire, but the
// document would not be the one main.tsx mounts against.
function mustReactRoot(): HTMLElement {
  const el = document.getElementById('react-root');
  if (!el) throw new Error('index.html no longer has #react-root');
  return el;
}
