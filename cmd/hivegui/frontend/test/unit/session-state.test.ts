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
