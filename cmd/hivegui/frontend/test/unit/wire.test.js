import { describe, it, expect } from 'vitest';
import {
  normalizeSessionInfo, normalizeProjectInfo,
  readProjectId, readWorktreeBranch,
} from '../../src/lib/wire.js';

describe('normalizeSessionInfo', () => {
  it('decodes the daemon\'s snake_case payload', () => {
    const got = normalizeSessionInfo({
      id: 's1', name: 'main', color: '#abc', order: 2,
      created: '2026-01-01T00:00:00Z', alive: true, agent: 'claude',
      project_id: 'p1',
      worktree_path: '/tmp/wt', worktree_branch: 'feat/x',
      last_error: 'boom',
    });
    expect(got).toMatchObject({
      id: 's1', projectId: 'p1', worktreePath: '/tmp/wt',
      worktreeBranch: 'feat/x', lastError: 'boom',
    });
  });
  it('passes camelCase through unchanged', () => {
    const got = normalizeSessionInfo({
      id: 's1', projectId: 'p2', worktreeBranch: 'main',
    });
    expect(got.projectId).toBe('p2');
    expect(got.worktreeBranch).toBe('main');
  });
  it('returns null/undefined inputs verbatim', () => {
    expect(normalizeSessionInfo(null)).toBe(null);
    expect(normalizeSessionInfo(undefined)).toBe(undefined);
  });
  it('defaults missing optional fields to safe empty values', () => {
    const got = normalizeSessionInfo({ id: 's1' });
    expect(got.order).toBe(0);
    expect(got.projectId).toBe('');
    expect(got.worktreeBranch).toBe('');
  });
});

describe('normalizeProjectInfo', () => {
  it('extracts id/name/color/cwd/order/created', () => {
    expect(normalizeProjectInfo({
      id: 'p1', name: 'alpha', color: '#fff', cwd: '/repo', order: 3,
      created: '2026-01-01T00:00:00Z',
    })).toMatchObject({ id: 'p1', name: 'alpha', cwd: '/repo', order: 3 });
  });
});

describe('readProjectId / readWorktreeBranch', () => {
  it('reads either case', () => {
    expect(readProjectId({ project_id: 'snake' })).toBe('snake');
    expect(readProjectId({ projectId: 'camel' })).toBe('camel');
    expect(readProjectId({})).toBe('');
    expect(readWorktreeBranch({ worktree_branch: 'br' })).toBe('br');
    expect(readWorktreeBranch({ worktreeBranch: 'br' })).toBe('br');
  });
});
