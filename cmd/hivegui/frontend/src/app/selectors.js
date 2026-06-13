// Read-only derivations over the shared state object. No DOM, no
// bridge calls — pure lookups modules can share without cycles.

import { state } from './state.js';

// orderedSessions returns sessions sorted by (project order, session order)
// so navigation always matches what the user sees.
export function orderedSessions() {
  const projOrder = new Map(state.projects.map((p, i) => [p.id, i]));
  return [...state.sessions].sort((a, b) => {
    const pa = projOrder.get(a.projectId ?? a.project_id ?? '') ?? 1e9;
    const pb = projOrder.get(b.projectId ?? b.project_id ?? '') ?? 1e9;
    if (pa !== pb) return pa - pb;
    return (a.order ?? 0) - (b.order ?? 0);
  });
}

// activeCwd resolves the directory associated with the current
// view: a session's worktree (preferred), otherwise the owning
// project's cwd, otherwise the user's currently-selected project.
// Empty string means "let the Go side fall back to launchDir".
export function activeCwd() {
  const id = state.activeId;
  const s = id ? state.sessions.find((x) => x.id === id) : null;
  if (s?.worktree_path) return s.worktree_path;
  const pid = (s?.projectId ?? s?.project_id) || activeProjectId();
  const p = pid ? state.projects.find((x) => x.id === pid) : null;
  return p?.cwd ?? '';
}

export function activeProjectId() {
  // currentProjectId is the user's explicit "I'm here" — set by
  // ⌘[/], project-header click, switchTo (synced to session's
  // project), and project events. Empty projects work because they
  // can be the current project even with no active session.
  if (state.currentProjectId) {
    return state.currentProjectId;
  }
  if (state.view === 'grid-project' && state.gridProjectId) {
    return state.gridProjectId;
  }
  if (state.activeId) {
    const s = state.sessions.find((x) => x.id === state.activeId);
    const pid = s?.projectId ?? s?.project_id;
    if (pid) return pid;
  }
  return state.projects[0]?.id ?? '';
}

// resolveSessionCwd picks the directory a session is actually running
// in: its worktree path if any, otherwise the owning project's cwd.
// Used by ⌘P / ⇧⌘P to fork a session into the same directory.
//
// Wire payloads from the daemon use snake_case (see
// internal/wire/control.go), so prefer those and fall back to the
// camelCase variants for safety — this matches `s.projectId ??
// s.project_id` used elsewhere in this file.
export function resolveSessionCwd(sess) {
  if (!sess) return '';
  const wt = sess.worktree_path ?? sess.worktreePath;
  if (wt) return wt;
  const pid = sess.projectId ?? sess.project_id;
  const proj = state.projects.find((p) => p.id === pid);
  return proj?.cwd ?? '';
}
