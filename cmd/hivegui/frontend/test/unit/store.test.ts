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
  hydratePersistedProjectSets,
} from '../../src/store/store.js';
import { hiveStateView as state } from '../../src/store/store.js';
import { setTerm, clearTerms } from '../../src/store/terms.js';
import {
  COLLAPSED_STORAGE_KEY,
  MINIMIZED_PROJECTS_STORAGE_KEY,
  namespacedKey,
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
  // Persistence of the collapse/minimize sets is off until the daemon
  // is identified, so every test that asserts a write hydrates first.
  hydratePersistedProjectSets(TEST_NS);
});

afterEach(() => {
  (globalThis as { localStorage?: unknown }).localStorage = undefined;
});

const s = () => appStore.getState();

// The two project-id keys are suffixed with the daemon's state-dir id
// (#340), so every assertion about them has to read the namespaced key.
const TEST_NS = 'ns0';
const CK = namespacedKey(COLLAPSED_STORAGE_KEY, TEST_NS);
const MK = namespacedKey(MINIMIZED_PROJECTS_STORAGE_KEY, TEST_NS);

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
    expect(JSON.parse(store.get(CK) as string)).toEqual(['p1']);

    toggleCollapsed('p1');

    expect(s().collapsed.has('p1')).toBe(false);
    expect(JSON.parse(store.get(CK) as string)).toEqual([]);
  });

  it('applyProjectList prunes collapsed entries for projects that are gone', () => {
    toggleCollapsed('p1');
    toggleCollapsed('p2');

    applyProjectList([{ id: 'p1' }]);

    expect([...s().collapsed]).toEqual(['p1']);
    expect(JSON.parse(store.get(CK) as string)).toEqual(['p1']);
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

// needs_attention is not store state any more — it lives on
// SessionInfo, set by setSessions/updateSession like any other wire
// field (see session-state.test.ts and the frozen transition table in
// docs/exec-plans/completed/336-session-state-model.md). There is nothing
// left here to test in isolation.

describe('minimize', () => {
  it('test_minimizeProject_and_restore_roundtrip', () => {
    applyProjectList([{ id: 'p1' }, { id: 'p2' }]);

    minimizeProject('p1');
    expect([...s().minimizedProjects]).toEqual(['p1']);
    expect(JSON.parse(store.get(MK) as string)).toEqual(['p1']);

    restoreProject('p1');
    expect([...s().minimizedProjects]).toEqual([]);
    expect(JSON.parse(store.get(MK) as string)).toEqual([]);
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

describe('persistence load branches', () => {
  // resetStore() re-reads localStorage, so seeding storage first is what
  // exercises the loadSaved* path. Without this the boot path is never
  // covered: beforeEach clears storage before every reset.
  it('hydrates every persisted field from storage', () => {
    store.set(CK, JSON.stringify(['p1', 'p2']));
    store.set(MK, JSON.stringify(['p3']));
    store.set(VIEW_STORAGE_KEY, 'grid-all');
    store.set('hive.fontSize', '21');
    store.set('hive.sidebarWidth', '333');

    resetStore();
    hydratePersistedProjectSets(TEST_NS);

    expect([...s().collapsed].sort()).toEqual(['p1', 'p2']);
    expect([...s().minimizedProjects]).toEqual(['p3']);
    expect(s().view).toBe('grid-all');
    expect(s().fontSize).toBe(21);
    expect(s().sidebarWidth).toBe(333);
  });

  // The project-id sets are the exception to "hydrated on import": their
  // key is namespaced by the daemon's state-dir id, which only an async
  // binding knows (#340). resetStore alone must leave them empty.
  it('leaves the project-id sets empty until hydration', () => {
    store.set(CK, JSON.stringify(['p1']));
    store.set(MK, JSON.stringify(['p3']));

    resetStore();

    expect([...s().collapsed]).toEqual([]);
    expect([...s().minimizedProjects]).toEqual([]);
    expect(s().projectSetsHydrated).toBe(false);
  });

  // THE REGRESSION GATE for #340. On main, initialData() loads the bare
  // key and applyProjectList prunes it against a project list from a
  // DIFFERENT daemon, persisting [] — which is how one GUI wiped
  // another's tray. After the fix the un-hydrated boot neither loads nor
  // writes that key.
  it('does not touch persisted project sets before hydration', () => {
    store.set(MINIMIZED_PROJECTS_STORAGE_KEY, JSON.stringify(['foreign']));
    store.set(COLLAPSED_STORAGE_KEY, JSON.stringify(['foreign']));

    resetStore(); // no hydratePersistedProjectSets: daemon not identified
    applyProjectList([{ id: 'p1' }, { id: 'p2' }]);

    expect(store.get(MINIMIZED_PROJECTS_STORAGE_KEY)).toBe(
      JSON.stringify(['foreign']),
    );
    expect(store.get(COLLAPSED_STORAGE_KEY)).toBe(JSON.stringify(['foreign']));
    // The list still applied — only the tidy-up was skipped.
    expect(s().projects.map((p) => p.id)).toEqual(['p1', 'p2']);
  });

  // Failing safe means writing nothing: falling back to the bare key is
  // what lets one instance clobber another's state.
  it('persists nothing when the daemon cannot be identified', () => {
    resetStore();
    hydratePersistedProjectSets('');

    minimizeProject('p1');

    expect(s().minimizedProjects.has('p1')).toBe(true);
    expect(store.get(MINIMIZED_PROJECTS_STORAGE_KEY)).toBeUndefined();
    expect(store.get(MK)).toBeUndefined();
  });

  // One-time move of pre-#340 values into this instance's namespace.
  it('adopts the legacy un-namespaced keys once, then removes them', () => {
    store.set(MINIMIZED_PROJECTS_STORAGE_KEY, JSON.stringify(['p3']));
    store.set(COLLAPSED_STORAGE_KEY, JSON.stringify(['p1']));

    resetStore();
    hydratePersistedProjectSets(TEST_NS);

    expect([...s().minimizedProjects]).toEqual(['p3']);
    expect([...s().collapsed]).toEqual(['p1']);
    expect(store.get(MK)).toBe(JSON.stringify(['p3']));
    expect(store.get(MINIMIZED_PROJECTS_STORAGE_KEY)).toBeUndefined();
    expect(store.get(COLLAPSED_STORAGE_KEY)).toBeUndefined();
  });

  // The whole point of #340: a boot under one daemon must not touch the
  // set another daemon's GUI persisted. Before the fix both instances
  // shared one key and each prune wiped the other's ids.
  it("leaves another daemon's set alone across a full boot", () => {
    const otherNS = 'ns1';
    const otherKey = namespacedKey(MINIMIZED_PROJECTS_STORAGE_KEY, otherNS);
    store.set(otherKey, JSON.stringify(['their-project']));

    resetStore();
    hydratePersistedProjectSets(TEST_NS);
    minimizeProject('our-project');
    applyProjectList([{ id: 'our-project' }]);

    expect(store.get(otherKey)).toBe(JSON.stringify(['their-project']));
    expect(JSON.parse(store.get(MK) as string)).toEqual(['our-project']);
  });

  // Restore-then-reboot must come back restored. The empty-set round
  // trip is the case that looks identical to "hydration never ran".
  it('round-trips an emptied set across a reboot', () => {
    minimizeProject('p1');
    restoreProject('p1');

    resetStore();
    hydratePersistedProjectSets(TEST_NS);

    expect([...s().minimizedProjects]).toEqual([]);
    expect(s().projectSetsHydrated).toBe(true);
  });

  it('degrades to defaults on garbage, rather than throwing at boot', () => {
    store.set(COLLAPSED_STORAGE_KEY, 'not json');
    store.set(VIEW_STORAGE_KEY, 'sideways');
    store.set('hive.fontSize', 'huge');
    store.set('hive.sidebarWidth', '');

    expect(() => resetStore()).not.toThrow();

    expect([...s().collapsed]).toEqual([]);
    expect(s().view).toBe('single');
    expect(s().fontSize).toBe(14);
    expect(s().sidebarWidth).toBe(SIDEBAR_MIN_WIDTH);
  });

  it('survives a localStorage that throws on every access', () => {
    // Private-browsing modes throw rather than returning null. Losing a
    // preference must never take the app down at boot.
    (globalThis as { localStorage?: unknown }).localStorage = {
      getItem() {
        throw new Error('denied');
      },
      setItem() {
        throw new Error('denied');
      },
    };

    expect(() => resetStore()).not.toThrow();
    expect(() => toggleCollapsed('p1')).not.toThrow();
    expect(s().collapsed.has('p1')).toBe(true);
  });
});

describe('the subscribe contract', () => {
  it('notifies once per real change and not at all for a no-op', () => {
    setSessions([{ id: 'a' }]);
    let hits = 0;
    const unsub = appStore.subscribe(() => {
      hits++;
    });

    minimizeSession('a');
    expect(hits).toBe(1);

    // Same id again: the helper hands back the SAME Set reference, so
    // there is no state change and no notification. This is the store's
    // central design claim.
    minimizeSession('a');
    expect(hits).toBe(1);

    restoreSession('a');
    expect(hits).toBe(2);

    restoreSession('a'); // already gone — no-op
    expect(hits).toBe(2);

    unsub();
  });

  it('project and session list actions are silent when nothing changes', () => {
    applyProjectList([{ id: 'p1', order: 1 }]);
    setSessions([{ id: 'a' }]);
    const p = s().projects[0];
    const sess = s().sessions[0];
    let hits = 0;
    const unsub = appStore.subscribe(() => {
      hits++;
    });

    addProject(p); // already present
    updateProject(p); // the very object already stored
    updateProject({ id: 'nope' }); // unknown id
    removeProject('nope');
    addSession(sess);
    updateSession(sess);
    updateSession({ id: 'nope' });
    removeSession('nope');

    expect(hits).toBe(0);

    // …and still fire for a genuine change.
    updateProject({ id: 'p1', order: 2 });
    expect(hits).toBe(1);

    unsub();
  });

  it('stops delivering after unsubscribe', () => {
    let hits = 0;
    const unsub = appStore.subscribe(() => {
      hits++;
    });
    minimizeSession('a');
    expect(hits).toBe(1);

    unsub();

    minimizeSession('b');
    setSessions([{ id: 'z' }]);
    expect(hits).toBe(1);
  });
});

describe('window.__hive_state', () => {
  it('test_window_hive_state_shape_unchanged', () => {
    // Asserts against the `state` facade, NOT appStore.getState(): the
    // facade is the object assigned to window.__hive_state, and it
    // carries one field the store does not (`terms`, from the registry).
    // Testing the store instead would keep this green while a deleted
    // facade getter broke ~12 e2e specs.
    const expected = [
      'projects',
      'sessions',
      'collapsed',
      'minimizedProjects',
      'attentionReturnId',
      'attentionRestored',
      'attentionRestoredProjects',
      'nav',
      'minimized',
      'aliveById',
      'phaseById',
      'dismissedDead',
      'terms',
      'activeId',
      'currentProjectId',
      'view',
      'gridProjectId',
      'fontSize',
    ];
    for (const key of expected) {
      expect(
        Reflect.has(state, key),
        `window.__hive_state lost the field: ${key}`,
      ).toBe(true);
    }

    // Types the specs depend on. `terms` is the load-bearing one: specs
    // reach state.terms.get(id).term.buffer.active.
    expect(Array.isArray(state.projects)).toBe(true);
    expect(Array.isArray(state.sessions)).toBe(true);
    expect(state.collapsed).toBeInstanceOf(Set);
    expect(state.minimized).toBeInstanceOf(Set);
    expect(state.aliveById).toBeInstanceOf(Map);
    expect(state.phaseById).toBeInstanceOf(Map);
    expect(state.terms).toBeInstanceOf(Map);
    expect(state.nav).toEqual({ back: [], fwd: [] });

    setActiveId('a');
    expect(state.activeId).toBe('a');
  });

  it('the facade reads through to the store, not a copy', () => {
    setSessions([{ id: 'a' }]);
    expect(state.sessions).toBe(appStore.getState().sessions);

    // And the terms registry is the live map the e2e specs hold on to.
    const reg = state.terms;
    setTerm('a', { host: {} } as never);
    expect(state.terms).toBe(reg);
    expect(state.terms.get('a')).toBeDefined();
    clearTerms();
  });

  it('nav keeps a stable reference — lib/nav-history mutates it in place', () => {
    const nav = state.nav;
    setSessions([{ id: 'a' }]);
    setActiveId('a');
    expect(state.nav).toBe(nav);
  });
});
