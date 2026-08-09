// @vitest-environment jsdom
//
// Covers the agent launcher's filter box (src/app/modals/launcher.ts):
// the substring narrowing itself, and the four things around it that
// carry real risk —
//   * the digit shortcuts (1–9) must keep working while the box is
//     empty and must become plain characters once it isn't,
//   * Enter must launch the SELECTED FILTERED row, not the row that
//     happened to sit at that index before filtering,
//   * a query typed while ListAgents is still in flight must survive
//     the resolve (the launcher opens before the agent list arrives),
//   * every opening must start empty, including reopening over an
//     already-open launcher.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { Mock } from 'vitest';
// Type-only: erased, so the generated module is never resolved at runtime.
import type { main } from '../../wailsjs/go/models';
import { isMac } from '../../src/lib/platform.js';

const AGENTS: main.AgentInfo[] = [
  {
    id: 'shell',
    name: 'Shell',
    color: '#888',
    available: true,
    installCmd: [],
  },
  {
    id: 'claude',
    name: 'Claude',
    color: '#d97757',
    available: true,
    installCmd: [],
  },
  {
    id: 'codex',
    name: 'Codex CLI',
    color: '#4a9',
    available: true,
    installCmd: [],
  },
] as main.AgentInfo[];

// Held so a test can decide WHEN ListAgents resolves — the in-flight
// query case needs to type between the open and the resolve.
let resolveAgents: (a: main.AgentInfo[]) => void;
let agentsPromise: Promise<main.AgentInfo[]>;
function freshAgentsPromise() {
  agentsPromise = new Promise((res) => {
    resolveAgents = res;
  });
  return agentsPromise;
}

const listAgents = vi.fn((): Promise<main.AgentInfo[]> => freshAgentsPromise());
const isGitRepo = vi.fn(
  (_cwd: string): Promise<boolean> => Promise.resolve(true),
);
const createSession = vi.fn(
  (
    _agent: string,
    _project: string,
    _name: string,
    _cwd: string,
    _cols: number,
    _rows: number,
    _worktree: boolean,
  ): Promise<string> => Promise.resolve('s1'),
);
const duplicateSession = vi.fn(
  (_agent: string, _project: string, _cwd: string): Promise<string> =>
    Promise.resolve('s2'),
);
const restartSession = vi.fn(
  (_id: string): Promise<string> => Promise.resolve(''),
);

// Forwarded variadically off Parameters<>, not at a fixed arity: a mock
// that drops an argument the real binding gained still satisfies
// toHaveBeenCalledWith. Same reason settings.test.ts does it.
vi.mock('../../src/bridge.js', () => ({
  ListAgents: (...a: Parameters<typeof listAgents>) => listAgents(...a),
  IsGitRepo: (...a: Parameters<typeof isGitRepo>) => isGitRepo(...a),
  CreateSession: (...a: Parameters<typeof createSession>) =>
    createSession(...a),
  DuplicateSession: (...a: Parameters<typeof duplicateSession>) =>
    duplicateSession(...a),
  RestartSession: (...a: Parameters<typeof restartSession>) =>
    restartSession(...a),
}));

// #terms / #projects / #status are the app singletons app/dom.ts
// resolves with mustEl at import time — launcher.ts pulls dom.js in for
// flashStatus, so they must exist before the dynamic import below even
// though this suite never touches them.
const MARKUP = `
  <main id="terms"></main>
  <ul id="projects"></ul>
  <div id="status"></div>
  <div id="launcher" class="hidden" role="menu"></div>`;

type LauncherModule = typeof import('../../src/app/modals/launcher.js');
let openLauncher: LauncherModule['openLauncher'];
let closeLauncher: LauncherModule['closeLauncher'];
let initLauncher: LauncherModule['initLauncher'];
let refocusActiveTerm: Mock<() => void>;
let setFocusedTile: Mock<(id: string | null) => void>;

function launcher() {
  return document.getElementById('launcher') as HTMLElement;
}
function searchBox() {
  return launcher().querySelector('.launcher-search') as HTMLInputElement;
}
function rows() {
  return Array.from(
    launcher().querySelectorAll('.launcher-item'),
  ) as HTMLElement[];
}
function names() {
  return rows().map((r) => r.querySelector('.agent-name')?.textContent);
}
function selectedName() {
  return launcher().querySelector('.launcher-item.selected .agent-name')
    ?.textContent;
}
// The launcher owns its keys via a listener on #launcher, so events
// must be dispatched from inside it (bubbling), exactly as a real
// keystroke into the focused filter box would.
// Returns false when the handler called preventDefault — i.e. when the
// launcher consumed the key rather than letting it reach the input.
function press(key: string, init: KeyboardEventInit = {}) {
  return searchBox().dispatchEvent(
    new KeyboardEvent('keydown', {
      key,
      bubbles: true,
      cancelable: true,
      ...init,
    }),
  );
}
// Typing = set the value, then fire `input`. jsdom won't do the former
// for us from a KeyboardEvent, and the digit path is keydown-only.
function type(text: string) {
  searchBox().value = text;
  searchBox().dispatchEvent(new Event('input', { bubbles: true }));
}
// Open and let ListAgents settle.
async function open() {
  openLauncher('p1');
  resolveAgents(AGENTS);
  await agentsPromise;
  await Promise.resolve();
}

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  // jsdom has no layout, so Element.scrollIntoView is undefined and
  // highlightLauncherSelection would throw on every render.
  Element.prototype.scrollIntoView = vi.fn();
  // Imported AFTER the markup exists: launcherEl is resolved at module
  // load via pageEl('launcher').
  const mod = await import('../../src/app/modals/launcher.js');
  openLauncher = mod.openLauncher;
  closeLauncher = mod.closeLauncher;
  initLauncher = mod.initLauncher;
  refocusActiveTerm = vi.fn();
  setFocusedTile = vi.fn();
  initLauncher({ refocusActiveTerm, setFocusedTile });
});

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  listAgents.mockImplementation(() => freshAgentsPromise());
  closeLauncher();
});

describe('launcher filter box', () => {
  it('filters rows by case-insensitive substring of the agent name', async () => {
    await open();
    expect(names()).toEqual(['Shell', 'Claude', 'Codex CLI']);
    type('CLA');
    expect(names()).toEqual(['Claude']);
    type('c');
    expect(names()).toEqual(['Claude', 'Codex CLI']);
  });

  it('resets selection to the first match when the query changes', async () => {
    await open();
    press('ArrowDown');
    press('ArrowDown');
    expect(selectedName()).toBe('Codex CLI');
    type('c');
    expect(selectedName()).toBe('Claude');
  });

  it('renders an empty-state row when nothing matches', async () => {
    await open();
    type('zzz');
    expect(rows()).toHaveLength(0);
    const empty = launcher().querySelector('.launcher-empty');
    expect(empty?.textContent).toBe('No agents match');
  });

  it('distinguishes an empty agent list from an empty filter result', async () => {
    openLauncher('p1');
    resolveAgents([]);
    await agentsPromise;
    await Promise.resolve();
    expect(launcher().querySelector('.launcher-empty')?.textContent).toBe(
      'No agents found',
    );
  });

  it('keeps showing the loading row when the user types before agents arrive', async () => {
    openLauncher('p1');
    expect(launcher().querySelector('.launcher-loading')).not.toBeNull();
    // The keystroke re-renders the list. While the request is in flight
    // there are no agents to filter, and an empty result must not be
    // reported as "No agents match" — the list isn't in yet.
    type('cla');
    expect(launcher().querySelector('.launcher-loading')?.textContent).toBe(
      'Loading agents…',
    );
    expect(launcher().querySelector('.launcher-empty')).toBeNull();
    resolveAgents(AGENTS);
    await agentsPromise;
    await Promise.resolve();
    expect(launcher().querySelector('.launcher-loading')).toBeNull();
    expect(names()).toEqual(['Claude']);
  });

  it('does not filter the previous opening list when reopened', async () => {
    await open();
    // Reopen without closing: until the new response lands there is no
    // list yet, so the stale one must not be what the query filters.
    openLauncher('p1');
    type('cla');
    expect(rows()).toHaveLength(0);
    expect(launcher().querySelector('.launcher-loading')).not.toBeNull();
  });

  it('honors a query typed while ListAgents is still in flight', async () => {
    openLauncher('p1');
    // Still loading — the filter box is already on screen and focused.
    expect(launcher().querySelector('.launcher-loading')).not.toBeNull();
    type('codex');
    resolveAgents(AGENTS);
    await agentsPromise;
    await Promise.resolve();
    expect(names()).toEqual(['Codex CLI']);
  });

  it('starts each open with an empty query', async () => {
    await open();
    type('cla');
    expect(names()).toEqual(['Claude']);
    press('Escape');
    await open();
    expect(searchBox().value).toBe('');
    expect(names()).toEqual(['Shell', 'Claude', 'Codex CLI']);
    // And again without closing first — ⌘T over an open launcher.
    type('cla');
    await open();
    expect(searchBox().value).toBe('');
    expect(names()).toEqual(['Shell', 'Claude', 'Codex CLI']);
  });
});

describe('launcher keyboard', () => {
  it('activates a row by digit while the query is empty', async () => {
    await open();
    press('2');
    expect(createSession).toHaveBeenCalledTimes(1);
    expect(createSession.mock.calls[0][0]).toBe('claude');
  });

  it('types the digit into the query once the query is non-empty', async () => {
    await open();
    type('c');
    // Not consumed: dispatchEvent returns true, so a real browser would
    // insert "2" into the box. jsdom won't do that for us, hence the
    // explicit assertion rather than checking the value.
    expect(press('2')).toBe(true);
    expect(createSession).not.toHaveBeenCalled();
    expect(names()).toEqual(['Claude', 'Codex CLI']);
  });

  it('treats a whitespace-only query as typing, not as an empty box', async () => {
    await open();
    // A lone space trims away for matching (the full list still shows)
    // but must still count as "the user is typing": hints off, digits
    // inert. Hint state and shortcut state read the same raw value.
    type(' ');
    expect(names()).toEqual(['Shell', 'Claude', 'Codex CLI']);
    expect(
      rows().map((r) => r.querySelector('.agent-num')?.textContent),
    ).toEqual(['', '', '']);
    expect(press('2')).toBe(true);
    expect(createSession).not.toHaveBeenCalled();
  });

  it('hides row numbers while the query is non-empty', async () => {
    await open();
    expect(
      rows().map((r) => r.querySelector('.agent-num')?.textContent),
    ).toEqual(['1', '2', '3']);
    type('c');
    expect(
      rows().map((r) => r.querySelector('.agent-num')?.textContent),
    ).toEqual(['', '']);
    type('');
    expect(
      rows().map((r) => r.querySelector('.agent-num')?.textContent),
    ).toEqual(['1', '2', '3']);
  });

  it('launches the selected filtered match, not the unfiltered index', async () => {
    await open();
    type('codex');
    press('Enter');
    expect(createSession).toHaveBeenCalledTimes(1);
    expect(createSession.mock.calls[0][0]).toBe('codex');
  });

  it('wraps arrow-key selection within the filtered set only', async () => {
    await open();
    type('c');
    expect(selectedName()).toBe('Claude');
    press('ArrowUp');
    expect(selectedName()).toBe('Codex CLI');
    press('ArrowDown');
    expect(selectedName()).toBe('Claude');
  });

  it('reopens (and so clears the query) on a second cmd-T', async () => {
    await open();
    type('cla');
    expect(names()).toEqual(['Claude']);
    // keyboard.js bails out entirely while the launcher is open, so
    // this binding only still works because the launcher repeats it.
    // cmdOrCtrl() rejects the cross-platform combo outright, so the
    // modifier has to match the platform the module resolved at load.
    press('t', isMac ? { metaKey: true } : { ctrlKey: true });
    resolveAgents(AGENTS);
    await agentsPromise;
    await Promise.resolve();
    expect(searchBox().value).toBe('');
    expect(names()).toEqual(['Shell', 'Claude', 'Codex CLI']);
  });

  it('closes on Escape and hands the keyboard back', async () => {
    await open();
    searchBox().focus();
    const box = searchBox();
    // Cleared AFTER opening: beforeEach calls closeLauncher(), which
    // already ran refocusActiveTerm once, so a bare toHaveBeenCalled()
    // here would pass even if Escape did nothing.
    refocusActiveTerm.mockClear();
    press('Escape');
    expect(launcher().classList.contains('hidden')).toBe(true);
    expect(document.activeElement).not.toBe(box);
    expect(refocusActiveTerm).toHaveBeenCalledTimes(1);
  });
});

describe('launcher worktree row', () => {
  it('survives a query that matches no agent', async () => {
    await open();
    type('zzz');
    const wt = launcher().querySelector('.launcher-worktree');
    expect(wt).not.toBeNull();
    const box = wt?.querySelector('input[type=checkbox]') as HTMLInputElement;
    box.checked = true;
    box.dispatchEvent(new Event('change', { bubbles: true }));
    expect(localStorage.getItem('hive.worktree')).toBe('1');
  });

  it('sits between the filter box and the agent list', async () => {
    await open();
    const kids = Array.from(launcher().children).map((c) => c.className);
    expect(kids).toEqual([
      'launcher-search',
      'launcher-worktree',
      'launcher-list',
    ]);
  });
});

describe('launcher stale-response handling', () => {
  it('ignores a ListAgents resolve from a superseded open', async () => {
    // Reopening while the first request is in flight. Both resolve; the
    // stale one must not touch the DOM the second open now owns. The
    // `hidden` guard alone can't catch this — the launcher is visible
    // again by the time the first response lands.
    openLauncher('p1');
    const resolveFirst = resolveAgents;
    const first = agentsPromise;
    openLauncher('p1');
    const resolveSecond = resolveAgents;
    const second = agentsPromise;

    resolveFirst(AGENTS);
    await first;
    await Promise.resolve();
    resolveSecond(AGENTS);
    await second;
    await Promise.resolve();

    expect(launcher().querySelectorAll('.launcher-worktree')).toHaveLength(1);
    expect(Array.from(launcher().children).map((c) => c.className)).toEqual([
      'launcher-search',
      'launcher-worktree',
      'launcher-list',
    ]);
    expect(names()).toEqual(['Shell', 'Claude', 'Codex CLI']);
  });

  it('does not close a reopened launcher when a superseded request rejects', async () => {
    listAgents.mockImplementationOnce(() => Promise.reject(new Error('boom')));
    openLauncher('p1');
    openLauncher('p1');
    resolveAgents(AGENTS);
    await agentsPromise;
    await Promise.resolve();
    await Promise.resolve();
    expect(launcher().classList.contains('hidden')).toBe(false);
    expect(names()).toEqual(['Shell', 'Claude', 'Codex CLI']);
  });
});

describe('launcher teardown', () => {
  it('does not throw when closed before it was ever opened', () => {
    expect(() => closeLauncher()).not.toThrow();
  });

  it('does not throw when ListAgents rejects', async () => {
    listAgents.mockImplementationOnce(() => Promise.reject(new Error('boom')));
    openLauncher('p1');
    await Promise.resolve();
    await Promise.resolve();
    expect(launcher().classList.contains('hidden')).toBe(true);
  });
});
