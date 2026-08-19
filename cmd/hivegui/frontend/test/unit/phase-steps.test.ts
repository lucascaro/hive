import { describe, it, expect } from 'vitest';
import {
  phasePanel,
  PHASE,
  phaseOf,
  isReady,
  isStarting,
  isClosing,
} from '../../src/lib/phase-steps.js';

const labels = (p: ReturnType<typeof phasePanel>) =>
  (p?.steps ?? []).map((s) => `${s.state}:${s.label}`);

describe('phaseOf', () => {
  it('defaults a missing or empty phase to ready', () => {
    expect(phaseOf(undefined)).toBe(PHASE.ready);
    expect(phaseOf({})).toBe(PHASE.ready);
    expect(phaseOf({ phase: '' })).toBe(PHASE.ready);
    expect(isReady(phaseOf({}))).toBe(true);
  });

  it('classifies the create and kill phases', () => {
    expect(isStarting(PHASE.starting)).toBe(true);
    expect(isStarting(PHASE.worktree)).toBe(true);
    expect(isStarting(PHASE.restarting)).toBe(true);
    expect(isClosing(PHASE.checking)).toBe(true);
    expect(isClosing(PHASE.closing)).toBe(true);
    expect(isClosing(PHASE.starting)).toBe(false);
    expect(isStarting(PHASE.closing)).toBe(false);
    expect(isReady(PHASE.ready)).toBe(true);
  });
});

describe('phasePanel', () => {
  it('returns null for a ready session — nothing to overlay', () => {
    expect(phasePanel({ phase: '' })).toBeNull();
  });

  it('returns null for an unknown phase from a newer daemon', () => {
    expect(phasePanel({ phase: 'teleporting' })).toBeNull();
  });

  it('walks the worktree checklist', () => {
    const at = (phase: string) =>
      labels(
        phasePanel({ phase, agent: 'claude', worktreeBranch: 'stone-valley' }),
      );

    expect(at('starting')).toEqual([
      'active:Registered session',
      'todo:Fetching origin',
      'todo:Creating worktree stone-valley',
      'todo:Starting claude',
    ]);
    expect(at('fetching')).toEqual([
      'done:Registered session',
      'active:Fetching origin',
      'todo:Creating worktree stone-valley',
      'todo:Starting claude',
    ]);
    expect(at('spawning')).toEqual([
      'done:Registered session',
      'done:Fetching origin',
      'done:Creating worktree stone-valley',
      'active:Starting claude',
    ]);
  });

  it('skips the git steps without a worktree branch', () => {
    expect(labels(phasePanel({ phase: 'spawning', agent: 'codex' }))).toEqual([
      'done:Registered session',
      'active:Starting codex',
    ]);
  });

  it('labels an agentless session as a shell', () => {
    expect(phasePanel({ phase: 'spawning' })?.status).toBe('Starting shell…');
  });

  it('always has an active step to spin on, even mid-skip', () => {
    // A worktree-less session goes starting → spawning; `fetching`
    // should never appear, but the panel must not end up spinnerless.
    const p = phasePanel({ phase: 'fetching', agent: 'claude' });
    expect(p?.steps.some((s) => s.state === 'active')).toBe(true);
  });

  it('describes the teardown', () => {
    expect(phasePanel({ phase: 'checking' })?.status).toBe(
      'Checking for uncommitted changes…',
    );
    expect(
      labels(phasePanel({ phase: 'closing', worktreeBranch: 'wt' })),
    ).toEqual([
      'done:Checking for uncommitted changes',
      'active:Removing worktree',
    ]);
    expect(labels(phasePanel({ phase: 'closing' }))).toEqual([
      'done:Checking for uncommitted changes',
      'active:Closing session',
    ]);
  });

  it('describes a restart', () => {
    expect(phasePanel({ phase: 'restarting', agent: 'claude' })?.status).toBe(
      'Restarting claude…',
    );
  });
});
