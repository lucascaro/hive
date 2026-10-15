// Pure filtering and grouping behind the archived-sessions list (spec 518).
import { describe, expect, it } from 'vitest';
import { filterArchived, groupArchived } from '../../src/lib/archived.js';
import type { ArchivedSessionInfo } from '../../src/app/state.js';

const A = (over: Partial<ArchivedSessionInfo>): ArchivedSessionInfo => ({
  session_id: 'x',
  name: 'x',
  archived_at: '2026-10-01T00:00:00Z',
  ...over,
});
const projectName = (a: ArchivedSessionInfo) => a.project_name ?? 'none';

const LIST = [
  A({
    session_id: '1',
    name: 'api refactor',
    project_id: 'p1',
    project_name: 'hive',
    agent: 'claude',
  }),
  A({
    session_id: '2',
    name: 'docs pass',
    project_id: 'p2',
    project_name: 'site',
    agent: 'pi',
    worktree_branch: 'feat/docs',
  }),
  A({
    session_id: '3',
    name: 'api tests',
    project_id: 'p1',
    project_name: 'hive',
    agent: 'pi',
  }),
];

describe('filterArchived', () => {
  it('returns everything for an empty or blank query', () => {
    expect(filterArchived(LIST, '', projectName)).toHaveLength(3);
    expect(filterArchived(LIST, '   ', projectName)).toHaveLength(3);
  });

  it('matches name, project, agent and branch case-insensitively', () => {
    const ids = (q: string) =>
      filterArchived(LIST, q, projectName).map((a) => a.session_id);
    expect(ids('API')).toEqual(['1', '3']);
    expect(ids('site')).toEqual(['2']);
    expect(ids('claude')).toEqual(['1']);
    expect(ids('feat/docs')).toEqual(['2']);
  });

  it('requires every word to match, so more words narrow', () => {
    const ids = (q: string) =>
      filterArchived(LIST, q, projectName).map((a) => a.session_id);
    expect(ids('api claude')).toEqual(['1']);
    expect(ids('api nope')).toEqual([]);
  });
});

describe('groupArchived', () => {
  it('groups by project, in order of first appearance, keeping row order', () => {
    const g = groupArchived(LIST, projectName);
    expect(g.map((x) => [x.name, x.items.map((a) => a.session_id)])).toEqual([
      ['hive', ['1', '3']],
      ['site', ['2']],
    ]);
  });

  it('puts sessions with no project in one group', () => {
    const g = groupArchived(
      [A({ session_id: 'a' }), A({ session_id: 'b' })],
      projectName,
    );
    expect(g).toHaveLength(1);
    expect(g[0].key).toBe('');
  });
});
