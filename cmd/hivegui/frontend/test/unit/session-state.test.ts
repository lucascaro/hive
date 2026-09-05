import { describe, it, expect } from 'vitest';
import { sessionState, stateTooltip } from '../../src/lib/session-state.js';

describe('sessionState', () => {
  it('is starting for any non-ready phase, alive or not', () => {
    expect(sessionState({ alive: false, phase: 'worktree' })).toBe('starting');
    expect(
      sessionState({ alive: true, phase: 'closing', needs_attention: true }),
    ).toBe('starting');
  });
  it('is running when alive, ready and unflagged', () => {
    expect(sessionState({ alive: true })).toBe('running');
    expect(sessionState({ alive: true, phase: '' })).toBe('running');
  });
  it('is attention when alive and flagged', () => {
    expect(sessionState({ alive: true, needs_attention: true })).toBe(
      'attention',
    );
  });
  it('is exited when dead with no recorded error', () => {
    expect(sessionState({ alive: false })).toBe('exited');
    // A stale bell on a dead session must not outrank the exit.
    expect(sessionState({ alive: false, needs_attention: true })).toBe(
      'exited',
    );
  });
  it('is working when the daemon says the session is producing output', () => {
    expect(sessionState({ alive: true, state: 'working' })).toBe('working');
  });
  it('is steady-shaped for the empty state, which is what an agent and an old daemon both send', () => {
    expect(sessionState({ alive: true, state: '' })).toBe('running');
    expect(sessionState({ alive: true })).toBe('running');
  });
  it('distinguishes a permission prompt from a plain wait', () => {
    expect(sessionState({ alive: true, state: 'waiting_permission' })).toBe(
      'waiting-permission',
    );
    expect(sessionState({ alive: true, state: 'waiting_input' })).toBe(
      'attention',
    );
  });
  it('lets an agent-reported permission prompt outrank the local bell flag', () => {
    // Folding the two together throws away the only distinction the
    // hook tier can make that the heuristic tier cannot.
    expect(
      sessionState({
        alive: true,
        state: 'waiting_permission',
        needs_attention: true,
      }),
    ).toBe('waiting-permission');
  });
  it('lets an unacknowledged bell outrank "working"', () => {
    expect(
      sessionState({ alive: true, state: 'working', needs_attention: true }),
    ).toBe('attention');
  });
  it('is error when the agent reported one, even while alive', () => {
    expect(sessionState({ alive: true, state: 'error' })).toBe('error');
  });
  it('lets death outrank whatever state was last reported', () => {
    expect(sessionState({ alive: false, state: 'working' })).toBe('exited');
  });
  it('is error when dead with a last_error, either spelling', () => {
    expect(sessionState({ alive: false, last_error: 'boom' })).toBe('error');
    expect(sessionState({ alive: false, lastError: 'boom' })).toBe('error');
    expect(sessionState({ alive: false, last_error: '' })).toBe('exited');
  });
});

describe('stateTooltip', () => {
  it('is the state words plus the tier when the agent reported nothing', () => {
    // The heuristic tier knows neither field, and must still produce
    // the one-line tooltip the icon had before prompt/summary existed.
    expect(stateTooltip({ alive: true })).toBe(
      'Idle\nguessed from terminal output',
    );
  });
  it('stacks prompt then summary then tier', () => {
    expect(
      stateTooltip({
        alive: true,
        state: 'waiting_permission',
        state_source: 'hook',
        last_prompt: 'refactor the parser',
        last_summary: 'Needs to run rm -rf build/',
      }),
    ).toBe(
      'Waiting for permission\n“refactor the parser”\nNeeds to run rm -rf build/\nreported by the agent',
    );
  });
  it('drops the lines whose fields are empty', () => {
    expect(
      stateTooltip({
        alive: true,
        state: 'working',
        state_source: 'extension',
        last_prompt: 'ship it',
      }),
    ).toBe('Working\n“ship it”\nreported by the agent');
  });
  it('honours a caller-resolved state so the icon and its words agree', () => {
    // TileChrome resolves with an overriding phase; passing the result
    // back in is what stops the tooltip disagreeing with the glyph.
    expect(stateTooltip({ alive: true }, 'starting')).toBe('Starting');
  });
  it('claims no tier for a state no tier produced', () => {
    // `starting` is resolved here from phase, and a dead session's
    // exited/error comes from the process exiting. Appending "guessed
    // from terminal output" to either invents a provenance.
    expect(stateTooltip({ alive: false, phase: 'worktree' })).toBe('Starting');
    expect(stateTooltip({ alive: false })).toBe('Exited');
    expect(stateTooltip({ alive: false, last_error: 'boom' })).toBe(
      'Exited with an error',
    );
  });
  it('reads any non-empty tier as reported, not just the two we ship', () => {
    // Only the heuristic tier is spelled "" on the wire, so a tier a
    // future daemon adds must not silently read as a guess.
    expect(stateTooltip({ alive: true, state_source: 'something-new' })).toBe(
      'Idle\nreported by the agent',
    );
  });
});
