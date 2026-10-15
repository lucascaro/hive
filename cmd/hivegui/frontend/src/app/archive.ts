// ---------- archive (spec 518) ----------
//
// Archive puts a session away: the daemon stops its process and keeps a
// durable record (name, project, agent, worktree, conversation id) that
// never expires. Unarchive brings it back through the same restore path
// as ⌘Z, so what can be lost on the way — scrollback always, sometimes
// the worktree or the conversation — is reported the same honest way.
//
// Unlike Close there is no dirty-worktree question: the worktree is not
// touched. So archiving never prompts, and never raises the undo banner
// (it has its own way back).
//
// Nothing here holds a copy of an archived session. The list is the
// daemon's, refreshed by archived:list.

import {
  ArchiveSession,
  DeleteArchivedSession,
  ListArchivedSessions,
  UnarchiveSession,
} from '../bridge.js';
import { flashStatus, reportFailure } from './dom.js';
import { openChoiceDialog } from './modals/choice-dialog.js';
import { restoreLosses, type RestoredEvent } from './undo-close.js';
import { appStore, setArchived } from '../store/store.js';
import type { ArchivedSessionInfo } from './state.js';

const appData = () => appStore.getState();

// Archives and unarchives THIS client asked for, by session id, until
// the daemon's answer lands: the answer is what the status line reports,
// and a window that did not ask must not announce somebody else's.
const pendingArchive = new Map<string, string>();
const pendingUnarchive = new Map<string, string>();

/** Ask the daemon for the archived list; the answer is archived:list. */
export function refreshArchived(): void {
  ListArchivedSessions().catch(reportFailure('list archived sessions'));
}

/** Archive one session. No confirmation: nothing is destroyed. */
export function archiveSession(id: string): void {
  const s = appData().sessions.find((x) => x.id === id);
  if (!s) return;
  pendingArchive.set(id, s.name ?? 'Session');
  ArchiveSession(id).catch((err) => {
    pendingArchive.delete(id);
    reportFailure('archive session')(err);
  });
}

/** ⌘-less palette command: archive the focused session. Declines with
 * nothing to archive so the palette does not report a success. */
export function archiveActiveSession(): boolean | undefined {
  const id = appData().activeId;
  if (!id) return false;
  archiveSession(id);
}

export function unarchiveSession(id: string): void {
  const a = appData().archived.find((x) => x.session_id === id);
  pendingUnarchive.set(id, a?.name ?? 'Session');
  UnarchiveSession(id).catch((err) => {
    pendingUnarchive.delete(id);
    reportFailure('unarchive session')(err);
  });
}

/** events.ts › archived:list. Publishes the list and reports the
 * archives this window asked for that have now landed. */
export function onArchivedList(list: ArchivedSessionInfo[]): void {
  setArchived(list);
  const here = new Set(list.map((a) => a.session_id));
  for (const [id, name] of pendingArchive) {
    if (!here.has(id)) continue;
    pendingArchive.delete(id);
    flashStatus(
      `Archived “${name}” — its worktree is untouched. Bring it back from Archived Sessions…`,
    );
  }
}

/** events.ts › session:restored. True when this was an unarchive this
 * window asked for, so the ⌘Z banner does not describe it as a reopen. */
export function onUnarchived(ev: RestoredEvent): boolean {
  const id = ev.session_id ?? ev.sessionId ?? '';
  const asked = pendingUnarchive.get(id);
  if (asked === undefined) return false;
  pendingUnarchive.delete(id);
  const name = appData().sessions.find((s) => s.id === id)?.name ?? asked;
  const losses = restoreLosses(ev);
  flashStatus(
    losses.length
      ? `Unarchived “${name}” — scrollback is gone, and ${losses.join('; ')}.`
      : `Unarchived “${name}”. Scrollback is gone; everything else came back.`,
  );
  return true;
}

const ARCHIVE_ERRORS: Record<string, string> = {
  archive_busy:
    'that session is still starting or closing — try again in a moment',
  no_such_session: 'that session no longer exists',
  archive_failed: 'could not archive the session',
  no_such_archived_session: 'that session is no longer archived',
  session_already_open: 'that session is already open',
  unarchive_failed: 'could not unarchive the session',
  delete_archived_failed: 'could not delete the archived session',
};

/** events.ts › control:error. Returns true when the code was one of
 * this feature's, so the generic status line is not shown twice. */
export function onArchiveError(
  code: string | undefined,
  sessionId?: string,
): boolean {
  if (!code || !(code in ARCHIVE_ERRORS)) return false;
  const pending =
    code === 'archive_busy' ||
    code === 'no_such_session' ||
    code === 'archive_failed'
      ? pendingArchive
      : pendingUnarchive;
  // no_such_session and session_already_open are not archive's alone
  // (an ACP prompt or a restore can raise them too): claim them only
  // while a request of this kind is outstanding, and never one that
  // names a session this window did not ask about.
  if (code === 'no_such_session' || code === 'session_already_open') {
    if (pending.size === 0) return false;
    if (sessionId && !pending.has(sessionId)) return false;
  }
  // These errors carry no session id, so every outstanding request of
  // this kind is dropped rather than guessing which one failed.
  pending.clear();
  flashStatus(ARCHIVE_ERRORS[code], true);
  return true;
}

/** Delete an archived session for good. Asks first, and names exactly
 * what goes: the record always, the worktree only if the user says so. */
export async function confirmAndDeleteArchived(
  a: ArchivedSessionInfo,
): Promise<void> {
  const hasWorktree = !!a.worktree_branch;
  const answer = await openChoiceDialog({
    title: 'Delete this archived session?',
    detail: a.name,
    note: hasWorktree
      ? 'Deleting discards the saved session and its conversation link. Its worktree stays on disk unless you also remove it — which deletes any uncommitted changes in it.'
      : 'Deleting discards the saved session and its conversation link. This cannot be undone.',
    choices: [
      { label: 'Cancel', value: 'cancel' },
      { label: 'Delete, keep worktree', value: 'keep' },
      ...(hasWorktree
        ? [
            {
              label: 'Delete and remove worktree',
              value: 'remove',
              danger: true,
            },
          ]
        : []),
    ],
  });
  if (answer === 'cancel') return;
  DeleteArchivedSession(a.session_id, answer === 'remove', false).catch(
    reportFailure('delete archived session'),
  );
}

/** events.ts › control:error archived_worktree_dirty. The daemon
 * refused to remove a worktree holding uncommitted changes; ask once
 * more, naming the loss, and only then force it. */
export async function onArchivedWorktreeDirty(
  sessionId: string,
): Promise<void> {
  const a = appData().archived.find((x) => x.session_id === sessionId);
  const answer = await openChoiceDialog({
    title: 'Remove the worktree anyway?',
    detail: a?.name ?? 'Archived session',
    bullets: [
      `It has uncommitted changes${a?.worktree_branch ? ` in ${a.worktree_branch}` : ''}.`,
    ],
    note: 'The saved session is discarded either way. Removing the worktree deletes those changes now, which cannot be undone.',
    choices: [
      { label: 'Cancel', value: 'cancel' },
      {
        label: 'Delete and remove worktree',
        value: 'force',
        danger: true,
      },
    ],
  });
  if (answer !== 'force') return;
  DeleteArchivedSession(sessionId, true, true).catch(
    reportFailure('delete archived session'),
  );
}

/** Test seam. */
export function resetArchiveForTest(): void {
  pendingArchive.clear();
  pendingUnarchive.clear();
}
