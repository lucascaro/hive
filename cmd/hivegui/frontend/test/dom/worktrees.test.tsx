// @vitest-environment jsdom
//
// Covers the worktree browser (src/app/modals/worktrees.ts, rendered by
// src/components/modals/Worktrees.tsx) and the choice dialog it drives
// for its destructive confirmations (src/app/modals/choice-dialog.ts,
// rendered by src/components/modals/ChoiceDialog.tsx). The row-
// classification logic is unit-tested in test/unit/worktrees.test.ts;
// what this file pins down is the part that can destroy work:
//
//   • a row that cannot be deleted is DISABLED, with the reason visible
//   • the three-way delete dialog gates the destructive call, and
//     cancelling it sends nothing
//   • force is sent only when something actually blocks
//   • delete_branch follows the button the user pressed
//   • a reply for a different project never repaints the open list
//
// React port (Phase 4): both #worktrees and #choice-dialog are now
// static roots (index.html) that mount their own islands, so this file
// renders both with RTL rather than driving one imperative module.
// openWorktrees/closeWorktrees/handleWorktreesPayload still write a
// store field rather than painting synchronously, so every call that
// touches it runs inside act(). The inline rename is unchanged: it is
// still the imperative helper in app/inline-rename.ts, mounted into an
// empty .worktree-main — the one behavioral change from the port is that
// a daemon repaint mid-rename no longer clobbers the input (the React
// row leaves an empty div for the imperative editor rather than
// rebuilding it), but no existing case exercised that clobbering, so
// there is nothing to re-assert here.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { Mock } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';
import { resetStore } from '../../src/store/store.js';
import { inlineRenameActive } from '../../src/app/inline-rename.js';

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

// The two static roots index.html declares, copied verbatim — both
// islands mount here. Nested under #app: RTL's cleanup() removes a
// render() container whose parentNode IS document.body, and each root
// is itself the render() container below.
const MARKUP = `
  <div id="app">
    <div id="worktrees" class="hv-dialog hidden" role="dialog"
      aria-modal="true" aria-labelledby="worktrees-title"></div>
    <div id="choice-dialog" class="hv-dialog hidden choice-dialog" role="alertdialog"
      aria-modal="true" aria-labelledby="choice-dialog-title"></div>
  </div>`;

type WorktreesModule = typeof import('../../src/app/modals/worktrees.js');
let openWorktrees: WorktreesModule['openWorktrees'];
let closeWorktrees: WorktreesModule['closeWorktrees'];
let initWorktrees: WorktreesModule['initWorktrees'];
let handleWorktreesPayload: WorktreesModule['handleWorktreesPayload'];
let refocusActiveTerm: Mock<() => void>;
let setFocusedTile: Mock<(id: string | null) => void>;
let openSessionIn: Mock<(projectId: string, worktreePath: string) => void>;
// Imported after the markup exists, same reason as settings.test.tsx /
// launcher.test.tsx: the modules they pull in resolve DOM singletons at
// load time.
let Worktrees: typeof import('../../src/components/modals/Worktrees.js')['Worktrees'];
let ChoiceDialog: typeof import('../../src/components/modals/ChoiceDialog.js')['ChoiceDialog'];

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  ({ openWorktrees, closeWorktrees, initWorktrees, handleWorktreesPayload } =
    await import('../../src/app/modals/worktrees.js'));
  ({ Worktrees } = await import('../../src/components/modals/Worktrees.js'));
  ({ ChoiceDialog } = await import(
    '../../src/components/modals/ChoiceDialog.js'
  ));
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
  resetStore();
  render(<Worktrees root={el('worktrees')} />, { container: el('worktrees') });
  render(<ChoiceDialog root={el('choice-dialog')} />, {
    container: el('choice-dialog'),
  });
});

function el<T extends HTMLElement = HTMLElement>(id: string): T {
  return document.getElementById(id) as T;
}
// The panel unmounts entirely when the modal is closed (unlike the old
// static markup, where #worktrees-list stayed in the DOM, just hidden),
// so a closed browser has no list element at all — fall back to empty
// rather than throwing, which is what "nothing rendered" means here.
const rows = () => [
  ...(document
    .getElementById('worktrees-list')
    ?.querySelectorAll<HTMLElement>('.worktree-row') ?? []),
];
const branchRows = () => [
  ...(document
    .getElementById('worktrees-branches')
    ?.querySelectorAll<HTMLElement>('.worktree-row') ?? []),
];
// Buttons are identified by their label, which is also what the user
// clicks — a renamed button should fail these tests.
const button = (row: Element, label: string): HTMLButtonElement => {
  const found = [...row.querySelectorAll('button')].find(
    (b) => b.textContent === label,
  );
  return found as HTMLButtonElement;
};
// Lets in-flight promises (openChoiceDialog's, the bridge mocks') settle
// AND React re-render before the assertions: both the mutation flows and
// handleWorktreesPayload write store state from outside a React event
// handler.
const flush = () =>
  act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });

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
  act(() => {
    openWorktrees(PROJECT);
  });
  await flush();
  act(() => {
    handleWorktreesPayload(p);
  });
}

// The choice dialog's static root, and whether the question currently on
// screen is this one — the root itself always exists now (index.html),
// so "no dialog" means hidden, not absent, unlike the old dynamically
// built element.
function dialogRoot(): HTMLElement {
  return el('choice-dialog');
}
function dialog(): HTMLElement | null {
  const root = dialogRoot();
  return root.classList.contains('hidden') ? null : root;
}
const choice = (name: string): HTMLButtonElement =>
  document.querySelector(
    `.choice-dialog button[data-choice="${name}"]`,
  ) as HTMLButtonElement;

describe('opening', () => {
  it('requests the inventory for the project it was opened on', async () => {
    act(() => {
      openWorktrees(PROJECT);
    });
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
    act(() => {
      openWorktrees(PROJECT);
    });
    await flush();
    expect(el('worktrees-empty').classList.contains('hidden')).toBe(false);
    expect(el('worktrees-empty-text').textContent).toContain('Reading');

    act(() => {
      handleWorktreesPayload(payload());
    });
    expect(el('worktrees-empty').classList.contains('hidden')).toBe(true);
    expect(el('worktrees-section-trees').classList.contains('hidden')).toBe(
      false,
    );
  });

  // The spinner means "still working"; a settled answer must not spin.
  it('spins while loading and stops for a settled answer', async () => {
    act(() => {
      openWorktrees(PROJECT);
    });
    await flush();
    expect(el('worktrees-empty-spinner').classList.contains('hidden')).toBe(
      false,
    );
    act(() => {
      handleWorktreesPayload(payload({ repo_root: '', worktrees: [] }));
    });
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
    act(() => {
      openWorktrees(PROJECT);
    });
    act(() => {
      closeWorktrees();
    });
    expect(el('worktrees').classList.contains('hidden')).toBe(true);
    expect(refocusActiveTerm).toHaveBeenCalled();
  });

  it('reports a missing project instead of opening', () => {
    act(() => {
      openWorktrees(null);
    });
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
    act(() => {
      handleWorktreesPayload(
        payload({
          project_id: 'p2',
          worktrees: [{ path: '/other/.worktrees/theirs', branch: 'theirs' }],
        }),
      );
    });
    expect(rows().map((r) => r.dataset.path)).toEqual([
      '/repo/.worktrees/mine',
    ]);
  });

  it('ignores a payload that arrives after the modal closed', async () => {
    await openWith(payload());
    act(() => {
      closeWorktrees();
    });
    act(() => {
      handleWorktreesPayload(
        payload({
          worktrees: [{ path: '/repo/.worktrees/late', branch: 'late' }],
        }),
      );
    });
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

  it('disables Delete for a worktree with live sessions and says why', async () => {
    await openWith(payload({ worktrees: [busy] }));
    const btn = button(rows()[0], 'Delete');
    expect(btn.disabled).toBe(true);
    expect(btn.title).toContain('2 sessions are running');
  });

  it('never opens the dialog or calls the daemon for a blocked row', async () => {
    await openWith(payload({ worktrees: [busy] }));
    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    expect(dialog()).toBeNull();
    expect(RemoveWorktree).not.toHaveBeenCalled();
  });

  it('opens a dialog offering three distinct outcomes', async () => {
    await openWith(payload({ worktrees: [clean] }));
    fireEvent.click(button(rows()[0], 'Delete'));
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
    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    expect(document.activeElement).toBe(choice('cancel'));
  });

  // Tab containment is not this modal's job any more: it lives in the
  // shared trap (test/dom/focus-trap.test.ts for the rules,
  // test/e2e/focus-traps.spec.ts for the wiring in a real focus model).
  // Asserting it here would only pin which listener happens to own it.
  it('offers no "delete both" for a worktree with no branch', async () => {
    await openWith(payload({ worktrees: [detached] }));
    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    expect(choice('both')).toBeNull();
    expect(choice('keep-branch')).not.toBeNull();
  });

  it('sends nothing when cancelled, and closes the dialog', async () => {
    await openWith(payload({ worktrees: [clean] }));
    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    fireEvent.click(choice('cancel'));
    await flush();
    expect(RemoveWorktree).not.toHaveBeenCalled();
    expect(dialog()).toBeNull();
  });

  // Escape backs out of the deletion — it must not also close the
  // whole browser underneath.
  it('cancels on Escape without closing the browser', async () => {
    await openWith(payload({ worktrees: [clean] }));
    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    act(() => {
      dialog()?.dispatchEvent(
        new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      );
    });
    await flush();
    expect(RemoveWorktree).not.toHaveBeenCalled();
    expect(dialog()).toBeNull();
    expect(el('worktrees').classList.contains('hidden')).toBe(false);
  });

  it('keeps the branch when "Delete, keep branch" is pressed', async () => {
    await openWith(payload({ worktrees: [clean] }));
    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    fireEvent.click(choice('keep-branch'));
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
    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    fireEvent.click(choice('both'));
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
    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    fireEvent.click(choice('keep-branch'));
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
    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    const text = dialog()?.textContent ?? '';
    expect(text).toContain('destroyed either way');
    expect(text).not.toContain('leaves its commits recoverable');
  });

  it('still promises recoverability for a clean worktree', async () => {
    await openWith(payload({ worktrees: [clean] }));
    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    expect(dialog()?.textContent ?? '').toContain('commits recoverable');
  });

  it('names what would be lost, in the dialog itself', async () => {
    await openWith(payload({ worktrees: [{ ...dirty, unpushed: 2 }] }));
    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    const text = dialog()?.textContent ?? '';
    expect(text).toContain('/repo/.worktrees/dirty');
    expect(text).toContain('uncommitted changes');
    expect(text).toContain('2 commits that are not pushed');
    expect(text).toContain('lose work');
  });

  it('does nothing if the browser closed while the dialog was open', async () => {
    await openWith(payload({ worktrees: [clean] }));
    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    act(() => {
      closeWorktrees();
    });
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
    fireEvent.click(button(rows()[0], 'Rename'));
    const input = rows()[0].querySelector('input') as HTMLInputElement;
    expect(input.value).toBe('clean');
    input.value = 'renamed';
    act(() => {
      input.dispatchEvent(
        new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
      );
    });
    await flush();
    expect(RenameWorktree).toHaveBeenCalledWith(
      'p1',
      '/repo/.worktrees/clean',
      'renamed',
    );
  });

  // The whole reason the input mounts into an EMPTY .worktree-main: the
  // daemon repaints this list on every mutation anywhere in the project,
  // and the imperative version rebuilt the row under the edit. A second
  // beginInlineRename would be just as bad — two inputs in one row, the
  // second seeded from the value the user is editing away from.
  it('survives a repaint mid-edit, exactly once', async () => {
    await openWith(payload({ worktrees: [clean] }));
    fireEvent.click(button(rows()[0], 'Rename'));
    const input = rows()[0].querySelector(
      'input.worktree-rename',
    ) as HTMLInputElement;
    input.value = 'half-typed';

    act(() => {
      handleWorktreesPayload(payload({ worktrees: [clean] }));
    });
    await flush();

    const inputs = document.querySelectorAll('input.worktree-rename');
    expect(inputs.length).toBe(1);
    expect((inputs[0] as HTMLInputElement).value).toBe('half-typed');
  });

  // beginInlineRename registers the edit module-side and only
  // Enter/Escape/blur clear it. React removing a focused input fires no
  // blur, so a row (or panel) that goes away mid-edit would leave
  // inlineRenameActive() true — and that is keyboard.ts's FIRST ladder
  // branch, which then swallows every keystroke in the app.
  it('does not strand the keyboard when the panel closes mid-edit', async () => {
    await openWith(payload({ worktrees: [clean] }));
    fireEvent.click(button(rows()[0], 'Rename'));
    expect(inlineRenameActive()).toBe(true);

    act(() => {
      closeWorktrees();
    });
    await flush();

    expect(inlineRenameActive()).toBe(false);
  });

  it('does not strand the keyboard when the row itself goes away', async () => {
    await openWith(payload({ worktrees: [clean] }));
    fireEvent.click(button(rows()[0], 'Rename'));
    expect(inlineRenameActive()).toBe(true);

    // The daemon dropped the worktree while its branch was being renamed.
    act(() => {
      handleWorktreesPayload(payload({ worktrees: [] }));
    });
    await flush();

    expect(inlineRenameActive()).toBe(false);
  });

  it('sends nothing on Escape', async () => {
    await openWith(payload({ worktrees: [clean] }));
    fireEvent.click(button(rows()[0], 'Rename'));
    const input = rows()[0].querySelector('input') as HTMLInputElement;
    input.value = 'discarded';
    act(() => {
      input.dispatchEvent(
        new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      );
    });
    await flush();
    expect(RenameWorktree).not.toHaveBeenCalled();
    // ...and the row is back to normal.
    expect(rows()[0].querySelector('input')).toBeNull();
  });

  it('sends nothing when the name is unchanged', async () => {
    await openWith(payload({ worktrees: [clean] }));
    fireEvent.click(button(rows()[0], 'Rename'));
    const input = rows()[0].querySelector('input') as HTMLInputElement;
    act(() => {
      input.dispatchEvent(
        new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
      );
    });
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
    fireEvent.click(button(rows()[0], 'New session'));
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
    fireEvent.click(button(rows()[0], 'Continue'));
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
  it('materializes a worktree for the branch', async () => {
    await openWith(payload({ orphan_branches: [{ name: 'stranded' }] }));
    fireEvent.click(button(branchRows()[0], 'Create worktree'));
    await flush();
    expect(CreateWorktree).toHaveBeenCalledWith('p1', 'stranded');
  });

  it('offers a Delete button', async () => {
    await openWith(payload({ orphan_branches: [{ name: 'stranded' }] }));
    expect(button(branchRows()[0], 'Delete')).toBeTruthy();
  });

  it('confirms before deleting, and sends nothing on cancel', async () => {
    await openWith(payload({ orphan_branches: [{ name: 'stranded' }] }));
    fireEvent.click(button(branchRows()[0], 'Delete'));
    await flush();
    expect(dialog()).not.toBeNull();
    fireEvent.click(choice('cancel'));
    await flush();
    expect(DeleteBranch).not.toHaveBeenCalled();
  });

  // A merged branch loses nothing, so no force is needed.
  it('deletes a merged branch without force', async () => {
    await openWith(
      payload({ orphan_branches: [{ name: 'tidy', merged: true }] }),
    );
    fireEvent.click(button(branchRows()[0], 'Delete'));
    await flush();
    expect(dialog()?.textContent).toContain('nothing is lost');
    fireEvent.click(choice('local'));
    await flush();
    expect(DeleteBranch).toHaveBeenCalledWith('p1', 'tidy', false, false);
  });

  // An unmerged branch is the case that loses commits: git refuses it
  // outright, so the override has to be explicit — and the dialog has
  // to say what goes.
  it('warns and forces for an unmerged branch', async () => {
    await openWith(payload({ orphan_branches: [{ name: 'wip', ahead: 3 }] }));
    fireEvent.click(button(branchRows()[0], 'Delete'));
    await flush();
    const text = dialog()?.textContent ?? '';
    expect(text).toContain('lose its commits');
    expect(text).toContain('3 commits that are not merged');
    fireEvent.click(choice('local'));
    await flush();
    expect(DeleteBranch).toHaveBeenCalledWith('p1', 'wip', true, false);
  });
});

describe('keyboard', () => {
  it('closes on Escape', async () => {
    await openWith(payload());
    act(() => {
      el('worktrees').dispatchEvent(
        new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      );
    });
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
    act(() => {
      el('worktrees').dispatchEvent(
        new window.KeyboardEvent('keydown', { key: 'r', bubbles: true }),
      );
    });
    expect(ListWorktrees).toHaveBeenCalledWith('p1');
  });

  // (r) must not eat a character the user is typing into the rename box.
  it('does not refresh while typing in the rename input', async () => {
    await openWith(
      payload({ worktrees: [{ path: '/repo/.worktrees/c', branch: 'c' }] }),
    );
    fireEvent.click(button(rows()[0], 'Rename'));
    ListWorktrees.mockClear();
    const input = rows()[0].querySelector('input') as HTMLInputElement;
    act(() => {
      input.dispatchEvent(
        new window.KeyboardEvent('keydown', { key: 'r', bubbles: true }),
      );
    });
    expect(ListWorktrees).not.toHaveBeenCalled();
  });
});
