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

// dropTargetIndex converts a drag-and-drop landing — "above" or "below" a
// target row — into the same global index reorderTarget returns, for the same
// reason: registry.moveInOrder deletes the id then inserts at that index into
// the daemon's ONE flat r.order list spanning every project.
//
// Where reorderTarget can name a sibling's `.order` directly (adjacent swap),
// a drop can land between any two rows, so this one resolves the neighbour
// that will sit at the drop slot and then compensates for the delete.
//
// The critical detail: the slot is an index into the sibling list with the
// dragged session ALREADY REMOVED. Computing it against the list that still
// contains the dragged row shifts every index by one whenever the dragged row
// sits before the target, and the drop lands one slot too low — which is the
// bug this function replaced (spec 305).
//
// `sessions` may be in any order; it is sorted by `.order` internally.
// Returns null when there is nothing to do (unknown target, drop onto self, or
// a slot the session already occupies).
export function dropTargetIndex(
  sessions: {
    id: string;
    projectId?: string;
    project_id?: string;
    order?: number;
  }[],
  draggedID: string,
  targetID: string,
  above: boolean,
): number | null {
  if (draggedID === targetID) return null;
  const globalOrdered = [...sessions].sort(
    (a, b) => (a.order ?? 0) - (b.order ?? 0),
  );
  const draggedIdx = globalOrdered.findIndex((s) => s.id === draggedID);
  const target = globalOrdered.find((s) => s.id === targetID);
  if (draggedIdx < 0 || !target) return null;

  const pid = target.projectId ?? target.project_id ?? '';
  // Siblings WITHOUT the dragged session — the list the drop slot indexes.
  const sibs = globalOrdered.filter(
    (s) => (s.projectId ?? s.project_id ?? '') === pid && s.id !== draggedID,
  );
  const targetIdx = sibs.findIndex((s) => s.id === targetID);
  if (targetIdx < 0) return null;
  const slot = above ? targetIdx : targetIdx + 1;

  // Translate the project-relative slot into a global index: the global
  // position of whichever sibling will be pushed down by the insert, or one
  // past the last sibling when the drop lands at the end of the project.
  let globalIdx =
    slot >= sibs.length
      ? globalOrdered.findIndex((s) => s.id === sibs[sibs.length - 1].id) + 1
      : globalOrdered.findIndex((s) => s.id === sibs[slot].id);
  if (globalIdx < 0) return null;
  // moveInOrder deletes before inserting, so an index after the dragged
  // session's current position shifts left by one.
  if (draggedIdx < globalIdx) globalIdx -= 1;
  return globalIdx === draggedIdx ? null : globalIdx;
}
