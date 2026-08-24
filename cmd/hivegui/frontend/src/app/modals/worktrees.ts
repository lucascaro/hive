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

// appendSubject adds the branch tip's commit subject under the row's
// status line — a branch name says what the work was called, this says
// what it is. Skipped when the daemon had none (a detached worktree,
// or an older daemon that does not send it).
function appendSubject(main: HTMLElement, subject?: string): void {
  if (!subject) return;
  const el = document.createElement('span');
  el.className = 'worktree-subject';
  el.textContent = subject;
  el.title = subject;
  main.appendChild(el);
}

// makeBadge builds the small uppercase tag at the right of a row.
function makeBadge(text: string, merged = false): HTMLElement {
  const badge = document.createElement('span');
  badge.className = merged ? 'worktree-badge merged' : 'worktree-badge';
  badge.textContent = text;
  return badge;
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
  appendSubject(main, w.subject);
  row.appendChild(main);

  if (readIsMain(w)) {
    row.appendChild(makeBadge('main'));
  } else if (w.merged) {
    // The whole point of the merged check is to be spottable at a
    // glance, so it gets a badge rather than a word in the status line.
    row.appendChild(makeBadge('merged', true));
  }

  const actions = document.createElement('div');
  actions.className = 'worktree-actions';

  if (!readIsMain(w)) {
    const startSession = (continueConversation: boolean) => {
      // Close FIRST, then open the launcher. closeWorktrees refocuses
      // the active terminal, and the launcher closes itself on
      // focusout — opening it first means it vanishes immediately.
      const projectId = modalState.projectId;
      closeWorktrees();
      deps.openSessionIn(projectId, w.path, continueConversation);
    };
    actions.appendChild(
      makeButton(
        'New session',
        'Start a fresh session in this worktree',
        () => startSession(false),
        { opensLauncher: true },
      ),
    );
    actions.appendChild(
      makeButton(
        'Continue',
        'Start a session and resume the last conversation in this worktree',
        () => startSession(true),
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
  appendSubject(main, b.subject);
  row.appendChild(main);
  if (b.merged) row.appendChild(makeBadge('merged', true));

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
  const choice = await askDeleteBranch(b);
  if (choice === 'cancel') return;
  if (!worktreesOpen() || !modalState.projectId) return;
  DeleteBranch(
    modalState.projectId,
    b.name,
    !b.merged,
    choice === 'remote',
  ).catch(reportFailure('delete branch'));
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
    choice === 'both' || choice === 'everywhere',
    choice === 'everywhere',
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
