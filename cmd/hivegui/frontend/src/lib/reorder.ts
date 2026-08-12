// reorderTarget converts "move the active session one slot up/down inside its
// own project" into the index the daemon wants.
//
// The daemon's session order (internal/registry: r.order) is ONE global list
// spanning every project; Entry.Order is an index into it, and
// registry.moveInOrder deletes the id then inserts at the given index. So the
// correct request for "swap with the adjacent sibling" is that sibling's
// CURRENT index in the global ordered list — which stays correct after the
// delete-then-insert in both directions and in both wrap cases.
//
// `ordered` must be the display order (app/selectors.ts orderedSessions()):
// sorted by project, then by session order. Wrapping is within the project —
// the last session in a project moves to the top of that same project.
// Returns null when there is nothing to do (unknown active session, or a
// project with fewer than two sessions).
export function reorderTarget(
  ordered: { id: string; projectId?: string; project_id?: string }[],
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
  return ordered.findIndex((s) => s.id === tgt.id);
}
