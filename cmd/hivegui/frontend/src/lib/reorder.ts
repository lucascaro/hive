// reorderTarget converts "move the active session one slot up/down inside its
// own project" into the index the daemon wants.
//
// The daemon's session order (internal/registry: r.order) is ONE global list
// spanning every project, and registry.moveInOrder deletes the id then inserts
// at the given index. So the request for "swap with the adjacent sibling" is
// that sibling's CURRENT index in r.order, which stays correct after the
// delete-then-insert in both directions and in both wrap cases.
//
// That index is the sibling's own `order` field — NOT its position in
// `ordered`. r.order is a flat creation-order list that is not grouped by
// project, while orderedSessions() groups by project first, so the two agree
// only when sessions happen to have been created project-by-project. Creating
// sessions alternately across projects makes every display position wrong, and
// the resulting move silently lands in the wrong place or does nothing.
// (app/sidebar.ts's drag-to-reorder path resolves the same conversion off
// `.order` for the same reason — keep the two consistent.)
//
// `ordered` must be the display order (app/selectors.ts orderedSessions()):
// sorted by project, then by session order. Only *adjacency* is read from it;
// the returned index comes from the target's own `order`. Wrapping is within
// the project — the last session in a project moves to the top of that same
// project. Returns null when there is nothing to do (unknown active session, a
// project with fewer than two sessions, or a target with no `order` value).
export function reorderTarget(
  ordered: {
    id: string;
    projectId?: string;
    project_id?: string;
    order?: number;
  }[],
  activeId: string | null,
  delta: number,
): number | null {
  if (!activeId || delta === 0) return null;
  const cur = ordered.find((s) => s.id === activeId);
  if (!cur) return null;
  const pid = cur.projectId ?? cur.project_id ?? '';
  const sibs = ordered.filter(
    (s) => (s.projectId ?? s.project_id ?? '') === pid,
  );
  if (sibs.length < 2) return null;
  const sIdx = sibs.findIndex((s) => s.id === activeId);
  const tgt = sibs[(sIdx + delta + sibs.length) % sibs.length];
  if (tgt.id === activeId) return null;
  return tgt.order ?? null;
}
