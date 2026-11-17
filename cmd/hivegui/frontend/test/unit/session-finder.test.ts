// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { describe, expect, it } from 'vitest';
import {
  finderRows,
  fuzzyScore,
  type FinderProject,
  type FinderSession,
} from '../../src/lib/session-finder.js';

describe('fuzzyScore', () => {
  it('is null for a non-match', () => {
    expect(fuzzyScore('xyz', 'fix-auth-bug')).toBeNull();
  });
  it('matches a subsequence, case-insensitively', () => {
    expect(fuzzyScore('FAB', 'fix-auth-bug')).not.toBeNull();
  });
  it('treats an empty query as a match', () => {
    expect(fuzzyScore('', 'anything')).not.toBeNull();
  });
  it('treats undefined text as no match, never a throw', () => {
    expect(fuzzyScore('a', undefined)).toBeNull();
    expect(fuzzyScore('a', '')).toBeNull();
  });
  it('ranks contiguous above scattered', () => {
    const tight = fuzzyScore('auth', 'auth-fix') as number;
    const loose = fuzzyScore('auth', 'a-u-t-h-fix') as number;
    expect(tight).toBeGreaterThan(loose);
  });
  it('ranks a word-start hit above a mid-word hit', () => {
    const start = fuzzyScore('bug', 'fix-bug') as number;
    const mid = fuzzyScore('bug', 'debugger') as number;
    expect(start).toBeGreaterThan(mid);
  });
  it('ranks a prefix above a later word-start hit', () => {
    const prefix = fuzzyScore('fix', 'fix-bug') as number;
    const later = fuzzyScore('fix', 'my-fix') as number;
    expect(prefix).toBeGreaterThan(later);
  });
});

const projects: FinderProject[] = [
  { id: 'p1', name: 'alpha' },
  { id: 'p2', name: 'beta' },
];
const s = (o: Partial<FinderSession> & { id: string }): FinderSession => ({
  project_id: 'p1',
  ...o,
});
const rows = (
  sessions: FinderSession[],
  query: string,
  back: string[] = [],
  active: string | null = null,
) =>
  finderRows({ sessions, projects, back, activeId: active, query }).map(
    (r) => r.id,
  );

describe('finderRows — one field at a time (spec criterion 2)', () => {
  const sessions = [
    s({ id: 'a', name: 'zzzname' }),
    s({ id: 'b', name: 'q', title: 'zzztitle' }),
    s({ id: 'c', name: 'q', worktree_branch: 'zzzbranch' }),
    s({ id: 'd', name: 'q', worktreeBranch: 'zzzcamel' }),
    s({ id: 'e', name: 'q', project_id: 'p2' }),
  ];
  it('matches the session name', () => {
    expect(rows(sessions, 'zzzname')).toEqual(['a']);
  });
  it('matches the OSC title', () => {
    expect(rows(sessions, 'zzztitle')).toEqual(['b']);
  });
  it('matches the branch, snake_case spelling', () => {
    expect(rows(sessions, 'zzzbranch')).toEqual(['c']);
  });
  it('matches the branch, camelCase spelling', () => {
    expect(rows(sessions, 'zzzcamel')).toEqual(['d']);
  });
  it('matches the project name', () => {
    expect(rows(sessions, 'beta')).toEqual(['e']);
  });
  it('matches a projectId-spelled session', () => {
    const only = [{ id: 'x', name: 'n', projectId: 'p2' }];
    expect(rows(only, 'beta')).toEqual(['x']);
  });
});

describe('finderRows — terms and ranking', () => {
  it('ANDs whitespace-separated terms across different fields', () => {
    const sessions = [
      s({ id: 'a', name: 'login', worktree_branch: 'main' }),
      s({ id: 'b', name: 'login', worktree_branch: 'feat' }),
    ];
    expect(rows(sessions, 'login feat')).toEqual(['b']);
  });
  it('ranks the better match first', () => {
    const sessions = [
      s({ id: 'loose', name: 'f-i-x' }),
      s({ id: 'tight', name: 'fix' }),
    ];
    expect(rows(sessions, 'fix')).toEqual(['tight', 'loose']);
  });
  it('keeps input order on a tie', () => {
    const sessions = [
      s({ id: 'a', name: 'same' }),
      s({ id: 'b', name: 'same' }),
    ];
    expect(rows(sessions, 'same')).toEqual(['a', 'b']);
  });
  it('never throws or prints "undefined" for sessions with no fields', () => {
    const bare = [{ id: 'bare' }];
    const out = finderRows({
      sessions: bare,
      projects: [],
      back: [],
      activeId: null,
      query: '',
    });
    expect(out).toHaveLength(1);
    expect(JSON.stringify(out[0])).not.toContain('undefined');
    expect(out[0].branch).toBe('');
  });
  it('is derived from the live inputs on every call (rename / branch change)', () => {
    const before = [s({ id: 'a', name: 'old', worktree_branch: 'one' })];
    const after = [s({ id: 'a', name: 'new', worktree_branch: 'two' })];
    expect(rows(before, 'new')).toEqual([]);
    expect(rows(after, 'new')).toEqual(['a']);
    expect(rows(after, 'two')).toEqual(['a']);
    expect(rows(after, 'one')).toEqual([]);
  });
});

describe('finderRows — empty query is recency order (criterion 3)', () => {
  const sessions = [
    s({ id: 'a' }),
    s({ id: 'b' }),
    s({ id: 'c' }),
    s({ id: 'd' }),
  ];
  it('puts the previously focused session first and the active one last', () => {
    // visited a, then b, then c; c is active; back holds departures.
    expect(rows(sessions, '', ['a', 'b'], 'c')).toEqual(['b', 'a', 'd', 'c']);
  });
  it('A→B→A: active A, back=[A,B] → B first, A last', () => {
    expect(rows(sessions, '', ['a', 'b'], 'a')).toEqual(['b', 'c', 'd', 'a']);
  });
  it('skips ids in back that no longer exist and dedupes', () => {
    expect(rows(sessions, '', ['gone', 'b', 'a', 'b'], 'c')).toEqual([
      'b',
      'a',
      'd',
      'c',
    ]);
  });
  it('falls back to painted order with no history', () => {
    expect(rows(sessions, '', [], 'a')).toEqual(['b', 'c', 'd', 'a']);
  });
});

describe('finderRows — attention ranking (spec 562)', () => {
  // A live, ready session: sessionState() resolves these to `running`
  // unless a flag below says otherwise.
  const live = (o: Partial<FinderSession> & { id: string }): FinderSession =>
    s({ alive: true, ...o });
  const waiting = { state: 'waiting_input' };

  it('keeps the previous session first even when others are flagged', () => {
    const sessions = [
      live({ id: 'a', ...waiting }),
      live({ id: 'b' }),
      live({ id: 'c' }),
    ];
    // left b for c: b is previous, a is flagged, c is active.
    expect(rows(sessions, '', ['b'], 'c')).toEqual(['b', 'a', 'c']);
  });

  it.each([
    ['attention (bell)', { needs_attention: true }],
    ['attention (waiting for input)', { state: 'waiting_input' }],
    ['waiting-permission', { state: 'waiting_permission' }],
    ['failed (live, daemon error)', { state: 'error' }],
    ['error (dead with last_error)', { alive: false, last_error: 'boom' }],
  ])('ranks a %s session ahead of unflagged ones', (_, flag) => {
    const sessions = [
      live({ id: 'prev' }),
      live({ id: 'x' }),
      live({ id: 'y' }),
      live({ id: 'flagged', ...flag }),
      live({ id: 'me' }),
    ];
    expect(rows(sessions, '', ['prev'], 'me')).toEqual([
      'prev',
      'flagged',
      'x',
      'y',
      'me',
    ]);
  });

  it('leads with flagged sessions when there is no history', () => {
    const sessions = [live({ id: 'a' }), live({ id: 'b', ...waiting })];
    expect(rows(sessions, '', [], null)).toEqual(['b', 'a']);
  });

  it('keeps recency order within the flagged and unflagged groups', () => {
    const sessions = [
      live({ id: 'a', ...waiting }),
      live({ id: 'b' }),
      live({ id: 'c', ...waiting }),
      live({ id: 'd' }),
      live({ id: 'e' }),
    ];
    // back (most recent last): d, c, b, a; e active. Recency: a b c d.
    expect(rows(sessions, '', ['d', 'c', 'b', 'a'], 'e')).toEqual([
      'a',
      'c',
      'b',
      'd',
      'e',
    ]);
  });

  it('lists a flagged previous session once, first', () => {
    const sessions = [
      live({ id: 'a', ...waiting }),
      live({ id: 'b', ...waiting }),
      live({ id: 'c' }),
    ];
    expect(rows(sessions, '', ['a'], 'c')).toEqual(['a', 'b', 'c']);
  });

  it('keeps the active session last even when flagged', () => {
    const sessions = [
      live({ id: 'a', ...waiting }),
      live({ id: 'b' }),
      live({ id: 'c' }),
    ];
    expect(rows(sessions, '', [], 'a')).toEqual(['b', 'c', 'a']);
  });

  it('does not flag a starting or exited session', () => {
    const sessions = [
      live({ id: 'plain' }),
      live({ id: 'starting', phase: 'starting', needs_attention: true }),
      live({ id: 'dead', alive: false, needs_attention: true }),
    ];
    expect(rows(sessions, '', [], null)).toEqual(['plain', 'starting', 'dead']);
  });

  it('a flag that clears is ranked unflagged on the next call', () => {
    const before = [live({ id: 'a' }), live({ id: 'b', ...waiting })];
    const after = [live({ id: 'a' }), live({ id: 'b' })];
    expect(rows(before, '')).toEqual(['b', 'a']);
    expect(rows(after, '')).toEqual(['a', 'b']);
  });

  it('typed query: a better-scoring unflagged match beats a worse flagged one', () => {
    const sessions = [
      live({ id: 'loose', name: 'f-i-x', ...waiting }),
      live({ id: 'tight', name: 'fix' }),
    ];
    expect(rows(sessions, 'fix')).toEqual(['tight', 'loose']);
  });

  it('typed query: at equal score the flagged session comes first', () => {
    const sessions = [
      live({ id: 'calm', name: 'same' }),
      live({ id: 'flagged', name: 'same', ...waiting }),
    ];
    expect(rows(sessions, 'same', ['calm'])).toEqual(['flagged', 'calm']);
  });

  it('typed query: the active session is never treated as flagged', () => {
    const sessions = [
      live({ id: 'me', name: 'same', ...waiting }),
      live({ id: 'other', name: 'same' }),
    ];
    expect(rows(sessions, 'same', [], 'me')).toEqual(['other', 'me']);
  });
});

describe('finderRows — scale (criterion 7)', () => {
  it('filters 100 sessions well inside the budget', () => {
    // Every third one flagged, so the partition and tie-break are timed.
    const many = Array.from({ length: 100 }, (_, i) =>
      s({
        id: `s${i}`,
        name: `session-${i}`,
        worktree_branch: `feat/x-${i}`,
        alive: true,
        state: i % 3 === 0 ? 'waiting_input' : undefined,
      }),
    );
    const t0 = performance.now();
    for (let i = 0; i < 20; i++) {
      rows(many, 'feat 7');
      rows(many, '');
    }
    expect((performance.now() - t0) / 20).toBeLessThan(20);
  });
});
