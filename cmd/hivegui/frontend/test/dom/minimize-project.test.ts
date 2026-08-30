// @vitest-environment jsdom
//
// Minimizing a whole project (src/app/sidebar.ts + src/app/view.ts).
//
// Two invariants this file exists for. First: restore is positional —
// minimizing never touches the project's Order, so a restored project
// must land back at the exact index it had, with no stored position.
// Second: a drop that reorders visible projects has to produce the same
// Order index it would produce with nothing minimized, because the
// daemon's index space still counts the minimized project.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';

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

let state: typeof import('../../src/app/state.js').state;
let renderSidebar: () => void;
let bridge: typeof import('../../src/bridge.js');
let gridScopeFor: typeof import('../../src/app/view.js').gridScopeFor;
let minimizeProject: typeof import('../../src/app/view.js').minimizeProject;
let switchToProject: typeof import('../../src/app/view.js').switchToProject;
let shiftActiveProject: typeof import('../../src/app/view.js').shiftActiveProject;
let initView: typeof import('../../src/app/view.js').initView;
let minimizeSession: typeof import('../../src/app/view.js').minimizeSession;
let navSession: typeof import('../../src/app/keyboard.js').navSession;
let reorderActive: typeof import('../../src/app/keyboard.js').reorderActive;

const noop = () => {};

beforeAll(async () => {
  document.body.innerHTML = `
    <div id="app">
      <ul id="projects"></ul>
      <ul id="minimized-projects" class="hidden"></ul>
      <div id="status"></div><div id="terms"></div>
      <div id="minimized-tray"></div><div id="empty-state"></div>
    </div>`;
  // view.ts installs a container ResizeObserver at module load; jsdom
  // has none, and nothing here exercises grid layout.
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  ({ state } = await import('../../src/app/state.js'));
  bridge = await import('../../src/bridge.js');
  const view = await import('../../src/app/view.js');
  gridScopeFor = view.gridScopeFor;
  minimizeProject = view.minimizeProject;
  switchToProject = view.switchToProject;
  shiftActiveProject = view.shiftActiveProject;
  initView = view.initView;
  minimizeSession = view.minimizeSession;
  ({ navSession, reorderActive } = await import('../../src/app/keyboard.js'));
  const sidebar = await import('../../src/app/sidebar.js');
  renderSidebar = sidebar.renderSidebar;
  sidebar.initSidebar({
    switchTo: noop,
    switchToProject: noop,
    minimizeProject: view.minimizeProject,
    restoreProject: view.restoreProject,
    minimizeSession: view.minimizeSession,
    restoreSession: view.restoreSession,
    confirmAndDeleteProject: noop,
    renderEmptyState: noop,
    refocusActiveTerm: noop,
  });
});

beforeEach(() => {
  localStorage.clear();
  state.projects = [
    { id: 'p1', name: 'one', color: '#111', order: 0 },
    { id: 'p2', name: 'two', color: '#222', order: 1 },
    { id: 'p3', name: 'three', color: '#333', order: 2 },
  ];
  state.sessions = [
    { id: 's1', name: 's1', project_id: 'p1', order: 0 },
    { id: 's2', name: 's2', project_id: 'p2', order: 1 },
    { id: 's3', name: 's3', project_id: 'p3', order: 2 },
  ];
  state.collapsed = new Set();
  state.minimized = new Set();
  state.minimizedProjects = new Set();
  state.attention = new Set();
  state.activeId = null;
  state.currentProjectId = null;
  state.view = 'single';
  vi.mocked(bridge.UpdateProject).mockClear();
  renderSidebar();
});

const listedPIDs = () =>
  [...document.querySelectorAll<HTMLElement>('#projects > .project')].map(
    (el) => el.dataset.pid,
  );
const chipPIDs = () =>
  [...document.querySelectorAll<HTMLElement>('.min-project-chip')].map(
    (el) => el.dataset.pid,
  );

function click(sel: string) {
  const el = document.querySelector<HTMLElement>(sel);
  if (!el) throw new Error(`no element for ${sel}`);
  el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
}

function minimize(pid: string) {
  click(
    `.project[data-pid="${pid}"] .project-actions button[aria-label^="Minimize"]`,
  );
}

describe('minimize project', () => {
  it('moves the project out of the list and into the tray', () => {
    minimize('p2');
    expect(listedPIDs()).toEqual(['p1', 'p3']);
    expect(chipPIDs()).toEqual(['p2']);
    expect(
      document.querySelector('.min-project-chip .min-project-name')
        ?.textContent,
    ).toBe('two');
  });

  it('restores the project to its original index', () => {
    minimize('p2');
    click('.min-project-chip[data-pid="p2"] .min-project-restore');
    expect(listedPIDs()).toEqual(['p1', 'p2', 'p3']);
    expect(chipPIDs()).toEqual([]);
  });

  it('restores from a click anywhere on the row, not just the ＋', () => {
    minimize('p2');
    click('.min-project-chip[data-pid="p2"] .min-project-open');
    expect(listedPIDs()).toEqual(['p1', 'p2', 'p3']);
    expect(chipPIDs()).toEqual([]);
  });

  it('hides the tray while nothing is minimized', () => {
    const tray = document.getElementById('minimized-projects');
    expect(tray?.classList.contains('hidden')).toBe(true);
    minimize('p1');
    expect(tray?.classList.contains('hidden')).toBe(false);
    click('.min-project-chip[data-pid="p1"] .min-project-restore');
    expect(tray?.classList.contains('hidden')).toBe(true);
  });

  it('persists the minimized set', () => {
    minimize('p3');
    expect(
      JSON.parse(localStorage.getItem('hive.minimizedProjects') ?? '[]'),
    ).toEqual(['p3']);
    click('.min-project-chip[data-pid="p3"] .min-project-restore');
    expect(
      JSON.parse(localStorage.getItem('hive.minimizedProjects') ?? '[]'),
    ).toEqual([]);
  });

  // updateSidebarSelection, not renderSidebar: switching projects
  // repaints selection in place, so a chip that only learned its state
  // at render time would keep a stale highlight.
  it('moves the active highlight between chips without a rebuild', async () => {
    const { updateSidebarSelection } = await import('../../src/app/sidebar.js');
    minimize('p1');
    minimize('p2');
    const chip = (pid: string) =>
      document.querySelector(`.min-project-chip[data-pid="${pid}"]`);
    state.currentProjectId = 'p1';
    updateSidebarSelection();
    expect(chip('p1')?.classList.contains('active')).toBe(true);
    expect(chip('p2')?.classList.contains('active')).toBe(false);

    state.currentProjectId = 'p2';
    updateSidebarSelection();
    expect(chip('p1')?.classList.contains('active')).toBe(false);
    expect(chip('p2')?.classList.contains('active')).toBe(true);
  });

  // A minimized project has no session rows, so a BEL inside it would
  // otherwise be invisible in the sidebar until ⌘B found it. The chip
  // is the only surface left to carry it.
  it('marks the chip when a session inside the project rings', async () => {
    const { updateSidebarSelection } = await import('../../src/app/sidebar.js');
    const chip = (pid: string) =>
      document.querySelector(`.min-project-chip[data-pid="${pid}"]`);
    state.attention = new Set(['s2']);
    minimize('p1');
    minimize('p2');
    expect(chip('p2')?.classList.contains('attention')).toBe(true);
    expect(chip('p1')?.classList.contains('attention')).toBe(false);

    // Clearing repaints in place, the same path clearAttention takes.
    state.attention.delete('s2');
    updateSidebarSelection();
    expect(chip('p2')?.classList.contains('attention')).toBe(false);
  });

  it('marks the chip active when it is the current project', () => {
    state.currentProjectId = 'p2';
    minimize('p2');
    expect(
      document
        .querySelector('.min-project-chip[data-pid="p2"]')
        ?.classList.contains('active'),
    ).toBe(true);
  });

  // The prune lives in the project:list / project:event handlers, not
  // in renderSidebar: renderSidebar runs before the first project list
  // arrives, and pruning against an empty state.projects there would
  // wipe the persisted set instead of trimming it.
  it('keeps the set intact across a render with no projects loaded yet', () => {
    minimize('p3');
    state.projects = [];
    renderSidebar();
    expect(state.minimizedProjects.has('p3')).toBe(true);
    expect(
      JSON.parse(localStorage.getItem('hive.minimizedProjects') ?? '[]'),
    ).toEqual(['p3']);
  });

  it("hides the project's sessions from grid views", () => {
    minimize('p2');
    expect(gridScopeFor('grid-all').map((s) => s.id)).toEqual(['s1', 's3']);
    expect(gridScopeFor('grid-project', 'p2')).toEqual([]);
    // The sessions are hidden, not gone.
    expect(state.sessions.map((s) => s.id)).toEqual(['s1', 's2', 's3']);
  });
});

describe('session rows', () => {
  const rowBtn = (sid: string) =>
    document.querySelector<HTMLButtonElement>(
      `.session-item[data-sid="${sid}"] .session-minimize`,
    );

  it('minimizes a session from its sidebar row', () => {
    rowBtn('s2')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(state.minimized.has('s2')).toBe(true);
    // Same effect as the grid tile's control: out of the grid, still in
    // the session list.
    expect(gridScopeFor('grid-all').map((x) => x.id)).toEqual(['s1', 's3']);
    expect(state.sessions.some((x) => x.id === 's2')).toBe(true);
  });

  it('toggles back to restore, and marks the row while minimized', () => {
    rowBtn('s2')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    const row = document.querySelector(`.session-item[data-sid="s2"]`);
    expect(row?.classList.contains('minimized')).toBe(true);
    expect(rowBtn('s2')?.textContent).toBe('＋');

    rowBtn('s2')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(state.minimized.has('s2')).toBe(false);
    expect(
      document
        .querySelector(`.session-item[data-sid="s2"]`)
        ?.classList.contains('minimized'),
    ).toBe(false);
  });

  it('does not switch to the session the button sits on', () => {
    state.activeId = 's1';
    rowBtn('s2')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(state.activeId).toBe('s1');
  });
});

describe('reordering around a minimized project', () => {
  // Drops carry the Order index the daemon will splice at. That index
  // counts every project, minimized or not — so the value must not
  // change just because one of them stopped being rendered.
  function drop(draggedPID: string, targetPID: string) {
    const target = document.querySelector<HTMLElement>(
      `.project[data-pid="${targetPID}"]`,
    );
    if (!target) throw new Error(`no row for ${targetPID}`);
    const dt = {
      types: ['text/x-hive-project'],
      dropEffect: '',
      getData: () => draggedPID,
    };
    const ev = new Event('drop', { bubbles: true, cancelable: true });
    Object.defineProperty(ev, 'dataTransfer', { value: dt });
    // jsdom rects are all-zero, so the hit test lands "below" the
    // target header — the branch this asserts on.
    Object.defineProperty(ev, 'clientY', { value: 0 });
    target.dispatchEvent(ev);
  }

  it('computes the same Order with and without a minimized neighbor', () => {
    drop('p3', 'p1');
    expect(vi.mocked(bridge.UpdateProject)).toHaveBeenCalledWith(
      'p3',
      '',
      '',
      '',
      1,
    );

    vi.mocked(bridge.UpdateProject).mockClear();
    minimize('p2');
    drop('p3', 'p1');
    expect(vi.mocked(bridge.UpdateProject)).toHaveBeenCalledWith(
      'p3',
      '',
      '',
      '',
      1,
    );
  });
});

// The grid branches are where the risk lives: everything above runs in
// single mode, where the hidden-session filter is not consulted at all.
// A selection that lands on a session the grid filters out is the
// "sidebar moves, nothing appears, keystrokes vanish" failure.
// The prune's real home: the daemon events that carry authoritative
// project data.
describe('project events', () => {
  async function handlers() {
    const { wireDaemonEvents } = await import('../../src/app/events.js');
    const { createScrollTrace } = await import('../../src/lib/scroll-debug.js');
    vi.mocked(bridge.EventsOn).mockClear();
    wireDaemonEvents({
      switchTo: noop,
      renderMinimizedTray: noop,
      renderGrid: noop,
      enforceViewFloor: noop,
      updateAppTitle: noop,
      focusActiveTerm: noop,
      refocusActiveTerm: noop,
      isDaemonRestarting: () => false,
      scrollTrace: createScrollTrace({ enabled: false }),
    });
    const map = new Map<string, (json: string) => void>();
    for (const [name, fn] of vi.mocked(bridge.EventsOn).mock.calls) {
      map.set(name as string, fn as (json: string) => void);
    }
    return map;
  }

  it('drops the minimized id when the project is deleted', async () => {
    minimize('p3');
    const h = await handlers();
    h.get('project:event')?.(
      JSON.stringify({ kind: 'removed', project: { id: 'p3' } }),
    );
    expect(state.minimizedProjects.has('p3')).toBe(false);
    expect(
      JSON.parse(localStorage.getItem('hive.minimizedProjects') ?? '[]'),
    ).toEqual([]);
  });

  it('prunes ids missing from an authoritative project list', async () => {
    minimize('p3');
    const h = await handlers();
    h.get('project:list')?.(
      JSON.stringify({ projects: [{ id: 'p1' }, { id: 'p2' }] }),
    );
    expect(state.minimizedProjects.has('p3')).toBe(false);
  });

  it('prunes against an empty authoritative list', async () => {
    // An empty list means every project really is gone — unlike the
    // empty state.projects a pre-boot renderSidebar would see.
    minimize('p3');
    const h = await handlers();
    h.get('project:list')?.(JSON.stringify({ projects: [] }));
    expect(state.minimizedProjects.has('p3')).toBe(false);
  });
});

describe('grid views', () => {
  const setActive = vi.fn((id: string | null) => {
    state.activeId = id;
  });

  beforeEach(async () => {
    setActive.mockClear();
    const { createScrollTrace } = await import('../../src/lib/scroll-debug.js');
    initView({
      // No xterm in jsdom: renderGrid only reaches for the tile's host
      // element and the show/hide pair.
      ensureTerm: (info) => {
        const existing = state.terms.get(info.id);
        if (existing) return existing;
        const tile = {
          host: document.createElement('div'),
          attached: true,
          needsReattach: false,
          deadOverlayShown: false,
          phase: '',
          setPhase() {},
          revealAfterReplay() {},
          ensureAttached() {},
          show() {},
          hide() {},
          rebaselineReplayCols() {},
          _onBodyResize() {},
          setInfo() {},
          setProject() {},
          setDead() {},
          writeData() {},
          destroy() {},
          _closeDead() {},
          _dismissDead() {},
        };
        state.terms.set(info.id, tile);
        return tile;
      },
      setActive,
      focusActiveTerm: () => {},
      scrollTrace: createScrollTrace({ enabled: false }),
    });
    state.terms = new Map();
    state.view = 'grid-all';
    state.activeId = 's2';
  });

  it('hands focus to a still-visible session when minimizing its project', () => {
    minimizeProject('p2');
    expect(state.activeId).not.toBe('s2');
    expect(gridScopeFor('grid-all').map((s) => s.id)).toContain(
      state.activeId as string,
    );
  });

  it('activates a visible sibling rather than a hidden first session', () => {
    // p1 keeps s1 minimized individually but has a visible sibling, so
    // selecting the project must not tear the user out of the grid.
    state.sessions.push({ id: 's1b', name: 's1b', project_id: 'p1', order: 3 });
    state.minimized = new Set(['s1']);
    switchToProject('p1');
    expect(state.activeId).toBe('s1b');
    expect(state.view).toBe('grid-all');
  });

  it('leaves the saved view preference alone when it falls back', () => {
    localStorage.setItem('hive.view', 'grid-all');
    minimizeProject('p2');
    state.view = 'grid-all';
    switchToProject('p2');
    expect(state.view).toBe('single');
    // The fallback is forced, not chosen — the next launch should still
    // come up in the grid the user picked.
    expect(localStorage.getItem('hive.view')).toBe('grid-all');
  });

  it('falls back to single when selecting a minimized project', () => {
    minimizeProject('p2');
    state.view = 'grid-all';
    switchToProject('p2');
    // The session is selected but has no tile in the grid, so the view
    // drops to single rather than focusing an invisible terminal.
    expect(state.activeId).toBe('s2');
    expect(state.view).toBe('single');
  });

  it('creates the tile for a session it has never rendered', () => {
    // showSingle only shows an existing tile, and this path does not go
    // through switchTo — without ensureTerm the fallback lands on a
    // blank pane. The normal case after a restart.
    state.terms = new Map();
    state.currentProjectId = 'p1';
    state.activeId = 's1';
    shiftActiveProject(1);
    expect(state.activeId).toBe('s2');
    expect(state.terms.has('s2')).toBe(true);
  });

  // ⌘[ / ⌘] can no longer LAND on a minimized project (see the
  // navigation suite below), but the fallback still has to fire for a
  // visible project whose sessions are all individually minimized —
  // that is the case this asserts.
  it('falls back to single when the target project has no visible session', () => {
    minimizeSession('s2');
    state.view = 'grid-all';
    state.currentProjectId = 'p1';
    state.activeId = 's1';
    shiftActiveProject(1);
    expect(state.currentProjectId).toBe('p2');
    expect(state.view).toBe('single');
  });

  it('leaves a grid view alone when the target project is visible', () => {
    state.currentProjectId = 'p1';
    state.activeId = 's1';
    switchToProject('p3');
    expect(state.view).toBe('grid-all');
    expect(state.activeId).toBe('s3');
  });
});

// Navigation must step OVER anything minimized. Before this, ⌘↓ walked
// the raw orderedSessions() and ⌘] walked state.projects unfiltered, so
// a project you put in the tray still landed under the cursor — and in
// a grid view the hidden-session fallback then dropped you to single.
// These run against the real view.ts, unlike keyboard-arrows.test.ts,
// which mocks it (a mocked isSessionHidden is falsy and would pass
// regardless of what ships).
describe('keyboard navigation skips minimized things', () => {
  beforeEach(async () => {
    const { createScrollTrace } = await import('../../src/lib/scroll-debug.js');
    initView({
      ensureTerm: (info) => {
        const existing = state.terms.get(info.id);
        if (existing) return existing;
        const tile = {
          host: document.createElement('div'),
          attached: true,
          needsReattach: false,
          deadOverlayShown: false,
          phase: '',
          setPhase() {},
          revealAfterReplay() {},
          ensureAttached() {},
          show() {},
          hide() {},
          rebaselineReplayCols() {},
          _onBodyResize() {},
          setInfo() {},
          setProject() {},
          setDead() {},
          writeData() {},
          destroy() {},
          _closeDead() {},
          _dismissDead() {},
        };
        state.terms.set(info.id, tile);
        return tile;
      },
      setActive: (id: string | null) => {
        state.activeId = id;
      },
      focusActiveTerm: () => {},
      scrollTrace: createScrollTrace({ enabled: false }),
    });
    state.terms = new Map();
    vi.mocked(bridge.UpdateSession).mockClear();
  });

  it('⌘↓ skips a session inside a minimized project', () => {
    minimizeProject('p2');
    state.activeId = 's1';
    navSession(+1);
    expect(state.activeId).toBe('s3');
  });

  it('⌘↑ skips an individually-minimized session', () => {
    minimizeSession('s2');
    state.activeId = 's3';
    navSession(-1);
    expect(state.activeId).toBe('s1');
  });

  it('stays put when every other session is hidden', () => {
    minimizeSession('s2');
    minimizeProject('p3');
    state.activeId = 's1';
    navSession(+1);
    expect(state.activeId).toBe('s1');
  });

  it('seeds on the first VISIBLE session when nothing is active', () => {
    // state.activeId is null whenever an empty project is selected or
    // the last session was closed. switchTo does not un-minimize, so
    // seeding on ord[0] blind would drop the user into the tray.
    minimizeProject('p1');
    state.activeId = null;
    navSession(+1);
    expect(state.activeId).toBe('s2');
  });

  it('⇧⌘↓ still reorders across a minimized sibling', () => {
    // The reorder branch sends an index into the daemon's GLOBAL order
    // space, which counts hidden sessions — filtering it would scatter
    // sessions. s1b is minimized and must still be a valid target.
    state.sessions.push({ id: 's1b', name: 's1b', project_id: 'p1', order: 1 });
    minimizeSession('s1b');
    state.activeId = 's1';
    reorderActive(+1);
    expect(vi.mocked(bridge.UpdateSession)).toHaveBeenCalledWith(
      's1',
      '',
      '',
      1,
    );
  });

  it('⌘] skips a minimized project', () => {
    minimizeProject('p2');
    state.currentProjectId = 'p1';
    state.activeId = 's1';
    shiftActiveProject(+1);
    expect(state.currentProjectId).toBe('p3');
  });

  it('⌘[ skips a minimized project', () => {
    minimizeProject('p3');
    state.currentProjectId = 'p1';
    state.activeId = 's1';
    shiftActiveProject(-1);
    expect(state.currentProjectId).toBe('p2');
  });

  it('stays on the current project when every other project is minimized', () => {
    minimizeProject('p2');
    minimizeProject('p3');
    state.currentProjectId = 'p1';
    state.activeId = 's1';
    shiftActiveProject(+1);
    expect(state.currentProjectId).toBe('p1');
  });
});
