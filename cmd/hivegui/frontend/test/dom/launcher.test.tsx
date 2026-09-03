// @vitest-environment jsdom
//
// Covers the agent launcher's filter box
// (src/components/modals/Launcher.tsx, opened through the
// openLauncher/closeLauncher pair in src/app/modals/launcher.ts):
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
import { act, fireEvent, render } from '@testing-library/react';
import { resetStore } from '../../src/store/store.js';
// Type-only: erased, so the generated module is never resolved at runtime.
import type { main } from '../../wailsjs/go/models';
import { isMac } from '../../src/lib/platform.js';
import { hiveStateView as state } from '../../src/store/store.js';

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
    _insertAfter?: string,
    _branch?: string,
    _worktreePath?: string,
    _continue?: boolean,
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
// resolves with mustEl at import time — launcher.ts pulls dom.ts in for
// flashStatus, so they must exist before the dynamic import below even
// though this suite never touches them.
//
// #launcher is nested one level under <body>: RTL's cleanup() removes a
// render() container whose parentNode IS document.body, which would rip
// the element pageEl('launcher') resolved at import time out of the
// document and turn every later lookup into a silent null.
const MARKUP = `
  <main id="terms"></main>
  <ul id="projects"></ul>
  <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
  <div id="app"><div id="launcher" class="hidden" role="menu"></div></div>`;

type LauncherModule = typeof import('../../src/app/modals/launcher.js');
let openLauncher: LauncherModule['openLauncher'];
let closeLauncher: LauncherModule['closeLauncher'];
let initLauncher: LauncherModule['initLauncher'];
let refocusActiveTerm: Mock<() => void>;
let setFocusedTile: Mock<(id: string | null) => void>;
// Imported with the module below, for the same reason: the component
// pulls app/dom.ts in, which resolves #terms with mustEl at load.
let Launcher: typeof import('../../src/components/modals/Launcher.js')['Launcher'];

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
  return launcher().querySelector('.launcher-item[data-selected] .agent-name')
    ?.textContent;
}
// The launcher owns its keys via a plain listener on #launcher, so
// events must be dispatched from inside it (bubbling), exactly as a real
// keystroke into the focused filter box would. That listener is not a
// React one, so the state it sets needs act() around the dispatch.
// Returns false when the handler called preventDefault — i.e. when the
// launcher consumed the key rather than letting it reach the input.
function press(key: string, init: KeyboardEventInit = {}) {
  let delivered = true;
  act(() => {
    delivered = searchBox().dispatchEvent(
      new KeyboardEvent('keydown', {
        key,
        bubbles: true,
        cancelable: true,
        ...init,
      }),
    );
  });
  return delivered;
}
// Typing goes through fireEvent, not a hand-built input event: the box
// is a controlled input, and React's value tracker swallows a change
// made by assigning .value directly.
function type(text: string) {
  fireEvent.change(searchBox(), { target: { value: text } });
}
// Open and let ListAgents settle.
// A few turns of the microtask queue: the IsGitRepo probe chains off
// the ListAgents .then, so one await is not enough.
async function flushMicrotasks() {
  for (let i = 0; i < 5; i++) await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
}

// Opening writes the store from outside a React event handler, and the
// agent list lands in a promise callback — both need act() for the
// island to have re-rendered by the time the assertions run.
async function openWith(fn: () => void) {
  await act(async () => {
    fn();
  });
}
async function settleAgents(list: main.AgentInfo[] = AGENTS) {
  await act(async () => {
    resolveAgents(list);
    await agentsPromise;
    await Promise.resolve();
  });
}

async function open() {
  await openWith(() => openLauncher('p1'));
  await settleAgents();
}

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  // jsdom has no layout, so Element.scrollIntoView is undefined and
  // highlightLauncherSelection would throw on every render.
  Element.prototype.scrollIntoView = vi.fn();
  // Imported AFTER the markup exists: launcherEl is resolved at module
  // load via pageEl('launcher').
  ({ Launcher } = await import('../../src/components/modals/Launcher.js'));
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
  resetStore();
  render(<Launcher root={launcher()} setFocusedTile={setFocusedTile} />, {
    container: launcher(),
  });
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
    await openWith(() => openLauncher('p1'));
    await settleAgents([]);
    expect(launcher().querySelector('.launcher-empty')?.textContent).toBe(
      'No agents found',
    );
  });

  it('keeps showing the loading row when the user types before agents arrive', async () => {
    await openWith(() => openLauncher('p1'));
    expect(launcher().querySelector('.launcher-loading')).not.toBeNull();
    // The keystroke re-renders the list. While the request is in flight
    // there are no agents to filter, and an empty result must not be
    // reported as "No agents match" — the list isn't in yet.
    type('cla');
    expect(launcher().querySelector('.launcher-loading')?.textContent).toBe(
      'Loading agents…',
    );
    expect(launcher().querySelector('.launcher-empty')).toBeNull();
    await settleAgents();
    expect(launcher().querySelector('.launcher-loading')).toBeNull();
    expect(names()).toEqual(['Claude']);
  });

  it('does not filter the previous opening list when reopened', async () => {
    await open();
    // Reopen without closing: until the new response lands there is no
    // list yet, so the stale one must not be what the query filters.
    await openWith(() => openLauncher('p1'));
    type('cla');
    expect(rows()).toHaveLength(0);
    expect(launcher().querySelector('.launcher-loading')).not.toBeNull();
  });

  it('honors a query typed while ListAgents is still in flight', async () => {
    await openWith(() => openLauncher('p1'));
    // Still loading — the filter box is already on screen and focused.
    expect(launcher().querySelector('.launcher-loading')).not.toBeNull();
    type('codex');
    await settleAgents();
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
    // The digit hint renders through kbd() now, so the row number is a
    // <kbd> child rather than the span's own text.
    const nums = () =>
      rows().map((r) => r.querySelector('.agent-num')?.textContent);
    await open();
    expect(nums()).toEqual(['[1]', '[2]', '[3]']);
    type('c');
    expect(nums()).toEqual(['', '']);
    type('');
    expect(nums()).toEqual(['[1]', '[2]', '[3]']);
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
    // keyboard.ts bails out entirely while the launcher is open, so
    // this binding only still works because the launcher repeats it.
    // cmdOrCtrl() rejects the cross-platform combo outright, so the
    // modifier has to match the platform the module resolved at load.
    press('t', isMac ? { metaKey: true } : { ctrlKey: true });
    await settleAgents();
    expect(searchBox().value).toBe('');
    expect(names()).toEqual(['Shell', 'Claude', 'Codex CLI']);
  });

  it('closes on Escape and hands the keyboard back', async () => {
    await open();
    searchBox().focus();
    const box = searchBox();
    // Cleared AFTER opening, so a bare toHaveBeenCalled() here cannot
    // pass on a refocus some earlier close already made.
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
    fireEvent.click(box);
    expect(localStorage.getItem('hive.worktree')).toBe('1');
  });

  it('sits between the filter box and the agent list', async () => {
    await open();
    const kids = Array.from(launcher().children).map((c) => c.className);
    expect(kids).toEqual([
      'launcher-search',
      'launcher-worktree',
      // The branch field trails the toggle it belongs to, hidden until
      // the toggle is on.
      'launcher-branch hidden',
      'launcher-list',
    ]);
  });
});

describe('launcher branch name', () => {
  const branchBox = () =>
    launcher().querySelector('.launcher-branch') as HTMLInputElement;
  const wtBox = () =>
    launcher().querySelector(
      '.launcher-worktree input[type=checkbox]',
    ) as HTMLInputElement;
  // Controlled inputs both: a click is what flips a checkbox, and
  // fireEvent.change is the only way past React's value tracker.
  const toggleWorktree = (on: boolean) => {
    if (wtBox().checked !== on) fireEvent.click(wtBox());
  };
  const typeBranch = (value: string) => {
    fireEvent.change(branchBox(), { target: { value } });
  };

  // The launcher preventDefaults mousedown on everything but its text
  // boxes, to keep the filter box focused. The branch box has to be
  // exempt or it is literally unclickable — which is how it shipped
  // first, because every test set .value directly.
  it('can be focused by clicking it', async () => {
    localStorage.setItem('hive.worktree', '1');
    await open();
    const ev = new window.MouseEvent('mousedown', {
      bubbles: true,
      cancelable: true,
    });
    act(() => {
      branchBox().dispatchEvent(ev);
    });
    expect(
      ev.defaultPrevented,
      'mousedown was preventDefaulted, so the box can never take focus',
    ).toBe(false);
  });

  it('still blocks focus moving to the agent rows', async () => {
    localStorage.setItem('hive.worktree', '1');
    await open();
    const rowEl = launcher().querySelector('.launcher-list > *') as HTMLElement;
    const ev = new window.MouseEvent('mousedown', {
      bubbles: true,
      cancelable: true,
    });
    act(() => {
      rowEl.dispatchEvent(ev);
    });
    expect(ev.defaultPrevented).toBe(true);
  });

  // Digits are row shortcuts while the filter box is empty. Inside the
  // branch box they are part of the name (`fix-2`) and were being
  // swallowed — the keystroke launched a session instead of typing.
  it('takes digits as text instead of launching a session', async () => {
    localStorage.setItem('hive.worktree', '1');
    await open();
    branchBox().focus();
    const ev = new window.KeyboardEvent('keydown', {
      key: '2',
      bubbles: true,
      cancelable: true,
    });
    act(() => {
      branchBox().dispatchEvent(ev);
    });
    expect(ev.defaultPrevented, 'digit was consumed as a row shortcut').toBe(
      false,
    );
    expect(createSession).not.toHaveBeenCalled();
  });

  it('launches on Enter from inside the branch box', async () => {
    localStorage.setItem('hive.worktree', '1');
    await open();
    branchBox().focus();
    typeBranch('typed-here');
    act(() => {
      branchBox().dispatchEvent(
        new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
      );
    });
    expect(createSession).toHaveBeenCalled();
    expect(createSession.mock.calls[0][8]).toBe('typed-here');
  });

  it('closes on Escape from inside the branch box', async () => {
    localStorage.setItem('hive.worktree', '1');
    await open();
    branchBox().focus();
    act(() => {
      branchBox().dispatchEvent(
        new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      );
    });
    expect(launcher().classList.contains('hidden')).toBe(true);
  });

  it('is hidden until the worktree toggle is on', async () => {
    localStorage.setItem('hive.worktree', '0');
    await open();
    expect(branchBox().classList.contains('hidden')).toBe(true);
    toggleWorktree(true);
    expect(branchBox().classList.contains('hidden')).toBe(false);
  });

  it('reaches CreateSession as the branch argument', async () => {
    localStorage.setItem('hive.worktree', '1');
    await open();
    typeBranch('my-feature');
    press('Enter');
    expect(createSession).toHaveBeenCalledWith(
      expect.any(String),
      'p1',
      '',
      '',
      0,
      0,
      true,
      expect.any(String),
      'my-feature',
      '',
      false,
    );
  });

  it('trims whitespace and sends empty for a blank name', async () => {
    localStorage.setItem('hive.worktree', '1');
    await open();
    typeBranch('   ');
    press('Enter');
    expect(createSession.mock.calls[0][8]).toBe('');
  });

  // A branch typed for one session must not silently become the next
  // session's branch — that would collide or reuse the wrong worktree.
  it('does not leak into the next launcher opening', async () => {
    localStorage.setItem('hive.worktree', '1');
    await open();
    typeBranch('first-only');
    act(() => closeLauncher());
    await open();
    expect(branchBox().value).toBe('');
    press('Enter');
    expect(createSession.mock.calls[0][8]).toBe('');
  });

  it('is cleared when the worktree toggle goes off', async () => {
    localStorage.setItem('hive.worktree', '1');
    await open();
    typeBranch('discard-me');
    toggleWorktree(false);
    press('Enter');
    expect(createSession.mock.calls[0][6]).toBe(false);
    expect(createSession.mock.calls[0][8]).toBe('');
  });

  // The IsGitRepo probe only runs for a project with a cwd, so this
  // case has to seed one — the rest of the suite never needs project
  // state and deliberately leaves it empty.
  it('disappears along with the toggle on a non-git project', async () => {
    localStorage.setItem('hive.worktree', '1');
    isGitRepo.mockResolvedValueOnce(false);
    state.projects = [{ id: 'p1', name: 'p', cwd: '/not-a-repo' }];
    try {
      await open();
      await act(async () => {
        await flushMicrotasks();
      });
      expect(branchBox().classList.contains('hidden')).toBe(true);
      expect(wtBox().checked).toBe(false);
    } finally {
      state.projects = [];
    }
  });
});

describe('launcher resume-in-worktree mode', () => {
  it('offers neither the worktree toggle nor a branch field', async () => {
    await openWith(() =>
      openLauncher('p1', { worktreePath: '/repo/.worktrees/resume' }),
    );
    await settleAgents();
    expect(launcher().querySelector('.launcher-worktree')).toBeNull();
    expect(launcher().querySelector('.launcher-branch')).toBeNull();
  });

  it('passes the worktree path and never asks for a new worktree', async () => {
    // Sticky preference is on; resume mode must still not create one.
    localStorage.setItem('hive.worktree', '1');
    await openWith(() =>
      openLauncher('p1', { worktreePath: '/repo/.worktrees/resume' }),
    );
    await settleAgents();
    press('Enter');
    expect(createSession.mock.calls[0][6]).toBe(false);
    expect(createSession.mock.calls[0][9]).toBe('/repo/.worktrees/resume');
  });

  it('does not leak the path into the next regular opening', async () => {
    await openWith(() =>
      openLauncher('p1', { worktreePath: '/repo/.worktrees/resume' }),
    );
    await settleAgents();
    act(() => closeLauncher());
    await open();
    press('Enter');
    expect(createSession.mock.calls[0][9]).toBe('');
  });
});

describe('launcher stale-response handling', () => {
  it('ignores a ListAgents resolve from a superseded open', async () => {
    // Reopening while the first request is in flight. Both resolve; the
    // stale one must not touch the DOM the second open now owns. The
    // `hidden` guard alone can't catch this — the launcher is visible
    // again by the time the first response lands.
    await openWith(() => openLauncher('p1'));
    const resolveFirst = resolveAgents;
    const first = agentsPromise;
    await openWith(() => openLauncher('p1'));
    const resolveSecond = resolveAgents;
    const second = agentsPromise;

    await act(async () => {
      resolveFirst(AGENTS);
      await first;
      await Promise.resolve();
    });
    await act(async () => {
      resolveSecond(AGENTS);
      await second;
      await Promise.resolve();
    });

    expect(launcher().querySelectorAll('.launcher-worktree')).toHaveLength(1);
    expect(launcher().querySelectorAll('.launcher-branch')).toHaveLength(1);
    expect(Array.from(launcher().children).map((c) => c.className)).toEqual([
      'launcher-search',
      'launcher-worktree',
      'launcher-branch hidden',
      'launcher-list',
    ]);
    expect(names()).toEqual(['Shell', 'Claude', 'Codex CLI']);
  });

  it('does not close a reopened launcher when a superseded request rejects', async () => {
    listAgents.mockImplementationOnce(() => Promise.reject(new Error('boom')));
    await openWith(() => openLauncher('p1'));
    await openWith(() => openLauncher('p1'));
    await act(async () => {
      resolveAgents(AGENTS);
      await agentsPromise;
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(launcher().classList.contains('hidden')).toBe(false);
    expect(names()).toEqual(['Shell', 'Claude', 'Codex CLI']);
  });
});

// Both close paths moved from initLauncher() into the island's effects
// in Phase 3, and they are load-bearing in opposite directions: keyboard.ts
// bails out for the whole window while #launcher is visible, so a launcher
// left open with focus elsewhere has nobody listening for Escape — while a
// close that fires on the very click that OPENED it makes the popup
// unopenable from the sidebar at all.
describe('launcher outside interaction', () => {
  it('closes on a click outside itself', async () => {
    await open();
    act(() => {
      document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(launcher().classList.contains('hidden')).toBe(true);
  });

  it('stays open for a click on the project actions that opened it', async () => {
    const actions = document.createElement('div');
    actions.className = 'hv-project-card__actions';
    document.getElementById('app')?.appendChild(actions);
    try {
      await open();
      act(() => {
        actions.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      expect(launcher().classList.contains('hidden')).toBe(false);
    } finally {
      actions.remove();
    }
  });

  it('stays open for a click on an opener that opts in with data-opens-launcher', async () => {
    const opener = document.createElement('button');
    opener.setAttribute('data-opens-launcher', '');
    document.getElementById('app')?.appendChild(opener);
    try {
      await open();
      act(() => {
        opener.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      expect(launcher().classList.contains('hidden')).toBe(false);
    } finally {
      opener.remove();
    }
  });

  it('closes when focus leaves it for something else', async () => {
    const outside = document.createElement('button');
    document.getElementById('app')?.appendChild(outside);
    try {
      await open();
      act(() => {
        searchBox().dispatchEvent(
          new FocusEvent('focusout', {
            bubbles: true,
            relatedTarget: outside,
          }),
        );
      });
      expect(launcher().classList.contains('hidden')).toBe(true);
    } finally {
      outside.remove();
    }
  });

  // relatedTarget null means focus went nowhere — that is closeLauncher's
  // own blur, so it must not recurse.
  it('ignores a focusout that goes nowhere', async () => {
    await open();
    act(() => {
      searchBox().dispatchEvent(
        new FocusEvent('focusout', { bubbles: true, relatedTarget: null }),
      );
    });
    expect(launcher().classList.contains('hidden')).toBe(false);
  });
});

describe('launcher teardown', () => {
  it('does not throw when closed before it was ever opened', () => {
    expect(() => closeLauncher()).not.toThrow();
  });

  it('does not throw when ListAgents rejects', async () => {
    listAgents.mockImplementationOnce(() => Promise.reject(new Error('boom')));
    await openWith(() => openLauncher('p1'));
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(launcher().classList.contains('hidden')).toBe(true);
  });
});
