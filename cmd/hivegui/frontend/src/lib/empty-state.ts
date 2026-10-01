// Empty-state model for the terminals pane. Pure — components/EmptyState.tsx renders
// whatever this returns, tests assert on the model.
//
// Returns null when at least one session is visible in the current
// scope, otherwise { kind, title, hint, actions } where actions is a
// list of { id, label } the renderer turns into real buttons.

import { EMPTY_KEYMAP, labelIn, type Keymap } from './bindings.js';
import { withKey } from './chord-label.js';

// Only `.length` is read off projects, so its element type is irrelevant.
export interface EmptyStateInput {
  projects?: readonly unknown[];
  sessions?: readonly { id: string; projectId?: string; project_id?: string }[];
  view?: string;
  currentProjectId?: string;
  gridProjectId?: string;
  minimized?: { has(id: string): boolean };
  isMac?: boolean;
  /** The user's keymap (spec 477); the shipped defaults when omitted. */
  keymap?: Keymap;
}

export interface EmptyState {
  kind: 'first-run' | 'project-empty' | 'all-minimized';
  title: string;
  hint: string;
  actions: { id: string; label: string }[];
}

export function emptyStateModel({
  projects = [],
  sessions = [],
  view = 'single',
  currentProjectId = '',
  gridProjectId = '',
  minimized = new Set<string>(),
  isMac = true,
  keymap = EMPTY_KEYMAP,
}: EmptyStateInput = {}): EmptyState | null {
  // The user's current keys, or none: a hint never names a key that
  // does nothing (spec 477).
  const newSession = labelIn(keymap, 'new-session', isMac);
  const newProject = labelIn(keymap, 'new-project', isMac);
  const sessionAction = {
    id: 'new-session',
    label: withKey('New session', newSession),
  };

  if (sessions.length === 0) {
    const launch = newSession
      ? `Press ${newSession} to launch an agent`
      : 'Start a new session to launch an agent';
    const create =
      projects.length === 0 && newProject
        ? `, or ${newProject} to create a project`
        : '';
    return {
      kind: 'first-run',
      title: 'No sessions yet',
      hint: `${launch}${create}.`,
      actions: [
        sessionAction,
        ...(projects.length === 0
          ? [{ id: 'new-project', label: withKey('New project', newProject) }]
          : []),
      ],
    };
  }

  // Scope: which sessions could be visible right now?
  let scope = sessions;
  if (view === 'grid-project') {
    const pid = gridProjectId || currentProjectId;
    scope = sessions.filter((s) => (s.projectId ?? s.project_id) === pid);
  } else if (view === 'single') {
    // Single mode always shows the active session when one exists;
    // an empty *project* still leaves the previous tile visible, so
    // only a truly empty current project with no active session needs
    // the nudge. Approximate: scope to the current project when set.
    if (currentProjectId) {
      scope = sessions.filter(
        (s) => (s.projectId ?? s.project_id) === currentProjectId,
      );
    }
  }

  if (scope.length === 0) {
    return {
      kind: 'project-empty',
      title: 'No sessions in this project',
      hint: newSession
        ? `${newSession} launches an agent here.`
        : 'Start a new session to launch an agent here.',
      actions: [sessionAction],
    };
  }

  if (view !== 'single' && scope.every((s) => minimized.has(s.id))) {
    return {
      kind: 'all-minimized',
      title: 'All sessions minimized',
      hint: 'Restore one from the session tray or the sidebar.',
      actions: [],
    };
  }

  return null;
}
