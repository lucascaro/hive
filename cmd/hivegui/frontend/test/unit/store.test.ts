// Store contract tests.
//
// These guard the three properties the whole React migration leans on:
// actions replace references (so zustand's reference equality notifies),
// persistence happens inside the owning action, and the shape Playwright
// reads off window.__hive_state does not drift.

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import {
  appStore,
  resetStore,
  setSessions,
  setProjects,
  applyProjectList,
  toggleCollapsed,
  addAttention,
  clearAttentionFor,
  minimizeProject,
  restoreProject,
  minimizeSession,
  restoreSession,
  setActiveId,
  setView,
  setFontSize,
  setSidebarWidth,
  addSession,
  updateSession,
  removeSession,
  forgetSession,
  setAlive,
  setSessionPhase,
  pruneToLiveSessions,
  addProject,
  removeProject,
  updateProject,
  SIDEBAR_MIN_WIDTH,
  SIDEBAR_MAX_WIDTH,
} from '../../src/store/store.js';
import {
  COLLAPSED_STORAGE_KEY,
  MINIMIZED_PROJECTS_STORAGE_KEY,
} from '../../src/lib/collapsed.js';
import { VIEW_STORAGE_KEY } from '../../src/lib/view.js';

// The unit project runs in node, which has no localStorage. A Map-backed
// stub is enough: the store only ever does getItem/setItem.
const store = new Map<string, string>();
const stubStorage = {
  getItem: (k: string) => store.get(k) ?? null,
  setItem: (k: string, v: string) => {
    store.set(k, String(v));
  },
  removeItem: (k: string) => {
    store.delete(k);
  },
  clear: () => store.clear(),
};

beforeEach(() => {
  store.clear();
  (globalThis as { localStorage?: unknown }).localStorage = stubStorage;
  resetStore();
});

afterEach(() => {
  (globalThis as { localStorage?: unknown }).localStorage = undefined;
});

const s = () => appStore.getState();

describe('setSessions', () => {
  it('test_setSessions_replaces_reference_and_notifies', () => {
    const before = s().sessions;
    const seen: number[] = [];
    const unsub = appStore.subscribe((st) => seen.push(st.sessions.length));

    setSessions([{ id: 'a' }, { id: 'b' }]);

    expect(s().sessions).not.toBe(before);
    expect(s().sessions.map((x) => x.id)).toEqual(['a', 'b']);
    expect(seen).toEqual([2]);
    unsub();
  });

  it('add / update / remove each hand back a new array', () => {
    setSessions([{ id: 'a', name: 'one' }]);
    const first = s().sessions;

    addSession({ id: 'b', name: 'two' });
    expect(s().sessions).not.toBe(first);
    expect(s().sessions).toHaveLength(2);

    // Re-adding an existing id is a no-op, not a duplicate.
    addSession({ id: 'b', name: 'two again' });
    expect(s().sessions).toHaveLength(2);

    updateSession({ id: 'a', name: 'renamed' });
    expect(s().sessions[0].name).toBe('renamed');

    removeSession('a');
    expect(s().sessions.map((x) => x.id)).toEqual(['b']);
  });

  it('pruneToLiveSessions drops bookkeeping for sessions that are gone', () => {
    setSessions([{ id: 'a' }, { id: 'b' }]);
    minimizeSession('a');
    minimizeSession('b');
    setAlive('a', true);
    setAlive('b', false);
    setSessionPhase('b', 'ready');

    setSessions([{ id: 'a' }]); // b retired by a snapshot, with no event
    pruneToLiveSessions();

    expect([...s().minimized]).toEqual(['a']);
    expect([...s().aliveById.keys()]).toEqual(['a']);
    expect(s().phaseById.has('b')).toBe(false);
  });

  it('forgetSession clears every per-session map in one write', () => {
    setSessions([{ id: 'a' }]);
    setAlive('a', true);
    setSessionPhase('a', 'ready');
    minimizeSession('a');

    forgetSession('a');

    expect(s().aliveById.has('a')).toBe(false);
    expect(s().phaseById.has('a')).toBe(false);
    expect(s().dismissedDead.has('a')).toBe(false);
    expect(s().minimized.has('a')).toBe(false);
  });
});

describe('collapsed', () => {
  it('test_toggleCollapsed_persists_to_localStorage', () => {
    toggleCollapsed('p1');

    expect(s().collapsed.has('p1')).toBe(true);
    expect(JSON.parse(store.get(COLLAPSED_STORAGE_KEY) as string)).toEqual([
      'p1',
    ]);

    toggleCollapsed('p1');

    expect(s().collapsed.has('p1')).toBe(false);
    expect(JSON.parse(store.get(COLLAPSED_STORAGE_KEY) as string)).toEqual([]);
  });

  it('applyProjectList prunes collapsed entries for projects that are gone', () => {
    toggleCollapsed('p1');
    toggleCollapsed('p2');

    applyProjectList([{ id: 'p1' }]);

    expect([...s().collapsed]).toEqual(['p1']);
    expect(JSON.parse(store.get(COLLAPSED_STORAGE_KEY) as string)).toEqual([
      'p1',
    ]);
  });
});

describe('projects', () => {
  it('setProjects never prunes — only applyProjectList does', () => {
    toggleCollapsed('p1');
    minimizeProject('p1');

    // The sidebar render path and the dom tests assign a project list
    // before the daemon has spoken. Pruning here would wipe the user's
    // persisted collapse/minimize state instead of tidying it.
    setProjects([]);

    expect(s().collapsed.has('p1')).toBe(true);
    expect(s().minimizedProjects.has('p1')).toBe(true);
    expect(s().currentProjectId).toBeNull();
  });
});

describe('attention', () => {
  it('test_markAttention_immutable_set_update', () => {
    const before = s().attention;

    addAttention('a');

    expect(s().attention).not.toBe(before);
    expect(before.has('a')).toBe(false); // the old set was NOT mutated
    expect(s().attention.has('a')).toBe(true);

    // A no-op add keeps the same reference, so it can't wake subscribers.
    const flagged = s().attention;
    addAttention('a');
    expect(s().attention).toBe(flagged);

    // clearAttentionFor mirrors Set.delete's return value.
    expect(clearAttentionFor('a')).toBe(true);
    expect(clearAttentionFor('a')).toBe(false);
    expect(s().attention.has('a')).toBe(false);
  });
});

describe('minimize', () => {
  it('test_minimizeProject_and_restore_roundtrip', () => {
    applyProjectList([{ id: 'p1' }, { id: 'p2' }]);

    minimizeProject('p1');
    expect([...s().minimizedProjects]).toEqual(['p1']);
    expect(
      JSON.parse(store.get(MINIMIZED_PROJECTS_STORAGE_KEY) as string),
    ).toEqual(['p1']);

    restoreProject('p1');
    expect([...s().minimizedProjects]).toEqual([]);
    expect(
      JSON.parse(store.get(MINIMIZED_PROJECTS_STORAGE_KEY) as string),
    ).toEqual([]);
  });

  it('session minimize/restore round-trips without touching projects', () => {
    minimizeSession('a');
    expect(s().minimized.has('a')).toBe(true);
    expect(s().minimizedProjects.size).toBe(0);

    restoreSession('a');
    expect(s().minimized.has('a')).toBe(false);
  });

  it('removeProject drops the id from both persisted sets', () => {
    applyProjectList([{ id: 'p1' }, { id: 'p2' }]);
    toggleCollapsed('p1');
    minimizeProject('p1');

    removeProject('p1');

    expect(s().collapsed.has('p1')).toBe(false);
    expect(s().minimizedProjects.has('p1')).toBe(false);
    // currentProjectId was p1 (first project); it re-points at what's left.
    expect(s().currentProjectId).toBe('p2');
  });

  it('projects stay sorted by order across add and update', () => {
    addProject({ id: 'b', order: 2 });
    addProject({ id: 'a', order: 1 });
    expect(s().projects.map((p) => p.id)).toEqual(['a', 'b']);

    updateProject({ id: 'a', order: 9 });
    expect(s().projects.map((p) => p.id)).toEqual(['b', 'a']);
  });
});

describe('persisted scalars', () => {
  it('setView writes through, and persist:false does not', () => {
    setView('grid-all');
    expect(store.get(VIEW_STORAGE_KEY)).toBe('grid-all');

    setView('single', false);
    expect(s().view).toBe('single');
    expect(store.get(VIEW_STORAGE_KEY)).toBe('grid-all'); // untouched
  });

  it('setFontSize clamps and persists', () => {
    setFontSize(999);
    expect(s().fontSize).toBe(32);
    expect(store.get('hive.fontSize')).toBe('32');
  });

  it('setSidebarWidth clamps to the design-system bounds', () => {
    setSidebarWidth(10);
    expect(s().sidebarWidth).toBe(SIDEBAR_MIN_WIDTH);

    setSidebarWidth(10_000);
    expect(s().sidebarWidth).toBe(SIDEBAR_MAX_WIDTH);
    expect(store.get('hive.sidebarWidth')).toBe(String(SIDEBAR_MAX_WIDTH));
  });
});

describe('window.__hive_state', () => {
  it('test_window_hive_state_shape_unchanged', () => {
    // Every field the Playwright specs and the app read off the state
    // facade. A rename here breaks the e2e suites silently, so the list
    // is spelled out rather than derived.
    const expected = [
      'projects',
      'sessions',
      'collapsed',
      'minimizedProjects',
      'attention',
      'attentionReturnId',
      'attentionRestored',
      'attentionRestoredProjects',
      'nav',
      'minimized',
      'aliveById',
      'phaseById',
      'dismissedDead',
      'activeId',
      'currentProjectId',
      'view',
      'gridProjectId',
      'fontSize',
    ];
    const actual = s();
    for (const key of expected) {
      expect(actual, `missing store field: ${key}`).toHaveProperty(key);
    }

    // Types the specs depend on.
    expect(Array.isArray(actual.projects)).toBe(true);
    expect(Array.isArray(actual.sessions)).toBe(true);
    expect(actual.collapsed).toBeInstanceOf(Set);
    expect(actual.attention).toBeInstanceOf(Set);
    expect(actual.minimized).toBeInstanceOf(Set);
    expect(actual.aliveById).toBeInstanceOf(Map);
    expect(actual.phaseById).toBeInstanceOf(Map);
    expect(actual.nav).toEqual({ back: [], fwd: [] });

    setActiveId('a');
    expect(s().activeId).toBe('a');
  });

  it('nav keeps a stable reference — lib/nav-history mutates it in place', () => {
    const nav = s().nav;
    setSessions([{ id: 'a' }]);
    setActiveId('a');
    expect(s().nav).toBe(nav);
  });
});
