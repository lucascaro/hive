// ---------- worktree browser ----------
//
// React port of src/app/modals/worktrees.ts, which keeps the daemon
// round-trip, the destructive confirmations and the "worktree:list"
// event sink. This file is the panel.
//
// It renders from one store field: the daemon answers every mutation
// with a fresh inventory, so there is nothing to patch — `null` is the
// loading state and every arriving payload is a full repaint.

import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import {
  beginInlineRename,
  cancelInlineRenameFor,
} from '../../app/inline-rename.js';
import {
  closeWorktrees,
  confirmAndDelete,
  confirmAndDeleteBranch,
  createWorktreeFor,
  openSessionIn,
  refresh,
  renameWorktreeTo,
  worktreesProjectId,
} from '../../app/modals/worktrees.js';
import {
  branchStatusLabel,
  canDelete,
  canRename,
  classifyWorktree,
  deleteBlockers,
  readIsMain,
  readOrphanBranches,
  readRepoRoot,
  readSessionIds,
  readWorktrees,
  shortPath,
  sortBranches,
  sortWorktrees,
  statusLabel,
  type BranchInfo,
  type WorktreeInfo,
} from '../../lib/worktrees.js';
import { useAppStore } from '../../store/store.js';
import { ModalShell } from './ModalShell.js';

export function Worktrees({ root }: { root: HTMLElement | null }): ReactNode {
  const entry = useAppStore((s) => s.modals.find((m) => m.id === 'worktrees'));

  // #worktrees sits outside React's tree, so its open/closed class is
  // applied here — as a layout effect, because that class is what every
  // keyboard gate and e2e visibility assertion reads and it has to be
  // right in the frame the panel first paints.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  if (!entry || !root) return null;
  // Remounted per opening: an in-progress rename belongs to the panel
  // that started it, not to the next project the user opens.
  return (
    <WorktreesPanel
      key={entry.seq}
      root={root}
      projectName={entry.projectName}
    />
  );
}

function WorktreesPanel({
  root,
  projectName,
}: {
  root: HTMLElement;
  projectName: string;
}): ReactNode {
  const payload = useAppStore((s) => s.worktreesPayload);
  // The path of the row being renamed, if any. The row still renders —
  // its label area is left empty for the imperative input to mount into.
  const [renaming, setRenaming] = useState<string | null>(null);

  // Mount-only: this effect IS the open. The close button is where focus
  // lands, same as every other dialog — without it focus stays on the
  // terminal and keystrokes leak behind the backdrop.
  useEffect(() => {
    document.getElementById('worktrees-close')?.focus();
  }, []);

  // Escape, the close button and the backdrop are ModalShell's; only the
  // refresh key is this panel's. On the root rather than a wrapper: the
  // key has to work wherever focus sits inside the dialog.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== 'r' && e.key !== 'R') return;
      if (e.metaKey || e.ctrlKey) return;
      // Never steal a keystroke from the inline rename input.
      if (e.target instanceof HTMLInputElement) return;
      e.preventDefault();
      refresh();
    }
    root.addEventListener('keydown', onKeyDown);
    return () => root.removeEventListener('keydown', onKeyDown);
  }, [root]);

  const repoRoot = payload ? readRepoRoot(payload) : '';
  const loading = payload === null;
  const notARepo = !loading && !repoRoot;
  const worktrees =
    payload && repoRoot ? sortWorktrees(readWorktrees(payload)) : [];
  const branches =
    payload && repoRoot ? sortBranches(readOrphanBranches(payload)) : [];

  return (
    <ModalShell
      id="worktrees"
      root={root}
      title="Worktrees"
      titleSuffix={
        <span id="worktrees-project">
          {projectName ? `· ${projectName}` : ''}
        </span>
      }
      size="lg"
      onClose={closeWorktrees}
      // patterns.md > Keyboard hints: `[…]` for symbols, `(…)` for letters.
      hints={[
        { keys: '[esc]', label: 'close' },
        { keys: '(r)', label: 'refresh' },
      ]}
    >
      {/* The single-message state. The spinner distinguishes "still
          working" from a settled answer — it is the one the session tiles
          use while a session starts. */}
      <div
        id="worktrees-empty"
        className={
          loading || notARepo ? 'worktrees-empty' : 'worktrees-empty hidden'
        }
      >
        <div className="worktrees-empty-card">
          <span
            id="worktrees-empty-spinner"
            className={loading ? 'phase-spinner' : 'phase-spinner hidden'}
          />
          <span id="worktrees-empty-text">
            {loading
              ? 'Reading worktrees…'
              : 'This project’s working directory is not a git repository, so it has no worktrees.'}
          </span>
        </div>
      </div>
      <div id="worktrees-body">
        <section
          id="worktrees-section-trees"
          className={loading || notARepo ? 'hidden' : undefined}
        >
          <h4>Worktrees</h4>
          <div id="worktrees-list">
            {worktrees.map((w) => (
              <WorktreeRow
                key={w.path}
                w={w}
                renaming={renaming === w.path}
                onStartRename={() => setRenaming(w.path)}
                onEndRename={() => setRenaming(null)}
              />
            ))}
          </div>
        </section>
        <section
          id="worktrees-section-branches"
          className={branches.length === 0 ? 'hidden' : undefined}
        >
          <h4>Branches with no worktree</h4>
          <p className="worktrees-hint">
            Create a worktree to pick this work back up.
          </p>
          <div id="worktrees-branches">
            {branches.map((b) => (
              <BranchRow key={b.name} b={b} />
            ))}
          </div>
        </section>
      </div>
    </ModalShell>
  );
}

// A row action. Deliberately a plain <button>, not the ui/button
// primitive: worktrees.css styles `.worktree-actions button` directly and
// the e2e specs select the danger one by class.
function RowButton({
  label,
  title,
  onClick,
  danger,
  disabled,
  opensLauncher,
}: {
  label: string;
  title: string;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
  opensLauncher?: boolean;
}): ReactNode {
  return (
    <button
      type="button"
      title={title}
      className={danger ? 'danger' : undefined}
      disabled={disabled}
      // Tells the launcher's outside-click handler that this click is
      // what opened it, so it doesn't close it again on the same event.
      data-opens-launcher={opensLauncher ? '' : undefined}
      onClick={onClick}
    >
      {label}
    </button>
  );
}

// The branch tip's commit subject, under the status line: a branch name
// says what the work was called, this says what it is. Absent when the
// daemon had none (a detached worktree, or an older daemon).
function Subject({ subject }: { subject?: string }): ReactNode {
  if (!subject) return null;
  return (
    <span className="worktree-subject" title={subject}>
      {subject}
    </span>
  );
}

function WorktreeRow({
  w,
  renaming,
  onStartRename,
  onEndRename,
}: {
  w: WorktreeInfo;
  renaming: boolean;
  onStartRename: () => void;
  onEndRename: () => void;
}): ReactNode {
  const mainRef = useRef<HTMLDivElement | null>(null);

  // The rename is still the shared imperative helper, for two reasons a
  // React-owned input would lose: keyboard.ts asks inlineRenameActive()
  // FIRST, so Escape cancels the edit instead of closing the panel; and
  // the commit/cancel/blur rules stay identical to the sidebar's and the
  // tile titles'. It mounts into an EMPTY .worktree-main — React owns no
  // children there while the rename is up, so a daemon repaint mid-edit
  // leaves the input alone (the imperative version rebuilt the row and
  // lost the edit).
  //
  // Keyed on `renaming` alone. The branch, the path and the callback are
  // read through a ref instead of listed as dependencies: a daemon
  // repaint re-renders this row, and an effect that re-ran on it would
  // start a SECOND rename over the first — two inputs in the row, the
  // second one seeded from the value the first was editing away from.
  const opts = useRef({ w, onEndRename });
  opts.current = { w, onEndRename };
  useEffect(() => {
    if (!renaming) return;
    const main = mainRef.current;
    if (!main) return;
    const { w: row, onEndRename: done } = opts.current;
    const input = beginInlineRename({
      value: row.branch ?? '',
      className: 'worktree-rename',
      mount: (input) => {
        input.setAttribute('aria-label', 'New branch name');
        main.appendChild(input);
      },
      unmount: (input) => input.remove(),
      onCommit: (next) => renameWorktreeTo(row.path, next),
      onDone: done,
    });
    // beginInlineRename registers the edit module-side, and only
    // Enter/Escape/blur clear that registration. React removing a
    // focused input does not fire blur, so a row that unmounts mid-edit
    // (the daemon dropped it, or the panel closed) would leave
    // inlineRenameActive() true — and that is the FIRST branch of
    // keyboard.ts's ladder, which swallows every keystroke in the app
    // until an Escape it no longer has an input to cancel.
    return () => {
      cancelInlineRenameFor(input);
    };
  }, [renaming]);

  const kind = classifyWorktree(w);
  const isMain = readIsMain(w);
  const renameBlocked = !canRename(w);
  const blockers = deleteBlockers(w);
  const deletable = canDelete(w);

  const startSession = (continueConversation: boolean) => {
    // Read the project BEFORE closing: closing drops the modal entry
    // this id lives on. Close FIRST, then open the launcher —
    // closeWorktrees refocuses the active terminal and the launcher
    // closes itself on focusout, so opening it first means it vanishes
    // immediately.
    const projectId = worktreesProjectId();
    closeWorktrees();
    openSessionIn(projectId, w.path, continueConversation);
  };

  return (
    <div className="worktree-row" data-kind={kind} data-path={w.path}>
      <div className="worktree-main" ref={mainRef}>
        {renaming ? null : (
          <>
            {/* The full path is what identifies the row unambiguously;
                the visible label is the readable half. */}
            <span className="worktree-name" title={w.path}>
              {w.branch || shortPath(w.path)}
            </span>
            <span className="worktree-status">{statusLabel(w)}</span>
            <Subject subject={w.subject} />
          </>
        )}
      </div>
      {isMain ? (
        <span className="worktree-badge">main</span>
      ) : w.merged ? (
        // The whole point of the merged check is to be spottable at a
        // glance, so it gets a badge rather than a word in the status
        // line.
        <span className="worktree-badge merged">merged</span>
      ) : null}
      <div className="worktree-actions">
        {isMain ? null : (
          <>
            <RowButton
              label="New session"
              title="Start a fresh session in this worktree"
              opensLauncher
              onClick={() => startSession(false)}
            />
            <RowButton
              label="Continue"
              title="Start a session and resume the last conversation in this worktree"
              opensLauncher
              onClick={() => startSession(true)}
            />
            <RowButton
              label="Rename"
              title={
                renameBlocked
                  ? readSessionIds(w).length > 0
                    ? 'Close the sessions in it first — renaming moves the directory'
                    : 'This worktree has no branch to rename'
                  : 'Rename the branch and move the directory to match'
              }
              disabled={renameBlocked}
              onClick={onStartRename}
            />
            <RowButton
              label="Delete"
              title={
                deletable
                  ? blockers.map((b) => b.reason).join(' ') ||
                    'Delete this worktree'
                  : blockers.map((b) => b.reason).join(' ')
              }
              danger
              disabled={!deletable}
              onClick={() => void confirmAndDelete(w)}
            />
          </>
        )}
      </div>
    </div>
  );
}

function BranchRow({ b }: { b: BranchInfo }): ReactNode {
  return (
    <div className="worktree-row" data-branch={b.name}>
      <div className="worktree-main">
        <span className="worktree-name">{b.name}</span>
        <span className="worktree-status">{branchStatusLabel(b)}</span>
        <Subject subject={b.subject} />
      </div>
      {b.merged ? <span className="worktree-badge merged">merged</span> : null}
      <div className="worktree-actions">
        <RowButton
          label="Create worktree"
          title={`Check out ${b.name} in a new worktree`}
          onClick={() => createWorktreeFor(b.name)}
        />
        <RowButton
          label="Delete"
          title={
            b.merged
              ? `Delete the branch ${b.name} (already merged)`
              : `Delete the branch ${b.name} — it has unmerged commits`
          }
          danger
          onClick={() => void confirmAndDeleteBranch(b)}
        />
      </div>
    </div>
  );
}
