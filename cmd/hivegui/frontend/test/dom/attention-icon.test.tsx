// @vitest-environment jsdom
//
// The sidebar row's state icon (components/SessionRow.tsx) on a bell.
//
// Regression: a bell toggled the `.attention` CSS class but, until this
// was fixed, never touched the row's
// <svg class="hv-state-icon dot"> — so the icon kept showing the
// "running" triangle (shape) and its <title> kept saying "Idle"
// (words) while the row was actually waiting for the user. icons.md
// makes both channels part of the state contract, so this pins them to
// data-state and the <use> href rather than the class, which was never
// the thing lying.
import { describe, it, expect, beforeAll, beforeEach } from 'vitest';
import type { SessionInfo } from '../../src/app/state.js';
import * as store from '../../src/store/store.js';
import {
  card,
  loadSidebar,
  mountSidebar,
  seed,
  update,
} from './sidebar-harness.js';

let Sidebar: Awaited<ReturnType<typeof loadSidebar>>;

beforeAll(async () => {
  Sidebar = await loadSidebar();
});

function withSessions(sessions: SessionInfo[]) {
  seed({
    projects: [{ id: 'p1', name: 'proj', color: '#888' }],
    sessions: sessions.map((s) => ({
      project_id: 'p1',
      alive: true,
      phase: '', // PHASE.ready — a session with no phase is in steady state
      ...s,
    })),
    collapsed: new Set(),
    activeId: null,
  });
  mountSidebar(Sidebar);
}

function dot(id: string): SVGSVGElement {
  const el = document.querySelector<SVGSVGElement>(
    `.hv-session-row[data-sid="${id}"] .hv-session-row__state`,
  );
  if (!el) throw new Error(`no state icon for ${id}`);
  return el;
}

// needs_attention lives on the session itself — there is no local set
// left to poke, so a "bell" in these tests is a session patch.
function setAttn(id: string, want: boolean) {
  const s = store.appStore.getState().sessions.find((x) => x.id === id);
  if (!s) throw new Error(`no session ${id}`);
  store.updateSession({ ...s, needs_attention: want });
}

beforeEach(() => {
  seed({});
});

describe('sidebar row state icon on attention', () => {
  it('switches to attention when the row gains it, and back when cleared', () => {
    withSessions([{ id: 'a', name: 'api', order: 0 }]);

    expect(dot('a').dataset.state).toBe('running');
    expect(dot('a').querySelector('use')?.getAttribute('href')).toBe(
      '#hv-state-running',
    );

    update(() => setAttn('a', true));

    expect(dot('a').dataset.state).toBe('attention');
    expect(dot('a').querySelector('use')?.getAttribute('href')).toBe(
      '#hv-state-attention',
    );
    expect(dot('a').querySelector('title')?.textContent).toBe(
      'Waiting for you',
    );

    update(() => setAttn('a', false));

    expect(dot('a').dataset.state).toBe('running');
    expect(dot('a').querySelector('use')?.getAttribute('href')).toBe(
      '#hv-state-running',
    );
    expect(dot('a').querySelector('title')?.textContent).toBe('Idle');
  });

  it('renders the daemon state glyphs the sidebar gained in spec 336', () => {
    withSessions([
      { id: 'w', name: 'busy', order: 0, state: 'working' },
      { id: 'p', name: 'blocked', order: 1, state: 'waiting_permission' },
    ]);

    expect(dot('w').dataset.state).toBe('working');
    expect(dot('w').querySelector('use')?.getAttribute('href')).toBe(
      '#hv-state-working',
    );
    expect(dot('w').querySelector('title')?.textContent).toBe('Working');

    // The distinction the whole state model exists for: a yes/no the
    // agent is blocked on does not look like a bell.
    expect(dot('p').dataset.state).toBe('waiting-permission');
    expect(dot('p').querySelector('use')?.getAttribute('href')).toBe(
      '#hv-state-waiting-permission',
    );
    expect(dot('p').querySelector('title')?.textContent).toBe(
      'Waiting for permission',
    );
  });

  it('patches in place rather than rebuilding the row', () => {
    withSessions([{ id: 'a', name: 'api', order: 0 }]);
    const before = dot('a');
    update(() => setAttn('a', true));
    expect(dot('a')).toBe(before);
  });
});

// Attention on a card is the union of its sessions' (patterns.md ›
// Attention bubbling), and while the card is collapsed it is also the
// count line. Both have to move on a bell that arrives with no other
// change — the imperative card had a separate patch path that once
// covered only part of this.
describe('project card on a store update', () => {
  it('bubbles a new bell to the card and refreshes the collapsed count', () => {
    withSessions([{ id: 'a', name: 'api', order: 0 }]);
    update(() => store.toggleCollapsed('p1'));

    const count = () =>
      card('p1')?.querySelector('.hv-project-card__count')?.textContent;

    expect(card('p1')?.dataset.state).toBeUndefined();
    expect(count()).toBe('1 session');

    const before = card('p1');
    update(() => setAttn('a', true));

    expect(card('p1')).toBe(before); // re-rendered, not rebuilt
    expect(card('p1')?.dataset.state).toBe('attention');
    expect(count()).toBe('1 session · 1 needs you');

    update(() => setAttn('a', false));
    expect(card('p1')?.dataset.state).toBeUndefined();
    expect(count()).toBe('1 session');
  });

  it('moves the active marker between cards without a rebuild', () => {
    seed({
      projects: [
        { id: 'p1', name: 'one', color: '#888' },
        { id: 'p2', name: 'two', color: '#888' },
      ],
      sessions: [],
      collapsed: new Set(),
      activeId: null,
      currentProjectId: 'p1',
    });
    mountSidebar(Sidebar);

    const before = card('p1');
    expect(card('p1')?.dataset.active).toBe('');
    expect(card('p2')?.dataset.active).toBeUndefined();

    update(() => store.setCurrentProjectId('p2'));
    expect(card('p1')).toBe(before);
    expect(card('p1')?.dataset.active).toBeUndefined();
    expect(card('p2')?.dataset.active).toBe('');
  });
});
