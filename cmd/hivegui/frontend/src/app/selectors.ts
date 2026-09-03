// Read-only derivations over the shared state object. No DOM, no
// bridge calls — pure lookups modules can share without cycles.

import type { SessionInfo } from './state.js';
import { appStore } from '../store/store.js';

// Live read of the store. A function, not a destructured snapshot: this
// module runs inside event handlers and must never cache a slice across
// a store write.
const appData = () => appStore.getState();

// orderedSessions returns sessions sorted by (project order, session order)
// so navigation always matches what the user sees.
export function orderedSessions(): SessionInfo[] {
  const projOrder = new Map(appData().projects.map((p, i) => [p.id, i]));
  return [...appData().sessions].sort((a, b) => {
    const pa = projOrder.get(a.projectId ?? a.project_id ?? '') ?? 1e9;
    const pb = projOrder.get(b.projectId ?? b.project_id ?? '') ?? 1e9;
    if (pa !== pb) return pa - pb;
    return (a.order ?? 0) - (b.order ?? 0);
  });
}

// nextAttentionId returns the id of the next session with an unread
// bell, walking orderedSessions() cyclically from the active one, or
// null when nothing else is flagged. The active session is skipped
// explicitly — the full-circle walk ends back on it, and ⌘B must always
// move you somewhere new (a flag on the session you're already looking
// at is stale; setActive clears it on focus anyway).
export function nextAttentionId(): string | null {
  const ord = orderedSessions();
  const n = ord.length;
  if (n === 0) return null;
  const start = ord.findIndex((s) => s.id === appData().activeId); // -1 → start at 0
  for (let i = 1; i <= n; i++) {
    const s = ord[(start + i) % n];
    if (s.id !== appData().activeId && appData().attention.has(s.id))
      return s.id;
  }
  return null;
}

// activeCwd resolves the directory associated with the current
// view: a session's worktree (preferred), otherwise the owning
// project's cwd, otherwise the user's currently-selected project.
// Empty string means "let the Go side fall back to launchDir".
export function activeCwd(): string {
  const id = appData().activeId;
  const s = id ? appData().sessions.find((x) => x.id === id) : null;
  // Both spellings, like resolveSessionCwd below — a camelCase-only
  // session used to fall through to the project cwd here, so ⌘N landed
  // in the repo root instead of the worktree. Not delegating to
  // resolveSessionCwd: its project fallback skips activeProjectId().
  const wt = s?.worktree_path ?? s?.worktreePath;
  if (wt) return wt;
  const pid = (s?.projectId ?? s?.project_id) || activeProjectId();
  const p = pid ? appData().projects.find((x) => x.id === pid) : null;
  return p?.cwd ?? '';
}

export function activeProjectId(): string {
  // currentProjectId is the user's explicit "I'm here" — set by
  // ⌘[/], project-header click, switchTo (synced to session's
  // project), and project events. Empty projects work because they
  // can be the current project even with no active session.
  // The one destructured read in src/: three fields, all consumed in the
  // next four lines with no await and no store write between them, and
  // TS needs the local bindings to narrow `string | null` to `string` on
  // return. Everywhere else the rule in FRONTEND.md holds — call appData()
  // per read, never hold a snapshot across anything that can write.
  const { currentProjectId, view, gridProjectId } = appData();
  if (currentProjectId) {
    return currentProjectId;
  }
  if (view === 'grid-project' && gridProjectId) {
    return gridProjectId;
  }
  if (appData().activeId) {
    const s = appData().sessions.find((x) => x.id === appData().activeId);
    const pid = s?.projectId ?? s?.project_id;
    if (pid) return pid;
  }
  return appData().projects[0]?.id ?? '';
}

// resolveSessionCwd picks the directory a session is actually running
// in: its worktree path if any, otherwise the owning project's cwd.
// Used by ⌘P / ⇧⌘P to fork a session into the same directory.
//
// Wire payloads from the daemon use snake_case (see
// internal/wire/control.go), so prefer those and fall back to the
// camelCase variants for safety. Both spellings are read everywhere in
// this file; only the ordering varies, and it doesn't matter — no
// payload carries both (test/e2e/payload-shapes.spec.ts pins the two
// shapes separately).
export function resolveSessionCwd(
  sess: SessionInfo | null | undefined,
): string {
  if (!sess) return '';
  const wt = sess.worktree_path ?? sess.worktreePath;
  if (wt) return wt;
  const pid = sess.projectId ?? sess.project_id;
  const proj = appData().projects.find((p) => p.id === pid);
  return proj?.cwd ?? '';
}
