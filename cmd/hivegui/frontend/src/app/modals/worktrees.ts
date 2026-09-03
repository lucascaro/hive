// ---------- worktree browser: the non-React half ----------
//
// Per-project view of git worktrees and the branches that have none.
// The panel renders from components/modals/Worktrees.tsx (Phase 4); all
// row logic (classification, delete blockers, sorting, confirm copy)
// lives in ../../lib/worktrees.ts.
//
// What stays here is the open/close pair its callers import (keyboard.ts,
// the sidebar, the session tiles, main.tsx), the daemon round-trip, and
// the "worktree:list" event sink — the daemon answers every mutation
// with a fresh inventory, so nothing patches: the store field is
// replaced and the component re-renders from it.

import { flushSync } from 'react-dom';
import {
  CreateWorktree,
  DeleteBranch,
  ListWorktrees,
  RemoveWorktree,
  RenameWorktree,
} from '../../bridge.js';
import { flashStatus, reportFailure } from '../dom.js';
import {
  dismissChoiceDialog,
  openChoiceDialog,
  type Choice,
} from './choice-dialog.js';
import {
  closeModal,
  isModalOpen,
  modalEntry,
  openModal,
  setWorktreesPayload,
} from '../../store/store.js';
import { pageEl } from '../el.js';
import { releaseFocus } from '../../lib/focus-trap.js';
import {
  deleteBlockers,
  needsConfirm,
  readProjectIdOf,
  type BranchInfo,
  type WorktreeInfo,
  type WorktreesPayload,
} from '../../lib/worktrees.js';
import type { ProjectInfo } from '../state.js';

// Narrow on purpose, matching project-editor.ts: this modal needs the
// focus callbacks and one way to open a session in a chosen worktree.
export interface WorktreesDeps {
  setFocusedTile: (id: string | null) => void;
  refocusActiveTerm: () => void;
  // openSessionIn launches the agent picker for an existing worktree.
  openSessionIn: (
    projectId: string,
    worktreePath: string,
    continueConversation: boolean,
  ) => void;
}

let deps: WorktreesDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
  openSessionIn: () => {},
};

// openSessionIn is the component's one dependency on main.tsx's wiring;
// it reads it through here rather than taking a prop, so the component
// mounts with the same shape as the other modals.
export function openSessionIn(
  projectId: string,
  worktreePath: string,
  continueConversation: boolean,
): void {
  deps.openSessionIn(projectId, worktreePath, continueConversation);
}

// worktreesProjectId is the project the open browser is showing, or ''.
// The destructive flows re-read it after their dialog resolves — the
// browser can have been closed, or moved to another project, while the
// question was up.
export function worktreesProjectId(): string {
  return modalEntry('worktrees')?.projectId ?? '';
}

export function openWorktrees(project: ProjectInfo | null): void {
  if (!project) {
    flashStatus('no project selected', true);
    return;
  }
  // Clear before the open, not after: the previous project's inventory
  // must never paint under the new title, and the component reads the
  // null as its loading state.
  setWorktreesPayload(null);
  openModal({
    id: 'worktrees',
    projectId: project.id,
    projectName: project.name ?? '',
  });
  deps.setFocusedTile(null);
  refresh();
}

export function closeWorktrees(): void {
  if (!isModalOpen('worktrees')) return;
  // A question about a row in this panel outlives the panel otherwise.
  dismissChoiceDialog();
  // Drop focus before the unmount, or it is left on a removed element
  // and the browser resolves it to <body> — stranding the keyboard.
  releaseFocus(pageEl('worktrees'));
  // flushSync: called from plain listeners (keyboard.ts's window
  // handler, a row's "New session" button), and refocusActiveTerm() —
  // or the launcher this hands off to — must not run while the panel is
  // still on screen.
  flushSync(() => closeModal('worktrees'));
  setWorktreesPayload(null);
  deps.refocusActiveTerm();
}

export function refresh(): void {
  const projectId = worktreesProjectId();
  if (!projectId) return;
  ListWorktrees(projectId).catch(reportFailure('list worktrees'));
}

// handleWorktreesPayload is the "worktree:list" event sink, wired in
// events.ts. Payloads for other projects are ignored: the daemon
// answers per-project and a stale reply must not repaint the list the
// user is looking at.
export function handleWorktreesPayload(payload: WorktreesPayload): void {
  const projectId = worktreesProjectId();
  if (!projectId) return;
  if (readProjectIdOf(payload) !== projectId) return;
  // The row a dialog was asking about may not survive this repaint.
  dismissChoiceDialog();
  setWorktreesPayload(payload);
}

export function initWorktrees(injected: WorktreesDeps): void {
  deps = injected;
}

// ---------- mutations ----------
//
// The destructive pair asks first, with a choice dialog rather than a
// Confirm: the real question has three answers and the consequence needs
// spelling out. Both re-check that the browser is still open on the same
// project after the await — the panel can have been closed, or moved to
// another project, while the question was up.

export function createWorktreeFor(branch: string): void {
  CreateWorktree(worktreesProjectId(), branch).catch(
    reportFailure('create worktree'),
  );
}

export function renameWorktreeTo(path: string, next: string): void {
  RenameWorktree(worktreesProjectId(), path, next).catch(
    reportFailure('rename worktree'),
  );
}

// What the user chose in the delete dialog. 'both' takes the branch
// with the directory; 'everywhere' takes the remote branch too.
type DeleteChoice = 'everywhere' | 'both' | 'keep-branch' | 'cancel';

// askDelete puts the three real outcomes on screen as buttons. A native
// Confirm can only answer yes/no, which forced the branch question and
// the deletion question into two sequential prompts — and left no way
// to back out of the second without having already answered the first.
async function askDelete(w: WorktreeInfo): Promise<DeleteChoice> {
  const choices: Choice[] = [
    { label: 'Cancel', value: 'cancel' },
    { label: 'Delete, keep branch', value: 'keep-branch' },
  ];
  if (w.branch) {
    // Not danger-styled when the branch is already merged and nothing
    // else is at stake — there is no work left to lose.
    const danger = needsConfirm(w) || !w.merged;
    choices.push({ label: 'Delete + local branch', value: 'both', danger });
    // Only offered when there is a remote branch to delete: a push is
    // the one step of this that reaches beyond the machine.
    if (w.upstream) {
      choices.push({
        label: 'Delete + branch everywhere',
        value: 'everywhere',
        danger,
      });
    }
  }
  const answer = await openChoiceDialog({
    title: needsConfirm(w)
      ? 'Delete this worktree and lose work?'
      : 'Delete this worktree?',
    detail: w.path,
    bullets: deleteBlockers(w).map((b) => b.reason),
    // Careful with this wording: keeping the branch preserves the
    // COMMITS, and says nothing about uncommitted changes, which the
    // directory removal destroys either way. Promising recoverability
    // flatly would argue against the blocker listed right above it.
    note: noteFor(w),
    choices,
  });
  return answer as DeleteChoice;
}

// noteFor is the dialog's closing line. It has to stay honest about
// which half of the work is recoverable: the branch keeps the commits,
// nothing keeps uncommitted changes.
function noteFor(w: WorktreeInfo): string {
  if (!w.branch) {
    return 'This worktree has no branch. Deleting it cannot be undone.';
  }
  const remote = w.upstream
    ? ` “Everywhere” also deletes ${w.upstream} on the remote.`
    : '';
  if (w.uncommitted) {
    return (
      `Its uncommitted changes are destroyed either way — nothing keeps those. ` +
      `Keeping the branch “${w.branch}” preserves only the commits already made.${remote}`
    );
  }
  if (w.merged) {
    return `The branch “${w.branch}” is already merged into the default branch, so deleting it loses nothing.${remote}`;
  }
  return `Keeping the branch “${w.branch}” leaves its commits recoverable. Deleting it cannot be undone.${remote}`;
}

// confirmAndDelete is the destructive path. The blockers are computed
// locally so the dialog can name exactly what is at stake, and force is
// sent only when something actually blocks — the daemon re-checks
// regardless and refuses on the wire if this view is stale.
export async function confirmAndDelete(w: WorktreeInfo): Promise<void> {
  const choice = await askDelete(w);
  if (choice === 'cancel') return;
  const projectId = worktreesProjectId();
  if (!projectId) return;

  RemoveWorktree(
    projectId,
    w.path,
    needsConfirm(w),
    choice === 'both' || choice === 'everywhere',
    choice === 'everywhere',
  ).catch(reportFailure('delete worktree'));
}

// 'remote' deletes the branch on its remote as well as locally.
type BranchDeleteChoice = 'remote' | 'local' | 'cancel';

// askDeleteBranch confirms removing an orphaned branch — the last
// handle on whatever commits only it has.
async function askDeleteBranch(b: BranchInfo): Promise<BranchDeleteChoice> {
  const bullets: string[] = [];
  if (!b.merged) {
    const ahead = b.ahead ?? 0;
    bullets.push(
      ahead === 1
        ? 'It has 1 commit that is not merged anywhere else.'
        : ahead > 1
          ? `It has ${ahead} commits that are not merged anywhere else.`
          : 'It is not merged into the default branch.',
    );
  }
  const answer = await openChoiceDialog({
    title: b.merged
      ? `Delete the branch “${b.name}”?`
      : `Delete “${b.name}” and lose its commits?`,
    detail: b.upstream ? `tracks ${b.upstream}` : 'no upstream',
    bullets,
    note: b.merged
      ? 'Its commits are already merged, so nothing is lost.'
      : 'Deleting an unmerged branch discards those commits. This cannot be undone.',
    choices: b.upstream
      ? [
          { label: 'Cancel', value: 'cancel' },
          { label: 'Delete local only', value: 'local', danger: !b.merged },
          {
            label: 'Delete local + remote',
            value: 'remote',
            danger: !b.merged,
          },
        ]
      : [
          { label: 'Cancel', value: 'cancel' },
          { label: 'Delete branch', value: 'local', danger: !b.merged },
        ],
  });
  // Narrowed rather than cast: this answer decides whether a remote
  // branch is deleted, so anything unrecognised must read as cancel.
  return answer === 'local' || answer === 'remote' ? answer : 'cancel';
}

// confirmAndDeleteBranch removes an orphaned branch. force is sent only
// for an unmerged branch, which git otherwise refuses outright; the
// daemon re-checks either way and answers with branch_unmerged if this
// view is stale.
export async function confirmAndDeleteBranch(b: BranchInfo): Promise<void> {
  const choice = await askDeleteBranch(b);
  if (choice === 'cancel') return;
  const projectId = worktreesProjectId();
  if (!projectId) return;
  DeleteBranch(projectId, b.name, !b.merged, choice === 'remote').catch(
    reportFailure('delete branch'),
  );
}
