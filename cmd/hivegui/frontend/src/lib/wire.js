// Wire-payload normalizers. The hived daemon emits snake_case JSON
// on the unix socket; older code paths in the GUI sometimes used
// camelCase. These helpers translate snake → camel once at the seam
// so downstream code can use one shape.

export function normalizeSessionInfo(raw) {
  if (!raw) return raw;
  return {
    id: raw.id,
    name: raw.name,
    color: raw.color,
    order: raw.order ?? 0,
    created: raw.created,
    alive: raw.alive,
    agent: raw.agent,
    projectId: raw.projectId ?? raw.project_id ?? '',
    worktreePath: raw.worktreePath ?? raw.worktree_path ?? '',
    worktreeBranch: raw.worktreeBranch ?? raw.worktree_branch ?? '',
    lastError: raw.lastError ?? raw.last_error ?? '',
  };
}

export function normalizeProjectInfo(raw) {
  if (!raw) return raw;
  return {
    id: raw.id,
    name: raw.name,
    color: raw.color,
    cwd: raw.cwd ?? '',
    order: raw.order ?? 0,
    created: raw.created,
  };
}

// readProjectId tolerates both snake_case and camelCase on session
// objects already in flight — many code paths in main.js do this
// inline; this is the canonical helper.
export function readProjectId(session) {
  return session?.projectId ?? session?.project_id ?? '';
}

// readWorktreeBranch is the same trick for the tile-header glyph
// title — snake_case from the wire, camelCase from older code paths.
export function readWorktreeBranch(session) {
  return session?.worktreeBranch ?? session?.worktree_branch ?? '';
}
