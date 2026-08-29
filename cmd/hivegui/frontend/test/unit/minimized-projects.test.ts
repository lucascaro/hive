// Project-level minimization: the persisted id set (src/lib/collapsed.ts
// under a second key) and the hidden-session predicate that unifies it
// with per-session minimization (src/lib/minimized.ts:filterHidden).
import { describe, it, expect } from 'vitest';
import {
  loadCollapsed,
  serializeCollapsed,
  pruneCollapsed,
  MINIMIZED_PROJECTS_STORAGE_KEY,
  COLLAPSED_STORAGE_KEY,
} from '../../src/lib/collapsed.js';
import { filterHidden } from '../../src/lib/minimized.js';

describe('minimized-projects storage', () => {
  it('uses a key distinct from the collapsed set', () => {
    expect(MINIMIZED_PROJECTS_STORAGE_KEY).toBe('hive.minimizedProjects');
    expect(MINIMIZED_PROJECTS_STORAGE_KEY).not.toBe(COLLAPSED_STORAGE_KEY);
  });

  it('round-trips a set of project ids', () => {
    const set = new Set(['p1', 'p3']);
    expect(loadCollapsed(serializeCollapsed(set))).toEqual(set);
  });

  it('degrades to an empty set on garbage', () => {
    expect(loadCollapsed('{not json')).toEqual(new Set());
    expect(loadCollapsed('{"p1":true}')).toEqual(new Set());
    expect(loadCollapsed(null)).toEqual(new Set());
  });

  it('prunes ids for projects that no longer exist', () => {
    const { set, changed } = pruneCollapsed(new Set(['p1', 'gone']), ['p1']);
    expect(changed).toBe(true);
    expect(set).toEqual(new Set(['p1']));
  });
});

describe('filterHidden', () => {
  const sessions = [
    { id: 's1', project_id: 'p1' },
    { id: 's2', project_id: 'p1' },
    { id: 's3', projectId: 'p2' },
  ];
  const ids = (xs: { id: string }[]) => xs.map((s) => s.id);

  it('keeps everything when both sets are empty', () => {
    expect(ids(filterHidden(sessions, new Set(), new Set()))).toEqual([
      's1',
      's2',
      's3',
    ]);
  });

  it('drops individually minimized sessions', () => {
    expect(ids(filterHidden(sessions, new Set(['s2']), new Set()))).toEqual([
      's1',
      's3',
    ]);
  });

  it('drops every session of a minimized project', () => {
    expect(ids(filterHidden(sessions, new Set(), new Set(['p1'])))).toEqual([
      's3',
    ]);
  });

  // The wire carries snake_case; some in-flight objects carry camelCase.
  it('reads both project-id spellings', () => {
    expect(ids(filterHidden(sessions, new Set(), new Set(['p2'])))).toEqual([
      's1',
      's2',
    ]);
  });

  it('tolerates null sets', () => {
    expect(ids(filterHidden(sessions, null, null))).toEqual(['s1', 's2', 's3']);
  });
});
