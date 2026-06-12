// Empty-state model for the terminals pane. Pure — main.js renders
// whatever this returns, tests assert on the model.
//
// Returns null when at least one session is visible in the current
// scope, otherwise { kind, title, hint, actions } where actions is a
// list of { id, label } the renderer turns into real buttons.

export function emptyStateModel({
  projects = [],
  sessions = [],
  view = 'single',
  currentProjectId = '',
  gridProjectId = '',
  minimized = new Set(),
  isMac = true,
} = {}) {
  const mod = isMac ? '⌘' : 'Ctrl+';

  if (sessions.length === 0) {
    return {
      kind: 'first-run',
      title: 'No sessions yet',
      hint: `Press ${mod}T to launch an agent${projects.length === 0 ? `, or ${mod}N to create a project` : ''}.`,
      actions: [
        { id: 'new-session', label: `New session (${mod}T)` },
        ...(projects.length === 0 ? [{ id: 'new-project', label: `New project (${mod}N)` }] : []),
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
      scope = sessions.filter((s) => (s.projectId ?? s.project_id) === currentProjectId);
    }
  }

  if (scope.length === 0) {
    return {
      kind: 'project-empty',
      title: 'No sessions in this project',
      hint: `${mod}T launches an agent here.`,
      actions: [{ id: 'new-session', label: `New session (${mod}T)` }],
    };
  }

  if (view !== 'single' && scope.every((s) => minimized.has(s.id))) {
    return {
      kind: 'all-minimized',
      title: 'All sessions minimized',
      hint: 'Restore one from the tray below.',
      actions: [],
    };
  }

  return null;
}
