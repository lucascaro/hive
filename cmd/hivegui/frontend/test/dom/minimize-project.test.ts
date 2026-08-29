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
  const sidebar = await import('../../src/app/sidebar.js');
  renderSidebar = sidebar.renderSidebar;
  sidebar.initSidebar({
    switchTo: noop,
    switchToProject: noop,
    minimizeProject: view.minimizeProject,
    restoreProject: view.restoreProject,
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

  it('marks the chip active when it is the current project', () => {
    state.currentProjectId = 'p2';
    minimize('p2');
    expect(
      document
        .querySelector('.min-project-chip[data-pid="p2"]')
        ?.classList.contains('active'),
    ).toBe(true);
  });

  it('drops dead ids from the persisted set', () => {
    minimize('p3');
    state.projects = state.projects.filter((p) => p.id !== 'p3');
    renderSidebar();
    expect(state.minimizedProjects.has('p3')).toBe(false);
    expect(
      JSON.parse(localStorage.getItem('hive.minimizedProjects') ?? '[]'),
    ).toEqual([]);
  });

  it("hides the project's sessions from grid views", () => {
    minimize('p2');
    expect(gridScopeFor('grid-all').map((s) => s.id)).toEqual(['s1', 's3']);
    expect(gridScopeFor('grid-project', 'p2')).toEqual([]);
    // The sessions are hidden, not gone.
    expect(state.sessions.map((s) => s.id)).toEqual(['s1', 's2', 's3']);
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
