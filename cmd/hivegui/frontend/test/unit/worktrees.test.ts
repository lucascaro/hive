import { describe, it, expect } from 'vitest';
import {
  classifyWorktree,
  deleteBlockers,
  canDelete,
  canRename,
  needsConfirm,
  statusLabel,
  sortWorktrees,
  sortBranches,
  branchStatusLabel,
  shortPath,
  deleteConfirmMessage,
  readSessionIds,
  readRepoRoot,
  readOrphanBranches,
  readWorktrees,
  readProjectIdOf,
  readIsMain,
  type WorktreeInfo,
} from '../../src/lib/worktrees.js';

// A pristine detached worktree — the baseline every case varies from.
function wt(overrides: Partial<WorktreeInfo> = {}): WorktreeInfo {
  return { path: '/repo/.worktrees/feature', branch: 'feature', ...overrides };
}

describe('classifyWorktree', () => {
  it('flags the main checkout', () => {
    expect(classifyWorktree(wt({ is_main: true }))).toBe('main');
  });

  it('flags a worktree with sessions as active', () => {
    expect(classifyWorktree(wt({ session_ids: ['s1'] }))).toBe('active');
  });

  it('treats uncommitted changes as holding work', () => {
    expect(classifyWorktree(wt({ uncommitted: true }))).toBe('holding');
  });

  it('treats unpushed commits as holding work', () => {
    expect(classifyWorktree(wt({ unpushed: 2 }))).toBe('holding');
  });

  // The conservative direction: an unanswerable base must never read
  // as disposable.
  it('treats an unknown comparison base as holding work', () => {
    expect(classifyWorktree(wt({ unknown: true }))).toBe('holding');
  });

  it('calls a clean detached worktree idle', () => {
    expect(classifyWorktree(wt())).toBe('idle');
  });

  it('prefers active over holding when both apply', () => {
    expect(
      classifyWorktree(wt({ session_ids: ['s1'], uncommitted: true })),
    ).toBe('active');
  });
});

describe('deleteBlockers', () => {
  it('returns nothing for a clean detached worktree', () => {
    expect(deleteBlockers(wt())).toEqual([]);
    expect(canDelete(wt())).toBe(true);
    expect(needsConfirm(wt())).toBe(false);
  });

  it('blocks the main checkout absolutely and says nothing else', () => {
    const blockers = deleteBlockers(wt({ is_main: true, uncommitted: true }));
    expect(blockers).toHaveLength(1);
    expect(blockers[0].absolute).toBe(true);
    expect(canDelete(wt({ is_main: true }))).toBe(false);
  });

  it('blocks a worktree with live sessions absolutely', () => {
    const blockers = deleteBlockers(wt({ session_ids: ['s1', 's2'] }));
    expect(blockers[0].absolute).toBe(true);
    expect(blockers[0].reason).toContain('2 sessions');
    expect(canDelete(wt({ session_ids: ['s1'] }))).toBe(false);
  });

  it('singularises the session count', () => {
    expect(deleteBlockers(wt({ session_ids: ['s1'] }))[0].reason).toContain(
      '1 session is running',
    );
  });

  // Order mirrors the daemon's refusal order in
  // registry.RemoveWorktree, so the message shown is the one the
  // daemon would actually send back.
  it('orders in-use before dirty before unpushed', () => {
    const blockers = deleteBlockers(
      wt({ session_ids: ['s1'], uncommitted: true, unpushed: 3 }),
    );
    expect(blockers.map((b) => b.absolute)).toEqual([true, false, false]);
    expect(blockers[1].reason).toContain('uncommitted');
    expect(blockers[2].reason).toContain('3 commits');
  });

  it('reports dirty and unpushed as overridable', () => {
    const w = wt({ uncommitted: true, unpushed: 1 });
    expect(deleteBlockers(w).every((b) => !b.absolute)).toBe(true);
    expect(canDelete(w)).toBe(true);
    expect(needsConfirm(w)).toBe(true);
  });

  it('explains an unknown base instead of implying it is clean', () => {
    const blockers = deleteBlockers(wt({ unknown: true }));
    expect(blockers).toHaveLength(1);
    expect(blockers[0].reason).toContain('could not be compared');
    expect(needsConfirm(wt({ unknown: true }))).toBe(true);
  });

  it('does not double-report unknown when a count is available', () => {
    const blockers = deleteBlockers(wt({ unpushed: 2, unknown: true }));
    expect(blockers).toHaveLength(1);
    expect(blockers[0].reason).toContain('2 commits');
  });
});

describe('canRename', () => {
  it('allows a detached-from-sessions branch worktree', () => {
    expect(canRename(wt())).toBe(true);
  });

  // Moving the directory out from under a running shell is the whole
  // reason this is refused.
  it('refuses while a session is inside', () => {
    expect(canRename(wt({ session_ids: ['s1'] }))).toBe(false);
  });

  it('refuses the main checkout', () => {
    expect(canRename(wt({ is_main: true }))).toBe(false);
  });

  it('refuses a detached HEAD (no branch to rename)', () => {
    expect(canRename(wt({ detached: true, branch: '' }))).toBe(false);
  });

  it('allows renaming a worktree that merely holds work', () => {
    expect(canRename(wt({ uncommitted: true, unpushed: 4 }))).toBe(true);
  });
});

describe('statusLabel', () => {
  it('says clean when there is nothing to report', () => {
    expect(statusLabel(wt())).toBe('clean');
  });

  it('joins every condition', () => {
    const label = statusLabel(
      wt({ session_ids: ['a', 'b'], uncommitted: true, unpushed: 1 }),
    );
    expect(label).toBe('2 sessions · uncommitted changes · 1 unpushed commit');
  });

  it('names the main checkout', () => {
    expect(statusLabel(wt({ is_main: true }))).toContain('main checkout');
  });

  // The main checkout is never probed, so its unknown flag is noise.
  it('does not report a missing remote for the main checkout', () => {
    expect(statusLabel(wt({ is_main: true, unknown: true }))).not.toContain(
      'no remote',
    );
  });

  it('reports a missing remote for a detached worktree', () => {
    expect(statusLabel(wt({ unknown: true }))).toContain(
      'no remote to compare',
    );
  });
});

describe('sortWorktrees', () => {
  it('orders main, then active, then holding, then idle', () => {
    const list = [
      wt({ path: '/d', branch: 'idle' }),
      wt({ path: '/c', branch: 'holding', uncommitted: true }),
      wt({ path: '/b', branch: 'active', session_ids: ['s'] }),
      wt({ path: '/a', branch: 'main', is_main: true }),
    ];
    expect(sortWorktrees(list).map((w) => w.branch)).toEqual([
      'main',
      'active',
      'holding',
      'idle',
    ]);
  });

  it('breaks ties on branch and is stable across calls', () => {
    const list = [
      wt({ branch: 'b', path: '/2' }),
      wt({ branch: 'a', path: '/1' }),
    ];
    expect(sortWorktrees(list).map((w) => w.branch)).toEqual(['a', 'b']);
    expect(sortWorktrees(sortWorktrees(list)).map((w) => w.branch)).toEqual([
      'a',
      'b',
    ]);
  });

  it('does not mutate its input', () => {
    const list = [wt({ branch: 'b' }), wt({ branch: 'a' })];
    sortWorktrees(list);
    expect(list.map((w) => w.branch)).toEqual(['b', 'a']);
  });
});

describe('sortBranches', () => {
  it('puts unmerged branches first', () => {
    const sorted = sortBranches([
      { name: 'done', merged: true },
      { name: 'wip' },
    ]);
    expect(sorted.map((b) => b.name)).toEqual(['wip', 'done']);
  });

  it('sorts alphabetically within a group', () => {
    const sorted = sortBranches([{ name: 'zeta' }, { name: 'alpha' }]);
    expect(sorted.map((b) => b.name)).toEqual(['alpha', 'zeta']);
  });
});

describe('branchStatusLabel', () => {
  it('falls back to "no worktree"', () => {
    expect(branchStatusLabel({ name: 'x' })).toBe('no worktree');
  });

  it('reports merged state, ahead count and upstream', () => {
    expect(
      branchStatusLabel({
        name: 'x',
        merged: true,
        ahead: 2,
        upstream: 'origin/x',
      }),
    ).toBe('merged · 2 commits ahead · tracks origin/x');
  });
});

describe('shortPath', () => {
  it('returns the last segment', () => {
    expect(shortPath('/repo/.worktrees/feature-x')).toBe('feature-x');
  });

  it('tolerates a trailing slash', () => {
    expect(shortPath('/repo/.worktrees/feature-x/')).toBe('feature-x');
  });

  it('returns the input when there is no separator', () => {
    expect(shortPath('bare')).toBe('bare');
  });
});

describe('deleteConfirmMessage', () => {
  it('promises the branch is kept by default', () => {
    const msg = deleteConfirmMessage(wt(), false);
    expect(msg).toContain('/repo/.worktrees/feature');
    expect(msg).toContain('branch “feature” is kept');
  });

  it('says both go when the branch is opted in', () => {
    const msg = deleteConfirmMessage(wt(), true);
    expect(msg).toContain('branch “feature” will both be deleted');
    expect(msg).toContain('cannot be undone');
  });

  // The note must not argue against the blocker above it: keeping the
  // branch preserves commits, and nothing preserves uncommitted work.
  it('does not promise recoverability when uncommitted work will die', () => {
    const msg = deleteConfirmMessage(wt({ uncommitted: true }), false);
    expect(msg).toContain('uncommitted changes');
  });

  it('lists every blocker so nothing is lost silently', () => {
    const msg = deleteConfirmMessage(
      wt({ uncommitted: true, unpushed: 2 }),
      false,
    );
    expect(msg).toContain('uncommitted changes');
    expect(msg).toContain('2 commits that are not pushed');
  });

  it('handles a detached worktree with no branch', () => {
    const msg = deleteConfirmMessage(wt({ branch: '', detached: true }), true);
    expect(msg).toContain('The directory will be deleted');
  });
});

// The daemon speaks snake_case; older GUI paths carried camelCase.
// Both must read, or the browser silently renders an empty list.
describe('wire field readers', () => {
  it('reads snake_case', () => {
    expect(readIsMain({ path: '/p', is_main: true })).toBe(true);
    expect(readSessionIds({ path: '/p', session_ids: ['a'] })).toEqual(['a']);
    expect(readRepoRoot({ repo_root: '/r' })).toBe('/r');
    expect(readProjectIdOf({ project_id: 'p1' })).toBe('p1');
    expect(
      readOrphanBranches({ orphan_branches: [{ name: 'x' }] }),
    ).toHaveLength(1);
  });

  it('reads camelCase', () => {
    expect(readIsMain({ path: '/p', isMain: true })).toBe(true);
    expect(readSessionIds({ path: '/p', sessionIds: ['a'] })).toEqual(['a']);
    expect(readRepoRoot({ repoRoot: '/r' })).toBe('/r');
    expect(readProjectIdOf({ projectId: 'p1' })).toBe('p1');
    expect(
      readOrphanBranches({ orphanBranches: [{ name: 'x' }] }),
    ).toHaveLength(1);
  });

  it('defaults to empty rather than throwing', () => {
    expect(readSessionIds({ path: '/p' })).toEqual([]);
    expect(readWorktrees({})).toEqual([]);
    expect(readOrphanBranches({})).toEqual([]);
    expect(readRepoRoot({})).toBe('');
  });
});
