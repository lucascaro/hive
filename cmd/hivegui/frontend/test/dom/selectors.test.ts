import { describe, it, expect, beforeEach } from 'vitest';
import { hiveStateView as state } from '../../src/store/store.js';
import { nextAttentionId } from '../../src/app/selectors.js';

// Three sessions in one project, display order a → b → c. needs_attention
// lives on the session itself now, so `flagged` just sets the field
// rather than a separate local set.
function seed(activeId: string | null, flagged: string[] = []) {
  state.projects = [{ id: 'p1' }];
  state.sessions = [
    { id: 'a', project_id: 'p1', order: 0 },
    { id: 'b', project_id: 'p1', order: 1 },
    { id: 'c', project_id: 'p1', order: 2 },
  ].map((s) => ({ ...s, needs_attention: flagged.includes(s.id) }));
  state.activeId = activeId;
}

describe('nextAttentionId', () => {
  beforeEach(() => seed(null));

  it('returns null when no session has attention', () => {
    seed('a');
    expect(nextAttentionId()).toBeNull();
  });

  it('returns null for an empty session list', () => {
    seed(null);
    state.sessions = [];
    expect(nextAttentionId()).toBeNull();
  });

  it('finds the next flagged session after the active one', () => {
    seed('a', ['c']);
    expect(nextAttentionId()).toBe('c');
  });

  it('picks the nearest flagged session, not the first in the list', () => {
    seed('a', ['b', 'c']);
    expect(nextAttentionId()).toBe('b');
  });

  it('wraps past the end of the list', () => {
    seed('c', ['a']);
    expect(nextAttentionId()).toBe('a');
  });

  it('skips the active session even when it is flagged', () => {
    seed('a', ['a', 'b']);
    expect(nextAttentionId()).toBe('b');
  });

  it('returns null when only the active session is flagged', () => {
    seed('a', ['a']);
    expect(nextAttentionId()).toBeNull();
  });

  it('starts from the top when there is no active session', () => {
    seed(null, ['b', 'c']);
    expect(nextAttentionId()).toBe('b');
  });

  it('crosses project boundaries in display order', () => {
    state.projects = [{ id: 'p1' }, { id: 'p2' }];
    state.sessions = [
      { id: 'a', project_id: 'p1', order: 0 },
      { id: 'z', project_id: 'p2', order: 0, needs_attention: true },
    ];
    state.activeId = 'a';
    expect(nextAttentionId()).toBe('z');
  });
});
