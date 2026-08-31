// Undo for an accidental session close.
//
// A close leaves a tombstone in the daemon's state dir, so reopening
// is a real operation rather than a held-back teardown. Two ways in:
// the banner this module raises the moment a close lands, and ⌘Z /
// File ▸ Reopen Closed Session for anything older than that.
//
// The banner is deliberately honest. Scrollback never comes back, and
// a restore can also lose the worktree or the agent conversation — so
// the follow-up message reports what was actually degraded instead of
// saying "restored" and letting the user discover the gap themselves.
import { KillSession, RestoreSession } from '../bridge.js';
import { state } from './state.js';
import { reportFailure } from './dom.js';
import { banner, type Banner } from '../ui/banner.js';

/** How long the undo offer stays on screen. The tombstone outlives it. */
const BANNER_MS = 15_000;

/** Shape of the daemon's session:restored payload (snake_case on the wire). */
interface RestoredEvent {
  session_id?: string;
  sessionId?: string;
  project_reassigned?: boolean;
  projectReassigned?: boolean;
  worktree_recreated?: boolean;
  worktreeRecreated?: boolean;
  worktree_lost?: boolean;
  worktreeLost?: boolean;
  conversation_lost?: boolean;
  conversationLost?: boolean;
  agent_fell_back?: boolean;
  agentFellBack?: boolean;
  patch_path?: string;
  patchPath?: string;
  patch_skipped?: boolean;
  patchSkipped?: boolean;
}

/** One close this client issued, remembered until its event lands. */
interface PendingClose {
  name: string;
  /** The close also asked for the worktree to be deleted. */
  deletedWorktree: boolean;
}

// Closes THIS client issued, by session id. Undo belongs to whoever
// pressed close: a session removed by another window (or by a project
// kill) must not raise a banner here, so the removed event alone is
// not enough to act on.
const pending = new Map<string, PendingClose>();

let undoBanner: Banner | null = null;
let hideTimer: ReturnType<typeof setTimeout> | null = null;

/**
 * Record that this client is closing `id`. Called at the close call
 * site, before the request goes out — the removed event carries no
 * hint of who asked for it.
 */
export function noteLocalClose(
  id: string,
  name: string,
  deletedWorktree = false,
) {
  if (!id) return;
  pending.set(id, { name: name || 'Session', deletedWorktree });
}

/**
 * Handle a session:event(removed). Raises the undo banner when this
 * client issued the close, and does nothing otherwise.
 */
export function onSessionRemoved(id: string) {
  const p = pending.get(id);
  if (!p) return;
  pending.delete(id);
  showUndo(id, p);
}

function showUndo(id: string, p: PendingClose) {
  if (!undoBanner) return;
  undoBanner.setText(
    p.deletedWorktree
      ? `Closed “${p.name}” and deleted its worktree.`
      : `Closed “${p.name}”.`,
  );
  const action = undoBanner.action('undo');
  action.textContent = p.deletedWorktree ? 'Reopen session' : 'Undo';
  action.hidden = false;
  undoBanner.el.dataset.sessionId = id;
  undoBanner.show();
  restartHideTimer();
}

function restartHideTimer() {
  if (hideTimer) clearTimeout(hideTimer);
  hideTimer = setTimeout(() => undoBanner?.hide(), BANNER_MS);
}

/**
 * Close the active session, remembering that this client asked for it.
 *
 * The single close entry point for ⌘W, the File menu and the command
 * palette — it exists so noteLocalClose can never be forgotten at one
 * of them. Without the note the removed event is indistinguishable
 * from a close another window issued, and the undo banner silently
 * does not appear.
 *
 * force=false, like every other live-session close: the daemon refuses
 * with worktree_dirty when there are uncommitted changes, and
 * events.ts asks the real three-way question.
 */
export function closeActiveSession() {
  const id = state.activeId;
  if (!id) return;
  const s = state.sessions.find((x) => x.id === id);
  noteLocalClose(id, s?.name ?? 'Session');
  KillSession(id, false).catch(reportFailure('close'));
}

/**
 * Reopen a closed session. An empty id means "the most recently
 * closed one" — resolved daemon-side so ⌘Z cannot race a retention
 * prune between listing and restoring.
 */
export function reopenClosedSession(id = '') {
  RestoreSession(id).catch(reportFailure('reopen session'));
}

/** ⌘Z / File ▸ Reopen Closed Session. */
export function reopenLastClosedSession() {
  reopenClosedSession('');
}

/**
 * Report the outcome of a restore. Replaces the banner text rather
 * than dismissing it: a restore that silently dropped the worktree or
 * the conversation would otherwise look like a clean undo.
 */
export function onSessionRestored(ev: RestoredEvent) {
  if (!undoBanner) return;
  const losses: string[] = [];
  if (ev.worktree_lost ?? ev.worktreeLost) {
    losses.push('its worktree could not be restored');
  } else if (ev.worktree_recreated ?? ev.worktreeRecreated) {
    losses.push(
      'the worktree was rebuilt from its branch, without uncommitted changes',
    );
  }
  if (ev.conversation_lost ?? ev.conversationLost) {
    losses.push('the agent started a new conversation');
  }
  if (ev.agent_fell_back ?? ev.agentFellBack) {
    losses.push('its agent is gone, so it came back as a shell');
  }
  if (ev.project_reassigned ?? ev.projectReassigned) {
    losses.push('its project was deleted, so it moved to the default one');
  }
  if (ev.patch_skipped ?? ev.patchSkipped) {
    losses.push('the uncommitted changes were too large to save');
  }
  const patch = ev.patch_path ?? ev.patchPath;
  if (patch) losses.push(`uncommitted changes were saved to ${patch}`);

  // Name the session that actually came back. ⌘Z sends an empty id and
  // the daemon resolves "the last closed one", so the session restored
  // is not necessarily the one the banner was offering undo for —
  // reporting an unqualified "Session reopened" would then describe the
  // wrong session. The event carries the id precisely so this message
  // can be specific; falling back to the generic noun is fine when the
  // added event has not landed yet.
  const id = ev.session_id ?? ev.sessionId ?? '';
  const name = state.sessions.find((s) => s.id === id)?.name;
  const subject = name ? `“${name}” reopened` : 'Session reopened';

  // Scrollback is unconditional: there is no disk-backed scrollback to
  // replay from, so every restore starts with an empty terminal. Said
  // once, plainly, rather than per-restore.
  const text = losses.length
    ? `${subject} — scrollback is gone, and ${losses.join('; ')}.`
    : `${subject}. Scrollback is gone; everything else came back.`;

  undoBanner.setText(text);
  undoBanner.action('undo').hidden = true;
  undoBanner.show();
  restartHideTimer();
}

/**
 * Build and mount the banner. Lazy, like the daemon and update
 * banners: jsdom tests import this module without ever closing a
 * session, and a mount on import would inject markup into scaffolds
 * that do not expect it.
 */
export function initUndoClose() {
  if (undoBanner) return;
  undoBanner = banner({
    kind: 'info',
    actions: [
      {
        id: 'undo',
        label: 'Undo',
        onClick: () => {
          const id = undoBanner?.el.dataset.sessionId || '';
          undoBanner?.hide();
          reopenClosedSession(id);
        },
      },
    ],
    onDismiss: () => undoBanner?.hide(),
  });
  undoBanner.el.dataset.slot = 'undo-close';
  document.getElementById('app')?.prepend(undoBanner.el);
}

/** Test seam: drop all state between cases. */
export function resetUndoCloseForTest() {
  pending.clear();
  if (hideTimer) clearTimeout(hideTimer);
  hideTimer = null;
  undoBanner?.el.remove();
  undoBanner = null;
}
