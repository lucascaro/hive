// Wire-payload helpers. The hived daemon emits snake_case JSON on
// the unix socket; older code paths in the GUI sometimes used
// camelCase.

// readProjectId tolerates both snake_case and camelCase on session
// objects already in flight — many code paths in main.js do this
// inline; this is the canonical helper.
export function readProjectId(
  session: { projectId?: string; project_id?: string } | null | undefined,
): string {
  return session?.projectId ?? session?.project_id ?? '';
}
