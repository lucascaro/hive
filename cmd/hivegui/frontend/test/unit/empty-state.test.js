import { describe, it, expect } from 'vitest';
import { emptyStateModel } from '../../src/lib/empty-state.js';

const sess = (id, pid) => ({ id, project_id: pid });

describe('emptyStateModel', () => {
  it('first-run when there are no sessions at all', () => {
    const m = emptyStateModel({ projects: [{ id: 'p1' }], sessions: [], isMac: true });
    expect(m.kind).toBe('first-run');
    expect(m.hint).toContain('⌘T');
    expect(m.actions.map((a) => a.id)).toEqual(['new-session']);
  });

  it('first-run with zero projects also offers New Project', () => {
    const m = emptyStateModel({ projects: [], sessions: [], isMac: false });
    expect(m.kind).toBe('first-run');
    expect(m.hint).toContain('Ctrl+T');
    expect(m.actions.map((a) => a.id)).toEqual(['new-session', 'new-project']);
  });

  it('project-empty when the current project has no sessions', () => {
    const m = emptyStateModel({
      projects: [{ id: 'p1' }, { id: 'p2' }],
      sessions: [sess('s1', 'p1')],
      view: 'single',
      currentProjectId: 'p2',
      isMac: true,
    });
    expect(m.kind).toBe('project-empty');
    expect(m.actions.map((a) => a.id)).toEqual(['new-session']);
  });

  it('grid-project scopes to gridProjectId', () => {
    const m = emptyStateModel({
      projects: [{ id: 'p1' }, { id: 'p2' }],
      sessions: [sess('s1', 'p1')],
      view: 'grid-project',
      gridProjectId: 'p2',
      isMac: true,
    });
    expect(m.kind).toBe('project-empty');
  });

  it('all-minimized in grid when every scoped session is minimized', () => {
    const m = emptyStateModel({
      projects: [{ id: 'p1' }],
      sessions: [sess('s1', 'p1'), sess('s2', 'p1')],
      view: 'grid-all',
      currentProjectId: 'p1',
      minimized: new Set(['s1', 's2']),
      isMac: true,
    });
    expect(m.kind).toBe('all-minimized');
    expect(m.actions).toEqual([]);
  });

  it('null when sessions are visible', () => {
    expect(emptyStateModel({
      projects: [{ id: 'p1' }],
      sessions: [sess('s1', 'p1')],
      view: 'single',
      currentProjectId: 'p1',
      isMac: true,
    })).toBeNull();
    expect(emptyStateModel({
      projects: [{ id: 'p1' }],
      sessions: [sess('s1', 'p1'), sess('s2', 'p1')],
      view: 'grid-all',
      minimized: new Set(['s1']),
      isMac: true,
    })).toBeNull();
  });

  it('minimized sessions do not trigger all-minimized in single view', () => {
    expect(emptyStateModel({
      projects: [{ id: 'p1' }],
      sessions: [sess('s1', 'p1')],
      view: 'single',
      currentProjectId: 'p1',
      minimized: new Set(['s1']),
      isMac: true,
    })).toBeNull();
  });
});
