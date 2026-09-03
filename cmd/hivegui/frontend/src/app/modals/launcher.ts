// ---------- agent launcher: the non-React half ----------
//
// The launcher renders from components/modals/Launcher.tsx (Phase 3).
// What stays here is everything that is not rendering:
//
//   * openLauncher / closeLauncher, the store actions every caller
//     (keyboard.ts, events.ts, main.ts, Sidebar.tsx, EmptyState.tsx)
//     already imports from this path,
//   * the launch-count table the agent list is ordered by,
//   * the three session actions that never open the launcher at all
//     (duplicate, restart) or that open it with a request built from
//     the active session.
//
// Focus-pipeline callbacks are injected via initLauncher(deps) — this
// module must never import the focus pipeline directly (main.ts owns
// that wiring).

import { flushSync } from 'react-dom';
import { DuplicateSession, RestartSession } from '../../bridge.js';
import { flashStatus, reportFailure } from '../dom.js';
import { activeProjectId, resolveSessionCwd } from '../selectors.js';
import { releaseFocus } from '../../lib/focus-trap.js';
import { pageEl } from '../el.js';
import {
  appStore,
  closeModal,
  isModalOpen,
  openModal,
} from '../../store/store.js';
import type { SessionInfo } from '../state.js';

// Live read of the store. A function, not a destructured snapshot: this
// module runs inside event handlers and must never cache a slice across
// a store write.
const appData = () => appStore.getState();

// Narrow on purpose: this modal needs exactly two callbacks off the
// focus pipeline, so it names those two rather than the whole module.
export interface LauncherDeps {
  setFocusedTile: (id: string | null) => void;
  refocusActiveTerm: () => void;
}

export interface LauncherOpts {
  forceWorktree?: boolean;
  duplicateFrom?: SessionInfo | null;
  duplicateCwd?: string;
  // worktreePath switches the launcher into "resume this worktree"
  // mode: the session runs in a worktree that already exists, so no
  // worktree row and no branch input are shown.
  worktreePath?: string;
  // continueConversation asks the agent to resume its most recent
  // conversation in that worktree instead of starting a new one.
  continueConversation?: boolean;
}

let deps: LauncherDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
};

export const launcherEl = pageEl('launcher');

// Agent id → launch count, persisted in localStorage. Read back as
// unknown JSON, so every lookup is `|| 0`.
export type AgentUsage = Record<string, number>;

export function loadAgentUsage(): AgentUsage {
  try {
    return JSON.parse(localStorage.getItem('hive.agentUsage') || '{}') || {};
  } catch {
    return {};
  }
}

export function bumpAgentUsage(id: string | undefined) {
  if (!id) return;
  const u = loadAgentUsage();
  u[id] = (u[id] || 0) + 1;
  try {
    localStorage.setItem('hive.agentUsage', JSON.stringify(u));
  } catch {}
}

// projectId is optional, not just nullable: main.ts, keyboard.ts and
// view.ts all call openLauncher() bare and let the `|| activeProjectId()`
// fallback below pick the project.
export function openLauncher(projectId?: string | null, opts?: LauncherOpts) {
  // Re-read the sticky pref each open so a one-shot forceWorktree from a
  // previous opening doesn't leak into the next regular open.
  // forceWorktree overrides for this opening only and is intentionally
  // not persisted.
  const forced =
    opts && typeof opts.forceWorktree === 'boolean' ? opts.forceWorktree : null;
  const duplicateFrom = opts?.duplicateFrom || null;
  const worktreePath = opts?.worktreePath || '';
  openModal({
    id: 'launcher',
    req: {
      projectId: projectId || activeProjectId(),
      // In duplicate mode the launcher is forking an existing session
      // into the same cwd, and in resume mode the worktree already
      // exists — never a new worktree in either.
      useWorktree:
        duplicateFrom || worktreePath
          ? false
          : (forced ?? localStorage.getItem('hive.worktree') === '1'),
      duplicateFrom,
      duplicateCwd: opts?.duplicateCwd || '',
      worktreePath,
      continueConversation: !!opts?.continueConversation,
    },
  });
}

export function closeLauncher() {
  // Idempotent: an outside click on a focusable element closes twice —
  // once from focusout at mousedown, once from the document click
  // handler — and running refocusActiveTerm() twice is pointless work
  // against the terminal.
  if (!isModalOpen('launcher')) return;
  // Blur first: refocusActiveTerm() bails when activeElement is an
  // INPUT (lib/focus.ts), and hiding the launcher via CSS does not
  // synchronously move focus out of it in a real engine.
  //
  // Whatever holds focus, not just the filter box — adding one focusable
  // control to the popup later must not quietly resurrect the bug this
  // was written for (terminal never refocused, because activeElement was
  // still an <input> the launcher owned).
  releaseFocus(launcherEl);
  // flushSync, because the order matters and React would not otherwise
  // keep it: this runs from a plain window listener, so an ordinary
  // store write is flushed in a later microtask — the popup would still
  // be visible (and `.hidden` still off #launcher) when
  // refocusActiveTerm() ran, and app/focus.ts refuses to touch the
  // terminal while a modal is open. The keyboard would land nowhere.
  flushSync(() => closeModal('launcher'));
  deps.refocusActiveTerm();
}

export function duplicateActiveSession() {
  const s = appData().sessions.find((x) => x.id === appData().activeId);
  if (!s) return;
  const cwd = resolveSessionCwd(s);
  if (!cwd) {
    flashStatus('cannot duplicate: source session has no cwd', true);
    return;
  }
  const pid = s.projectId ?? s.project_id ?? '';
  if (s.agent) bumpAgentUsage(s.agent);
  DuplicateSession(s.agent || '', pid, cwd, s.id).catch(
    reportFailure('duplicate session'),
  );
}

export function restartActiveSession() {
  const s = appData().sessions.find((x) => x.id === appData().activeId);
  if (!s) {
    flashStatus('no active session to restart', true);
    return;
  }
  RestartSession(s.id).catch(reportFailure('restart'));
}

export function duplicateActiveSessionChooseTool() {
  const s = appData().sessions.find((x) => x.id === appData().activeId);
  if (!s) return;
  const cwd = resolveSessionCwd(s);
  if (!cwd) {
    flashStatus('cannot duplicate: source session has no cwd', true);
    return;
  }
  const pid = s.projectId ?? s.project_id ?? '';
  openLauncher(pid, { duplicateFrom: s, duplicateCwd: cwd });
}

export function initLauncher(injected: LauncherDeps) {
  deps = injected;
}
