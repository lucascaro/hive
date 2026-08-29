// @vitest-environment jsdom
//
// The window-title line under each sidebar session name
// (src/app/sidebar.ts + src/lib/term-title.ts).
//
// The regression this file exists for is the in-place update test: a
// title change must patch the existing rows, not rebuild them. A full
// renderSidebar() is an innerHTML wipe that recreates every row and
// listener, and a running program changes its title as it works — taking
// that path would thrash the sidebar and eat dblclick pairs (the bug
// updateSidebarSelection was written for). The daemon gives title
// changes their own SESSION_EVENT kind so this path is reachable at all.
import { describe, it, expect, beforeAll, beforeEach } from 'vitest';
import type { SessionInfo } from '../../src/app/state.js';

let state: typeof import('../../src/app/state.js').state;
let renderSidebar: () => void;
let updateSidebarTitles: () => void;

const noop = () => {};

beforeAll(async () => {
  document.body.innerHTML = `
    <div id="app"><ul id="projects"></ul><div id="status"></div>
    <div id="terms"></div><div id="minimized-tray"></div>
    <div id="empty-state"></div></div>`;
  ({ state } = await import('../../src/app/state.js'));
  const sidebar = await import('../../src/app/sidebar.js');
  ({ renderSidebar, updateSidebarTitles } = sidebar);
  sidebar.initSidebar({
    switchTo: noop,
    switchToProject: noop,
    minimizeProject: noop,
    restoreProject: noop,
    confirmAndDeleteProject: noop,
    renderEmptyState: noop,
    refocusActiveTerm: noop,
  });
});

function seed(sessions: SessionInfo[]) {
  state.projects = [{ id: 'p1', name: 'proj', color: '#888' }];
  state.sessions = sessions.map((s) => ({ project_id: 'p1', ...s }));
  state.collapsed = new Set();
  state.attention = new Set();
  state.activeId = null;
  renderSidebar();
}

function row(id: string): HTMLElement {
  const el = document.querySelector<HTMLElement>(
    `.session-item[data-sid="${id}"]`,
  );
  if (!el) throw new Error(`no sidebar row for ${id}`);
  return el;
}

function titleSlot(id: string): HTMLElement {
  const el = row(id).querySelector<HTMLElement>('.session-title');
  if (!el) throw new Error(`no title slot for ${id}`);
  return el;
}

beforeEach(() => {
  const ul = document.getElementById('projects');
  if (ul) ul.innerHTML = '';
});

describe('sidebar window titles', () => {
  it('renders the title under the session name', () => {
    seed([{ id: 'a', name: 'api', order: 0, title: 'npm run build' }]);

    const r = row('a');
    // Name and title stack inside one wrapper; the dot and swatch stay
    // siblings of that wrapper so they center against the taller row.
    const text = r.querySelector('.session-text');
    expect(text).not.toBeNull();
    expect(text?.querySelector('.name')?.textContent).toBe('api');
    expect(text?.querySelector('.session-title')?.textContent).toBe(
      'npm run build',
    );
    expect(r.querySelector('.dot')?.parentElement).toBe(r);
    expect(r.querySelector('.swatch')?.parentElement).toBe(r);
  });

  it('exposes the untruncated title as a tooltip', () => {
    const long = 'deploying '.repeat(30);
    seed([{ id: 'a', name: 'api', order: 0, title: long }]);
    expect(titleSlot('a').title).toBe(long.trim());
  });

  it('hides the line when the session has no title', () => {
    seed([{ id: 'a', name: 'api', order: 0 }]);
    expect(titleSlot('a').hidden).toBe(true);
    expect(titleSlot('a').textContent).toBe('');
  });

  it('hides the line when the title just echoes the session name', () => {
    seed([{ id: 'a', name: 'api', order: 0, title: 'api' }]);
    expect(titleSlot('a').hidden).toBe(true);
  });

  it('updates titles in place without rebuilding the rows', () => {
    seed([
      { id: 'a', name: 'api', order: 0, title: 'step one' },
      { id: 'b', name: 'web', order: 1, title: 'step one' },
    ]);
    const before = row('a');
    const beforeSlot = titleSlot('a');

    state.sessions[0] = { ...state.sessions[0], title: 'step two' };
    updateSidebarTitles();

    // Same nodes, new text. Node identity is the assertion that matters:
    // a rebuild would pass a textContent check and still break dblclick.
    expect(row('a')).toBe(before);
    expect(titleSlot('a')).toBe(beforeSlot);
    expect(beforeSlot.textContent).toBe('step two');
    expect(titleSlot('b').textContent).toBe('step one');
  });

  it('reveals and hides the line as a title appears and is suppressed', () => {
    seed([{ id: 'a', name: 'api', order: 0 }]);
    expect(titleSlot('a').hidden).toBe(true);

    state.sessions[0] = { ...state.sessions[0], title: 'working' };
    updateSidebarTitles();
    expect(titleSlot('a').hidden).toBe(false);
    expect(titleSlot('a').textContent).toBe('working');

    // A title that becomes the session name is suppressed again rather
    // than left showing a redundant second line.
    state.sessions[0] = { ...state.sessions[0], title: 'api' };
    updateSidebarTitles();
    expect(titleSlot('a').hidden).toBe(true);
  });
});
