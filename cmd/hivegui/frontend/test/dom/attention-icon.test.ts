// @vitest-environment jsdom
//
// The sidebar row's state icon (src/app/sidebar.ts) on a bell.
//
// Regression: a bell (events.ts onSessionBell) toggles the `.attention`
// CSS class via updateSidebarSelection but, until this was fixed, never
// touched the row's <svg class="hv-state-icon dot"> — so the icon kept
// showing the "running" triangle (shape) and its <title> kept saying
// "Running" (words) while the row was actually waiting for the user.
// icons.md makes both channels part of the state contract, so this pins
// them to data-state and the <use> href rather than the class, which
// was never the thing lying.
import { describe, it, expect, beforeAll, beforeEach } from 'vitest';
import type { SessionInfo } from '../../src/app/state.js';

let state: typeof import('../../src/app/state.js').state;
let renderSidebar: () => void;
let updateSidebarSelection: () => void;

const noop = () => {};

beforeAll(async () => {
  document.body.innerHTML = `
    <div id="app"><ul id="projects"></ul><div id="status"></div>
    <div id="terms"></div><div id="minimized-tray"></div>
    <div id="empty-state"></div></div>`;
  ({ state } = await import('../../src/app/state.js'));
  const sidebar = await import('../../src/app/sidebar.js');
  ({ renderSidebar, updateSidebarSelection } = sidebar);
  sidebar.initSidebar({
    switchTo: noop,
    switchToProject: noop,
    minimizeProject: noop,
    restoreProject: noop,
    minimizeSession: noop,
    restoreSession: noop,
    confirmAndDeleteProject: noop,
    renderEmptyState: noop,
    refocusActiveTerm: noop,
  });
});

function seed(sessions: SessionInfo[]) {
  state.projects = [{ id: 'p1', name: 'proj', color: '#888' }];
  state.sessions = sessions.map((s) => ({
    project_id: 'p1',
    alive: true,
    phase: '', // PHASE.ready — a session with no phase is in steady state
    ...s,
  }));
  state.collapsed = new Set();
  state.attention = new Set();
  state.activeId = null;
  renderSidebar();
}

function dot(id: string): SVGSVGElement {
  const el = document.querySelector<SVGSVGElement>(
    `.session-item[data-sid="${id}"] .dot`,
  );
  if (!el) throw new Error(`no state icon for ${id}`);
  return el;
}

beforeEach(() => {
  const ul = document.getElementById('projects');
  if (ul) ul.innerHTML = '';
});

describe('sidebar row state icon on attention', () => {
  it('switches to attention when the row gains it, and back when cleared', () => {
    seed([{ id: 'a', name: 'api', order: 0 }]);

    expect(dot('a').dataset.state).toBe('running');
    expect(dot('a').querySelector('use')?.getAttribute('href')).toBe(
      '#hv-state-running',
    );

    state.attention.add('a');
    updateSidebarSelection();

    expect(dot('a').dataset.state).toBe('attention');
    expect(dot('a').querySelector('use')?.getAttribute('href')).toBe(
      '#hv-state-attention',
    );
    expect(dot('a').querySelector('title')?.textContent).toBe(
      'Waiting for you',
    );

    state.attention.delete('a');
    updateSidebarSelection();

    expect(dot('a').dataset.state).toBe('running');
    expect(dot('a').querySelector('use')?.getAttribute('href')).toBe(
      '#hv-state-running',
    );
    expect(dot('a').querySelector('title')?.textContent).toBe('Running');
  });

  it('patches in place rather than rebuilding the row', () => {
    seed([{ id: 'a', name: 'api', order: 0 }]);
    const before = dot('a');
    state.attention.add('a');
    updateSidebarSelection();
    expect(dot('a')).toBe(before);
  });
});
