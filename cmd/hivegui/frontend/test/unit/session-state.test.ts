import { describe, it, expect } from 'vitest';
import { sessionState } from '../../src/lib/session-state.js';

describe('sessionState', () => {
  it('is starting for any non-ready phase, alive or not', () => {
    expect(sessionState({ alive: false, phase: 'worktree' }, false)).toBe(
      'starting',
    );
    expect(sessionState({ alive: true, phase: 'closing' }, true)).toBe(
      'starting',
    );
  });
  it('is running when alive, ready and unflagged', () => {
    expect(sessionState({ alive: true }, false)).toBe('running');
    expect(sessionState({ alive: true, phase: '' }, false)).toBe('running');
  });
  it('is attention when alive and flagged', () => {
    expect(sessionState({ alive: true }, true)).toBe('attention');
  });
  it('is exited when dead with no recorded error', () => {
    expect(sessionState({ alive: false }, false)).toBe('exited');
    // A stale bell on a dead session must not outrank the exit.
    expect(sessionState({ alive: false }, true)).toBe('exited');
  });
  it('is working when the daemon says the session is producing output', () => {
    expect(sessionState({ alive: true, state: 'working' }, false)).toBe(
      'working',
    );
  });
  it('is steady-shaped for the empty state, which is what an agent and an old daemon both send', () => {
    expect(sessionState({ alive: true, state: '' }, false)).toBe('running');
    expect(sessionState({ alive: true }, false)).toBe('running');
  });
  it('distinguishes a permission prompt from a plain wait', () => {
    expect(
      sessionState({ alive: true, state: 'waiting_permission' }, false),
    ).toBe('waiting-permission');
    expect(sessionState({ alive: true, state: 'waiting_input' }, false)).toBe(
      'attention',
    );
  });
  it('lets an agent-reported permission prompt outrank the local bell flag', () => {
    // Folding the two together throws away the only distinction the
    // hook tier can make that the heuristic tier cannot.
    expect(
      sessionState({ alive: true, state: 'waiting_permission' }, true),
    ).toBe('waiting-permission');
  });
  it('lets an unacknowledged bell outrank "working"', () => {
    expect(sessionState({ alive: true, state: 'working' }, true)).toBe(
      'attention',
    );
  });
  it('is error when the agent reported one, even while alive', () => {
    expect(sessionState({ alive: true, state: 'error' }, false)).toBe('error');
  });
  it('lets death outrank whatever state was last reported', () => {
    expect(sessionState({ alive: false, state: 'working' }, false)).toBe(
      'exited',
    );
  });
  it('is error when dead with a last_error, either spelling', () => {
    expect(sessionState({ alive: false, last_error: 'boom' }, false)).toBe(
      'error',
    );
    expect(sessionState({ alive: false, lastError: 'boom' }, false)).toBe(
      'error',
    );
    expect(sessionState({ alive: false, last_error: '' }, false)).toBe(
      'exited',
    );
  });
});
