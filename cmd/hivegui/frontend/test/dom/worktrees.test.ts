// @vitest-environment jsdom
//
// Covers the worktree browser (src/app/modals/worktrees.ts). The
// row-classification logic is unit-tested in test/unit/worktrees.test.ts;
// what this file pins down is the part that can destroy work:
//
//   • a row that cannot be deleted is DISABLED, with the reason visible
//   • the three-way delete dialog gates the destructive call, and
//     cancelling it sends nothing
//   • force is sent only when something actually blocks
//   • delete_branch follows the button the user pressed
//   • a reply for a different project never repaints the open list
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { Mock } from 'vitest';

const ListWorktrees = vi.fn((_p: string): Promise<void> => Promise.resolve());
const RemoveWorktree = vi.fn(
  (
    _p: string,
    _path: string,
    _force: boolean,
    _branch: boolean,
  ): Promise<void> => Promise.resolve(),
);
const CreateWorktree = vi.fn(
  (_p: string, _b: string): Promise<void> => Promise.resolve(),
);
const RenameWorktree = vi.fn(
  (_p: string, _path: string, _b: string): Promise<void> => Promise.resolve(),
);
const DeleteBranch = vi.fn(
  (_p: string, _b: string, _force: boolean): Promise<void> => Promise.resolve(),
);
const flashStatus = vi.fn();

// Forwarded variadically so a mock that drops an argument the real
// binding gained still fails toHaveBeenCalledWith.
vi.mock('../../src/bridge.js', () => ({
  ListWorktrees: (...a: Parameters<typeof ListWorktrees>) =>
    ListWorktrees(...a),
  RemoveWorktree: (...a: Parameters<typeof RemoveWorktree>) =>
    RemoveWorktree(...a),
  CreateWorktree: (...a: Parameters<typeof CreateWorktree>) =>
    CreateWorktree(...a),
  RenameWorktree: (...a: Parameters<typeof RenameWorktree>) =>
    RenameWorktree(...a),
  DeleteBranch: (...a: Parameters<typeof DeleteBranch>) => DeleteBranch(...a),
}));

vi.mock('../../src/app/dom.js', () => ({
  flashStatus: (...a: unknown[]) => flashStatus(...a),
  setStatus: vi.fn(),
  reportFailure: () => () => {},
}));

// worktrees.ts builds its own dialog now (the dialog primitive), so the
// fixture is only the app root it mounts into.
const MARKUP = `<div id="app"></div>`;

type WorktreesModule = typeof import('../../src/app/modals/worktrees.js');
let openWorktrees: WorktreesModule['openWorktrees'];
let closeWorktrees: WorktreesModule['closeWorktrees'];
let initWorktrees: WorktreesModule['initWorktrees'];
let handleWorktreesPayload: WorktreesModule['handleWorktreesPayload'];
let refocusActiveTerm: Mock<() => void>;
let setFocusedTile: Mock<(id: string | null) => void>;
let openSessionIn: Mock<(projectId: string, worktreePath: string) => void>;

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  ({ openWorktrees, closeWorktrees, initWorktrees, handleWorktreesPayload } =
    await import('../../src/app/modals/worktrees.js'));
  refocusActiveTerm = vi.fn();
  setFocusedTile = vi.fn();
  openSessionIn = vi.fn();
  initWorktrees({ setFocusedTile, refocusActiveTerm, openSessionIn });
});

beforeEach(() => {
  for (const m of [
    ListWorktrees,
    RemoveWorktree,
    CreateWorktree,
    RenameWorktree,
    DeleteBranch,
    flashStatus,
  ]) {
    m.mockReset();
  }
  ListWorktrees.mockResolvedValue(undefined);
  RemoveWorktree.mockResolvedValue(undefined);
  CreateWorktree.mockResolvedValue(undefined);
  RenameWorktree.mockResolvedValue(undefined);
  DeleteBranch.mockResolvedValue(undefined);
  refocusActiveTerm.mockReset();
  setFocusedTile.mockReset();
  openSessionIn.mockReset();
  closeWorktrees();
});

const el = <T extends HTMLElement = HTMLElement>(id: string): T =>
  document.getElementById(id) as T;
const rows = () => [
  ...el('worktrees-list').querySelectorAll<HTMLElement>('.worktree-row'),
];
const branchRows = () => [
  ...el('worktrees-branches').querySelectorAll<HTMLElement>('.worktree-row'),
];
// Buttons are identified by their label, which is also what the user
// clicks — a renamed button should fail these tests.
const button = (row: Element, label: string): HTMLButtonElement => {
  const found = [...row.querySelectorAll('button')].find(
    (b) => b.textContent === label,
  );
  return found as HTMLButtonElement;
};
const flush = () => new Promise((r) => setTimeout(r, 0));

const PROJECT = { id: 'p1', name: 'hive', cwd: '/repo' };

// The daemon's reply shape (snake_case, as on the wire).
function payload(over: Record<string, unknown> = {}) {
  return {
    project_id: 'p1',
    repo_root: '/repo',
    worktrees: [{ path: '/repo', branch: 'main', is_main: true }],
    orphan_branches: [],
    ...over,
  };
}

// open + deliver one inventory, the state every test starts from.
async function openWith(p: Record<string, unknown>) {
  openWorktrees(PROJECT);
  await flush();
  handleWorktreesPayload(p);
}

describe('opening', () => {
  it('requests the inventory for the project it was opened on', async () => {
    openWorktrees(PROJECT);
    await flush();
    expect(ListWorktrees).toHaveBeenCalledWith('p1');
    expect(el('worktrees').classList.contains('hidden')).toBe(false);
    // The modal owns the keyboard while open.
    expect(setFocusedTile).toHaveBeenCalledWith(null);
  });

  // The loading card must disappear once the inventory lands. It did
  // not: this codebase has no global `.hidden { display: none }` rule
  // — every element declares its own — and #worktrees-empty had none,
  // so "Loading…" sat on top of the loaded list.
  it('hides the loading card once the inventory arrives', async () => {
    openWorktrees(PROJECT);
    await flush();
    expect(el('worktrees-empty').classList.contains('hidden')).toBe(false);
    expect(el('worktrees-empty-text').textContent).toContain('Reading');

    handleWorktreesPayload(payload());
    expect(el('worktrees-empty').classList.contains('hidden')).toBe(true);
    expect(el('worktrees-section-trees').classList.contains('hidden')).toBe(
      false,
    );
  });

  // The spinner means "still working"; a settled answer must not spin.
  it('spins while loading and stops for a settled answer', async () => {
    openWorktrees(PROJECT);
    await flush();
    expect(el('worktrees-empty-spinner').classList.contains('hidden')).toBe(
      false,
    );
    handleWorktreesPayload(payload({ repo_root: '', worktrees: [] }));
    expect(el('worktrees-empty-spinner').classList.contains('hidden')).toBe(
      true,
    );
  });

  it('says so when the project is not a git repository', async () => {
    await openWith(payload({ repo_root: '', worktrees: [] }));
    expect(el('worktrees-empty').classList.contains('hidden')).toBe(false);
    expect(el('worktrees-empty-text').textContent).toContain(
      'not a git repository',
    );
    expect(el('worktrees-section-trees').classList.contains('hidden')).toBe(
      true,
    );
  });

  it('refocuses the terminal on close', () => {
    openWorktrees(PROJECT);
    closeWorktrees();
    expect(el('worktrees').classList.contains('hidden')).toBe(true);
    expect(refocusActiveTerm).toHaveBeenCalled();
  });

  it('reports a missing project instead of opening', () => {
    openWorktrees(null);
    expect(flashStatus).toHaveBeenCalledWith('no project selected', true);
    expect(el('worktrees').classList.contains('hidden')).toBe(true);
  });
});

describe('rendering', () => {
  it('lists worktrees with the main checkout first', async () => {
    await openWith(
      payload({
        worktrees: [
          { path: '/repo/.worktrees/idle', branch: 'idle' },
          {
            path: '/repo/.worktrees/busy',
            branch: 'busy',
            session_ids: ['s1'],
          },
          { path: '/repo', branch: 'main', is_main: true },
        ],
      }),
    );
    expect(rows().map((r) => r.dataset.kind)).toEqual([
      'main',
      'active',
      'idle',
    ]);
  });

  it('shows the full path as the row title', async () => {
    await openWith(
      payload({
        worktrees: [{ path: '/repo/.worktrees/feature', branch: 'feature' }],
      }),
    );
    const name = rows()[0].querySelector('.worktree-name') as HTMLElement;
    expect(name.textContent).toBe('feature');
    expect(name.title).toBe('/repo/.worktrees/feature');
  });

  it('hides the branch section when nothing is orphaned', async () => {
    await openWith(payload());
    expect(el('worktrees-section-branches').classList.contains('hidden')).toBe(
      true,
    );
  });

  it('lists orphaned branches when there are any', async () => {
    await openWith(
      payload({ orphan_branches: [{ name: 'stranded', ahead: 2 }] }),
    );
    expect(el('worktrees-section-branches').classList.contains('hidden')).toBe(
      false,
    );
    expect(branchRows()).toHaveLength(1);
    expect(branchRows()[0].textContent).toContain('stranded');
    expect(branchRows()[0].textContent).toContain('2 commits ahead');
  });

  // A reply for another project arriving while this one is open must
  // not repaint the list under the user.
  it('ignores a payload for a different project', async () => {
    await openWith(
      payload({
        worktrees: [{ path: '/repo/.worktrees/mine', branch: 'mine' }],
      }),
    );
    handleWorktreesPayload(
      payload({
        project_id: 'p2',
        worktrees: [{ path: '/other/.worktrees/theirs', branch: 'theirs' }],
      }),
    );
    expect(rows().map((r) => r.dataset.path)).toEqual([
      '/repo/.worktrees/mine',
    ]);
  });

  it('ignores a payload that arrives after the modal closed', async () => {
    await openWith(payload());
    closeWorktrees();
    handleWorktreesPayload(
      payload({
        worktrees: [{ path: '/repo/.worktrees/late', branch: 'late' }],
      }),
    );
    // Nothing was re-rendered into the closed modal.
    expect(
      rows().every((r) => r.dataset.path !== '/repo/.worktrees/late'),
    ).toBe(true);
  });
});

describe('the main checkout', () => {
  it('offers no actions at all', async () => {
    await openWith(payload());
    expect(rows()[0].querySelectorAll('button')).toHaveLength(0);
  });
});

describe('deleting', () => {
  const dirty = {
    path: '/repo/.worktrees/dirty',
    branch: 'dirty',
    uncommitted: true,
  };
  const clean = { path: '/repo/.worktrees/clean', branch: 'clean' };
  const detached = { path: '/repo/.worktrees/loose', detached: true };
  const busy = {
    path: '/repo/.worktrees/busy',
    branch: 'busy',
    session_ids: ['s1', 's2'],
  };

  const dialog = () => document.querySelector('.choice-dialog');
  const choice = (name: string): HTMLButtonElement =>
    document.querySelector(
      `.choice-dialog button[data-choice="${name}"]`,
    ) as HTMLButtonElement;

  it('disables Delete for a worktree with live sessions and says why', async () => {
    await openWith(payload({ worktrees: [busy] }));
    const btn = button(rows()[0], 'Delete');
    expect(btn.disabled).toBe(true);
    expect(btn.title).toContain('2 sessions are running');
  });

  it('never opens the dialog or calls the daemon for a blocked row', async () => {
    await openWith(payload({ worktrees: [busy] }));
    button(rows()[0], 'Delete').click();
    await flush();
    expect(dialog()).toBeNull();
    expect(RemoveWorktree).not.toHaveBeenCalled();
  });

  it('opens a dialog offering three distinct outcomes', async () => {
    await openWith(payload({ worktrees: [clean] }));
    button(rows()[0], 'Delete').click();
    await flush();
    expect(dialog()).not.toBeNull();
    expect(choice('cancel')).not.toBeNull();
    expect(choice('keep-branch')).not.toBeNull();
    expect(choice('both')).not.toBeNull();
    // Nothing is sent until one of them is pressed.
    expect(RemoveWorktree).not.toHaveBeenCalled();
  });

  // The safe option is the default: a stray Enter must not delete.
  it('focuses Cancel', async () => {
    await openWith(payload({ worktrees: [clean] }));
    button(rows()[0], 'Delete').click();
    await flush();
    expect(document.activeElement).toBe(choice('cancel'));
  });

  // Tab containment is not this modal's job any more: it lives in the
  // shared trap (test/dom/focus-trap.test.ts for the rules,
  // test/e2e/focus-traps.spec.ts for the wiring in a real focus model).
  // Asserting it here would only pin which listener happens to own it.
  it('offers no "delete both" for a worktree with no branch', async () => {
    await openWith(payload({ worktrees: [detached] }));
    button(rows()[0], 'Delete').click();
    await flush();
    expect(choice('both')).toBeNull();
    expect(choice('keep-branch')).not.toBeNull();
  });

  it('sends nothing when cancelled, and closes the dialog', async () => {
    await openWith(payload({ worktrees: [clean] }));
    button(rows()[0], 'Delete').click();
    await flush();
    choice('cancel').click();
    await flush();
    expect(RemoveWorktree).not.toHaveBeenCalled();
    expect(dialog()).toBeNull();
  });

  // Escape backs out of the deletion — it must not also close the
  // whole browser underneath.
  it('cancels on Escape without closing the browser', async () => {
    await openWith(payload({ worktrees: [clean] }));
    button(rows()[0], 'Delete').click();
    await flush();
    dialog()?.dispatchEvent(
      new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    );
    await flush();
    expect(RemoveWorktree).not.toHaveBeenCalled();
    expect(dialog()).toBeNull();
    expect(el('worktrees').classList.contains('hidden')).toBe(false);
  });

  it('keeps the branch when "Delete, keep branch" is pressed', async () => {
    await openWith(payload({ worktrees: [clean] }));
    button(rows()[0], 'Delete').click();
    await flush();
    choice('keep-branch').click();
    await flush();
    expect(RemoveWorktree).toHaveBeenCalledWith(
      'p1',
      '/repo/.worktrees/clean',
      false,
      false,
      false,
    );
  });

  it('deletes the branch when "Delete both" is pressed', async () => {
    await openWith(payload({ worktrees: [clean] }));
    button(rows()[0], 'Delete').click();
    await flush();
    choice('both').click();
    await flush();
    expect(RemoveWorktree).toHaveBeenCalledWith(
      'p1',
      '/repo/.worktrees/clean',
      false,
      true,
      false,
    );
  });

  it('sends force=true only when something blocks', async () => {
    await openWith(payload({ worktrees: [dirty] }));
    button(rows()[0], 'Delete').click();
    await flush();
    choice('keep-branch').click();
    await flush();
    expect(RemoveWorktree).toHaveBeenCalledWith(
      'p1',
      '/repo/.worktrees/dirty',
      true,
      false,
      false,
    );
  });

  // The closing line has to stay honest: the branch preserves the
  // commits and nothing preserves uncommitted changes, so promising
  // recoverability flatly would contradict the blocker listed above it
  // — while "Delete, keep branch" is the non-danger button.
  it('does not promise recoverability when uncommitted work will die', async () => {
    await openWith(payload({ worktrees: [dirty] }));
    button(rows()[0], 'Delete').click();
    await flush();
    const text = dialog()?.textContent ?? '';
    expect(text).toContain('destroyed either way');
    expect(text).not.toContain('leaves its commits recoverable');
  });

  it('still promises recoverability for a clean worktree', async () => {
    await openWith(payload({ worktrees: [clean] }));
    button(rows()[0], 'Delete').click();
    await flush();
    expect(dialog()?.textContent ?? '').toContain('commits recoverable');
  });

  it('names what would be lost, in the dialog itself', async () => {
    await openWith(payload({ worktrees: [{ ...dirty, unpushed: 2 }] }));
    button(rows()[0], 'Delete').click();
    await flush();
    const text = dialog()?.textContent ?? '';
    expect(text).toContain('/repo/.worktrees/dirty');
    expect(text).toContain('uncommitted changes');
    expect(text).toContain('2 commits that are not pushed');
    expect(text).toContain('lose work');
  });

  it('does nothing if the browser closed while the dialog was open', async () => {
    await openWith(payload({ worktrees: [clean] }));
    button(rows()[0], 'Delete').click();
    await flush();
    closeWorktrees();
    choice('both')?.click();
    await flush();
    expect(RemoveWorktree).not.toHaveBeenCalled();
  });
});

describe('renaming', () => {
  const clean = { path: '/repo/.worktrees/clean', branch: 'clean' };
  const busy = {
    path: '/repo/.worktrees/busy',
    branch: 'busy',
    session_ids: ['s1'],
  };

  it('is disabled while a session is inside, because the directory moves', async () => {
    await openWith(payload({ worktrees: [busy] }));
    const btn = button(rows()[0], 'Rename');
    expect(btn.disabled).toBe(true);
    expect(btn.title).toContain('Close the sessions');
  });

  it('commits the new branch name on Enter', async () => {
    await openWith(payload({ worktrees: [clean] }));
    button(rows()[0], 'Rename').click();
    const input = rows()[0].querySelector('input') as HTMLInputElement;
    expect(input.value).toBe('clean');
    input.value = 'renamed';
    input.dispatchEvent(
      new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
    );
    await flush();
    expect(RenameWorktree).toHaveBeenCalledWith(
      'p1',
      '/repo/.worktrees/clean',
      'renamed',
    );
  });

  it('sends nothing on Escape', async () => {
    await openWith(payload({ worktrees: [clean] }));
    button(rows()[0], 'Rename').click();
    const input = rows()[0].querySelector('input') as HTMLInputElement;
    input.value = 'discarded';
    input.dispatchEvent(
      new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    );
    await flush();
    expect(RenameWorktree).not.toHaveBeenCalled();
    // ...and the row is back to normal.
    expect(rows()[0].querySelector('input')).toBeNull();
  });

  it('sends nothing when the name is unchanged', async () => {
    await openWith(payload({ worktrees: [clean] }));
    button(rows()[0], 'Rename').click();
    const input = rows()[0].querySelector('input') as HTMLInputElement;
    input.dispatchEvent(
      new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
    );
    await flush();
    expect(RenameWorktree).not.toHaveBeenCalled();
  });
});

describe('resuming work', () => {
  it('hands the worktree path to the session launcher and closes', async () => {
    await openWith(
      payload({
        worktrees: [{ path: '/repo/.worktrees/resume', branch: 'resume' }],
      }),
    );
    button(rows()[0], 'New session').click();
    expect(openSessionIn).toHaveBeenCalledWith(
      'p1',
      '/repo/.worktrees/resume',
      false,
    );
    expect(el('worktrees').classList.contains('hidden')).toBe(true);
  });
});

describe('continuing previous work', () => {
  // "New session" and "Continue" differ only in whether the agent is
  // asked to resume its last conversation in that directory — the
  // worktree is the same either way.
  it('offers both a fresh session and a continue', async () => {
    await openWith(
      payload({
        worktrees: [{ path: '/repo/.worktrees/resume', branch: 'resume' }],
      }),
    );
    expect(button(rows()[0], 'New session')).toBeTruthy();
    expect(button(rows()[0], 'Continue')).toBeTruthy();
  });

  it('asks the launcher to resume when Continue is used', async () => {
    await openWith(
      payload({
        worktrees: [{ path: '/repo/.worktrees/resume', branch: 'resume' }],
      }),
    );
    button(rows()[0], 'Continue').click();
    expect(openSessionIn).toHaveBeenCalledWith(
      'p1',
      '/repo/.worktrees/resume',
      true,
    );
  });

  it('offers neither on the main checkout', async () => {
    await openWith(payload());
    expect(rows()[0].querySelectorAll('button')).toHaveLength(0);
  });
});

describe('orphaned branches', () => {
  const dialog = () => document.querySelector('.choice-dialog');
  const choice = (name: string): HTMLButtonElement =>
    document.querySelector(
      `.choice-dialog button[data-choice="${name}"]`,
    ) as HTMLButtonElement;

  it('materializes a worktree for the branch', async () => {
    await openWith(payload({ orphan_branches: [{ name: 'stranded' }] }));
    button(branchRows()[0], 'Create worktree').click();
    await flush();
    expect(CreateWorktree).toHaveBeenCalledWith('p1', 'stranded');
  });

  it('offers a Delete button', async () => {
    await openWith(payload({ orphan_branches: [{ name: 'stranded' }] }));
    expect(button(branchRows()[0], 'Delete')).toBeTruthy();
  });

  it('confirms before deleting, and sends nothing on cancel', async () => {
    await openWith(payload({ orphan_branches: [{ name: 'stranded' }] }));
    button(branchRows()[0], 'Delete').click();
    await flush();
    expect(dialog()).not.toBeNull();
    choice('cancel').click();
    await flush();
    expect(DeleteBranch).not.toHaveBeenCalled();
  });

  // A merged branch loses nothing, so no force is needed.
  it('deletes a merged branch without force', async () => {
    await openWith(
      payload({ orphan_branches: [{ name: 'tidy', merged: true }] }),
    );
    button(branchRows()[0], 'Delete').click();
    await flush();
    expect(dialog()?.textContent).toContain('nothing is lost');
    choice('local').click();
    await flush();
    expect(DeleteBranch).toHaveBeenCalledWith('p1', 'tidy', false, false);
  });

  // An unmerged branch is the case that loses commits: git refuses it
  // outright, so the override has to be explicit — and the dialog has
  // to say what goes.
  it('warns and forces for an unmerged branch', async () => {
    await openWith(payload({ orphan_branches: [{ name: 'wip', ahead: 3 }] }));
    button(branchRows()[0], 'Delete').click();
    await flush();
    const text = dialog()?.textContent ?? '';
    expect(text).toContain('lose its commits');
    expect(text).toContain('3 commits that are not merged');
    choice('local').click();
    await flush();
    expect(DeleteBranch).toHaveBeenCalledWith('p1', 'wip', true, false);
  });
});

describe('keyboard', () => {
  it('closes on Escape', async () => {
    await openWith(payload());
    el('worktrees').dispatchEvent(
      new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    );
    expect(el('worktrees').classList.contains('hidden')).toBe(true);
  });

  // The tip's commit subject is what tells the user what is in a
  // branch; the name alone does not.
  it('shows the tip commit subject on both lists', async () => {
    await openWith(
      payload({
        worktrees: [
          { path: '/repo/.worktrees/c', branch: 'c', subject: 'feat: a thing' },
        ],
        orphan_branches: [{ name: 'o', merged: true, subject: 'fix: a bug' }],
      }),
    );
    expect(rows()[0].querySelector('.worktree-subject')?.textContent).toBe(
      'feat: a thing',
    );
    expect(
      branchRows()[0].querySelector('.worktree-subject')?.textContent,
    ).toBe('fix: a bug');
  });

  // An older daemon sends no subject at all — the line is skipped, not
  // rendered empty.
  it('omits the subject line when the payload has none', async () => {
    await openWith(
      payload({ worktrees: [{ path: '/repo/.worktrees/c', branch: 'c' }] }),
    );
    expect(rows()[0].querySelector('.worktree-subject')).toBeNull();
  });

  it('refreshes on (r)', async () => {
    await openWith(payload());
    ListWorktrees.mockClear();
    el('worktrees').dispatchEvent(
      new window.KeyboardEvent('keydown', { key: 'r', bubbles: true }),
    );
    expect(ListWorktrees).toHaveBeenCalledWith('p1');
  });

  // (r) must not eat a character the user is typing into the rename box.
  it('does not refresh while typing in the rename input', async () => {
    await openWith(
      payload({ worktrees: [{ path: '/repo/.worktrees/c', branch: 'c' }] }),
    );
    button(rows()[0], 'Rename').click();
    ListWorktrees.mockClear();
    const input = rows()[0].querySelector('input') as HTMLInputElement;
    input.dispatchEvent(
      new window.KeyboardEvent('keydown', { key: 'r', bubbles: true }),
    );
    expect(ListWorktrees).not.toHaveBeenCalled();
  });
});
