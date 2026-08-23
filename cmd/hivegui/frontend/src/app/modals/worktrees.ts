// ---------- worktree browser ----------
//
// Per-project view of git worktrees and the branches that have none.
// Lets the user resume work in an existing worktree, materialize a
// worktree for an orphaned branch, rename one, and delete the ones
// they're done with.
//
// All row logic (classification, delete blockers, sorting, confirm
// copy) lives in ../../lib/worktrees.ts so it is unit-testable; this
// module is the DOM and the daemon round-trip.
//
// The daemon answers every mutation with a fresh inventory
// (wire.FrameWorktrees → the "worktree:list" event), so this module
// never patches its own state — it re-renders from what comes back.

import {
  ListWorktrees,
  RemoveWorktree,
  CreateWorktree,
  RenameWorktree,
  DeleteBranch,
} from '../../bridge.js';
import { flashStatus, reportFailure } from '../dom.js';
import { registerModal } from './registry.js';
import {
  openChoiceDialog,
  dismissChoiceDialog,
  type Choice,
} from './choice-dialog.js';
import { pageEl } from '../el.js';
import { beginInlineRename } from '../inline-rename.js';
import { releaseFocus } from './focus-trap.js';
import {
  classifyWorktree,
  canDelete,
  canRename,
  needsConfirm,
  deleteBlockers,
  statusLabel,
  branchStatusLabel,
  sortWorktrees,
  sortBranches,
  shortPath,
  readSessionIds,
  readIsMain,
  readRepoRoot,
  readWorktrees,
  readOrphanBranches,
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
  openSessionIn: (projectId: string, worktreePath: string) => void;
}

let deps: WorktreesDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
  openSessionIn: () => {},
};

export const worktreesEl = pageEl('worktrees');
const titleProjectEl = pageEl('worktrees-project');
const listEl = pageEl('worktrees-list');
const branchesEl = pageEl('worktrees-branches');
const emptyEl = pageEl('worktrees-empty');
const emptyTextEl = pageEl('worktrees-empty-text');
const emptySpinnerEl = pageEl('worktrees-empty-spinner');
const treesSection = pageEl('worktrees-section-trees');
const branchesSection = pageEl('worktrees-section-branches');

const modalState: { projectId: string; lastPayload: WorktreesPayload | null } =
  {
    projectId: '',
    lastPayload: null,
  };

// Closing the browser (or re-rendering under it) dismisses any open
// question too — otherwise it stays on screen asking about a worktree
// the user is no longer looking at.

export function worktreesOpen(): boolean {
  return !worktreesEl.classList.contains('hidden');
}

export function openWorktrees(project: ProjectInfo | null): void {
  if (!project) {
    flashStatus('no project selected', true);
    return;
  }
  modalState.projectId = project.id;
  modalState.lastPayload = null;
  titleProjectEl.textContent = project.name ? `· ${project.name}` : '';
  renderLoading();
  worktreesEl.classList.remove('hidden');
  deps.setFocusedTile(null);
  refresh();
  setTimeout(() => pageEl('worktrees-close').focus(), 0);
}

export function closeWorktrees(): void {
  dismissChoiceDialog();
  // Drop focus before hiding, or it is left on a display:none element
  // and the browser resolves it to <body> — stranding the keyboard.
  releaseFocus(worktreesEl);
  worktreesEl.classList.add('hidden');
  modalState.projectId = '';
  deps.refocusActiveTerm();
}

function refresh(): void {
  if (!modalState.projectId) return;
  ListWorktrees(modalState.projectId).catch(reportFailure('list worktrees'));
}

// showEmpty puts the panel into its single-message state. spinning
// distinguishes "still working" from a settled answer — the spinner is
// the same one the session tiles use while a session starts.
function showEmpty(text: string, spinning: boolean): void {
  emptyTextEl.textContent = text;
  emptySpinnerEl.classList.toggle('hidden', !spinning);
  emptyEl.classList.remove('hidden');
  treesSection.classList.add('hidden');
  branchesSection.classList.add('hidden');
}

function renderLoading(): void {
  listEl.textContent = '';
  branchesEl.textContent = '';
  showEmpty('Reading worktrees…', true);
}

// handleWorktreesPayload is the "worktree:list" event sink, wired in
// events.ts. Payloads for other projects are ignored: the daemon
// answers per-project and a stale reply must not repaint the list the
// user is looking at.
export function handleWorktreesPayload(payload: WorktreesPayload): void {
  if (!worktreesOpen()) return;
  if (readProjectIdOf(payload) !== modalState.projectId) return;
  modalState.lastPayload = payload;
  render(payload);
}

function render(payload: WorktreesPayload): void {
  // The row a dialog was asking about may not survive this repaint.
  dismissChoiceDialog();
  listEl.textContent = '';
  branchesEl.textContent = '';

  if (!readRepoRoot(payload)) {
    showEmpty(
      'This project’s working directory is not a git repository, so it has no worktrees.',
      false,
    );
    return;
  }
  emptyEl.classList.add('hidden');
  treesSection.classList.remove('hidden');

  for (const w of sortWorktrees(readWorktrees(payload))) {
    listEl.appendChild(worktreeRow(w));
  }

  const branches = sortBranches(readOrphanBranches(payload));
  branchesSection.classList.toggle('hidden', branches.length === 0);
  for (const b of branches) {
    branchesEl.appendChild(branchRow(b));
  }
}

function makeButton(
  label: string,
  title: string,
  onClick: () => void,
  opts: { danger?: boolean; disabled?: boolean; opensLauncher?: boolean } = {},
): HTMLButtonElement {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.textContent = label;
  btn.title = title;
  // Tells the launcher's outside-click handler that this click is what
  // opened it, so it doesn't close it again on the same event.
  if (opts.opensLauncher) btn.dataset.opensLauncher = '';
  if (opts.danger) btn.className = 'danger';
  if (opts.disabled) btn.disabled = true;
  else btn.addEventListener('click', onClick);
  return btn;
}

function worktreeRow(w: WorktreeInfo): HTMLElement {
  const row = document.createElement('div');
  row.className = 'worktree-row';
  const kind = classifyWorktree(w);
  row.dataset.kind = kind;
  row.dataset.path = w.path;

  const main = document.createElement('div');
  main.className = 'worktree-main';
  const name = document.createElement('span');
  name.className = 'worktree-name';
  name.textContent = w.branch || shortPath(w.path);
  // The full path is what identifies the row unambiguously; the
  // visible label is the readable half.
  name.title = w.path;
  const status = document.createElement('span');
  status.className = 'worktree-status';
  status.textContent = statusLabel(w);
  main.appendChild(name);
  main.appendChild(status);
  row.appendChild(main);

  if (readIsMain(w)) {
    const badge = document.createElement('span');
    badge.className = 'worktree-badge';
    badge.textContent = 'main';
    row.appendChild(badge);
  }

  const actions = document.createElement('div');
  actions.className = 'worktree-actions';

  if (!readIsMain(w)) {
    actions.appendChild(
      makeButton(
        'Open session',
        'Start a session in this worktree',
        () => {
          // Close FIRST, then open the launcher. closeWorktrees refocuses
          // the active terminal, and the launcher closes itself on
          // focusout — opening it first means it vanishes immediately.
          const projectId = modalState.projectId;
          closeWorktrees();
          deps.openSessionIn(projectId, w.path);
        },
        { opensLauncher: true },
      ),
    );

    const renameBlocked = !canRename(w);
    actions.appendChild(
      makeButton(
        'Rename',
        renameBlocked
          ? readSessionIds(w).length > 0
            ? 'Close the sessions in it first — renaming moves the directory'
            : 'This worktree has no branch to rename'
          : 'Rename the branch and move the directory to match',
        () => startRename(w),
        { disabled: renameBlocked },
      ),
    );

    const blockers = deleteBlockers(w);
    const deletable = canDelete(w);
    actions.appendChild(
      makeButton(
        'Delete',
        deletable
          ? blockers.map((b) => b.reason).join(' ') || 'Delete this worktree'
          : blockers.map((b) => b.reason).join(' '),
        () => confirmAndDelete(w),
        { danger: true, disabled: !deletable },
      ),
    );
  }
  row.appendChild(actions);
  return row;
}

function branchRow(b: BranchInfo): HTMLElement {
  const row = document.createElement('div');
  row.className = 'worktree-row';
  row.dataset.branch = b.name;

  const main = document.createElement('div');
  main.className = 'worktree-main';
  const name = document.createElement('span');
  name.className = 'worktree-name';
  name.textContent = b.name;
  const status = document.createElement('span');
  status.className = 'worktree-status';
  status.textContent = branchStatusLabel(b);
  main.appendChild(name);
  main.appendChild(status);
  row.appendChild(main);

  const actions = document.createElement('div');
  actions.className = 'worktree-actions';
  actions.appendChild(
    makeButton(
      'Create worktree',
      `Check out ${b.name} in a new worktree`,
      () => {
        CreateWorktree(modalState.projectId, b.name).catch(
          reportFailure('create worktree'),
        );
      },
    ),
  );
  actions.appendChild(
    makeButton(
      'Delete',
      b.merged
        ? `Delete the branch ${b.name} (already merged)`
        : `Delete the branch ${b.name} — it has unmerged commits`,
      () => confirmAndDeleteBranch(b),
      { danger: true },
    ),
  );
  row.appendChild(actions);
  return row;
}

// confirmAndDeleteBranch removes an orphaned branch. force is sent only
// for an unmerged branch, which git otherwise refuses outright; the
// daemon re-checks either way and answers with branch_unmerged if this
// view is stale.
async function confirmAndDeleteBranch(b: BranchInfo): Promise<void> {
  if (!(await askDeleteBranch(b))) return;
  if (!worktreesOpen() || !modalState.projectId) return;
  DeleteBranch(modalState.projectId, b.name, !b.merged).catch(
    reportFailure('delete branch'),
  );
}

// startRename swaps the row's label for an input, via the shared
// inline-rename helper the sidebar and tile titles already use — same
// commit-on-Enter, cancel-on-Escape, cancel-on-blur behaviour, and the
// same registration that lets keyboard.ts give Escape to the editor
// instead of closing this panel.
function startRename(w: WorktreeInfo): void {
  const row = listEl.querySelector<HTMLElement>(
    `.worktree-row[data-path="${cssEscape(w.path)}"]`,
  );
  const main = row?.querySelector<HTMLElement>('.worktree-main');
  if (!main) return;
  const label = main.innerHTML;

  beginInlineRename({
    value: w.branch ?? '',
    className: 'worktree-rename',
    mount: (input) => {
      main.textContent = '';
      main.appendChild(input);
      input.setAttribute('aria-label', 'New branch name');
    },
    // Restore the row exactly as it was. A commit repaints from the
    // daemon's reply moments later anyway; this is what the user sees
    // in between, and after a cancel it is the final state.
    unmount: () => {
      main.innerHTML = label;
    },
    onCommit: (next) => {
      RenameWorktree(modalState.projectId, w.path, next).catch(
        reportFailure('rename worktree'),
      );
    },
  });
}

// cssEscape guards the attribute selector above. CSS.escape is present
// in every browser the Wails webview targets, but jsdom in older
// environments can lack it, and a missing escape would throw on a
// branch name containing a quote.
function cssEscape(value: string): string {
  const fn = (globalThis as { CSS?: { escape?: (s: string) => string } }).CSS
    ?.escape;
  return fn ? fn(value) : value.replace(/["\\]/g, '\\$&');
}

// What the user chose in the delete dialog.
type DeleteChoice = 'both' | 'keep-branch' | 'cancel';

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
    choices.push({ label: 'Delete both', value: 'both', danger: true });
  }
  const answer = await openChoiceDialog({
    title: needsConfirm(w)
      ? 'Delete this worktree and lose work?'
      : 'Delete this worktree?',
    detail: w.path,
    bullets: deleteBlockers(w).map((b) => b.reason),
    note: w.branch
      ? `Keeping the branch “${w.branch}” leaves its commits recoverable. Deleting both cannot be undone.`
      : 'This worktree has no branch. Deleting it cannot be undone.',
    choices,
  });
  return answer as DeleteChoice;
}

// askDeleteBranch confirms removing an orphaned branch — the last
// handle on whatever commits only it has.
async function askDeleteBranch(b: BranchInfo): Promise<boolean> {
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
    choices: [
      { label: 'Cancel', value: 'cancel' },
      { label: 'Delete branch', value: 'delete', danger: !b.merged },
    ],
  });
  return answer === 'delete';
}

// confirmAndDelete is the destructive path. The blockers are computed
// locally so the dialog can name exactly what is at stake, and force is
// sent only when something actually blocks — the daemon re-checks
// regardless and refuses on the wire if this view is stale.
async function confirmAndDelete(w: WorktreeInfo): Promise<void> {
  const choice = await askDelete(w);
  if (choice === 'cancel') return;
  // The modal can have been closed (or moved to another project) while
  // the dialog was open.
  if (!worktreesOpen() || !modalState.projectId) return;

  RemoveWorktree(
    modalState.projectId,
    w.path,
    needsConfirm(w),
    choice === 'both',
  ).catch(reportFailure('delete worktree'));
}

export function initWorktrees(injected: WorktreesDeps): void {
  deps = injected;
  registerModal(worktreesEl);
  pageEl('worktrees-close').addEventListener('click', closeWorktrees);
  worktreesEl.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      closeWorktrees();
    } else if (
      (e.key === 'r' || e.key === 'R') &&
      !e.metaKey &&
      !e.ctrlKey &&
      // Never steal a keystroke from the inline rename input.
      !(e.target instanceof HTMLInputElement)
    ) {
      e.preventDefault();
      refresh();
    }
  });
  // Click on the backdrop (not the panel) closes, matching the
  // launcher's outside-click behaviour.
  worktreesEl.addEventListener('mousedown', (e) => {
    if (e.target === worktreesEl) closeWorktrees();
  });
}
