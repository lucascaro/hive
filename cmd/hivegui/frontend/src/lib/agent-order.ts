// Launcher agent ordering: which agents the new-session launcher lists, and
// in what order. The rule is pure and table-testable; load/save at the
// bottom are the only storage-touching part. The launch counts it sorts by
// live in app/modals/launcher.ts.
//
// The rule, in three bands:
//   1. hidden agents are dropped,
//   2. pinned agents come first, in the user's pinned order,
//   3. everything else follows by launch count (most-used first), ties in
//      catalog order — the launcher's original, automatic ordering.
//
// Only deliberate choices are stored: a fresh profile has no prefs, so
// band 3 is the whole list and the launcher behaves exactly as before.

export interface AgentPrefs {
  hidden: string[];
  // Ordered: the array IS the manual order.
  pinned: string[];
}

export const EMPTY_AGENT_PREFS: AgentPrefs = { hidden: [], pinned: [] };

// parseAgentPrefs reads stored JSON defensively: anything that is not the
// expected shape degrades to "no prefs" rather than hiding agents on the
// strength of a corrupt value.
export function parseAgentPrefs(raw: string | null): AgentPrefs {
  if (!raw) return EMPTY_AGENT_PREFS;
  let v: unknown;
  try {
    v = JSON.parse(raw);
  } catch {
    return EMPTY_AGENT_PREFS;
  }
  if (!v || typeof v !== 'object') return EMPTY_AGENT_PREFS;
  const ids = (x: unknown): string[] =>
    Array.isArray(x) ? x.filter((s): s is string => typeof s === 'string') : [];
  const o = v as Record<string, unknown>;
  return { hidden: ids(o.hidden), pinned: ids(o.pinned) };
}

export function orderAgents<T extends { id: string }>(
  list: readonly T[],
  usage: Readonly<Record<string, number>>,
  prefs: AgentPrefs,
): T[] {
  const hidden = new Set(prefs.hidden);
  const visible = list.filter((a) => !hidden.has(a.id));
  const byId = new Map(visible.map((a) => [a.id, a]));
  // Unknown ids (a deleted custom agent) are skipped, not errors.
  const pinned = prefs.pinned
    .map((id) => byId.get(id))
    .filter((a): a is T => a !== undefined);
  const pinnedIds = new Set(pinned.map((a) => a.id));
  const rest = visible
    .filter((a) => !pinnedIds.has(a.id))
    .map((a, i) => ({ a, i }))
    .sort((x, y) => {
      const ux = usage[x.a.id] || 0;
      const uy = usage[y.a.id] || 0;
      if (ux !== uy) return uy - ux;
      return x.i - y.i;
    })
    .map((e) => e.a);
  return [...pinned, ...rest];
}

// moveIndex converts an above/below drop on the row at targetIdx into the
// index the dragged row ends up at once it is removed and re-inserted.
// Dragging downward compensates for the removal shifting the target up.
// Returns draggedIdx itself when the drop is a no-op.
export function moveIndex(
  draggedIdx: number,
  targetIdx: number,
  above: boolean,
): number {
  const to = above ? targetIdx : targetIdx + 1;
  return draggedIdx < to ? to - 1 : to;
}

// moveItem returns a copy of `list` with the item at `from` moved to `to`.
export function moveItem<T>(list: readonly T[], from: number, to: number): T[] {
  const out = [...list];
  const [item] = out.splice(from, 1);
  out.splice(to, 0, item);
  return out;
}

// Stored by Settings → Agents, read by the launcher on every open.
const AGENT_PREFS_KEY = 'hive.agentPrefs';

export function loadAgentPrefs(): AgentPrefs {
  try {
    return parseAgentPrefs(localStorage.getItem(AGENT_PREFS_KEY));
  } catch {
    return EMPTY_AGENT_PREFS;
  }
}

export function saveAgentPrefs(prefs: AgentPrefs) {
  try {
    localStorage.setItem(AGENT_PREFS_KEY, JSON.stringify(prefs));
  } catch {
    /* Denied storage: not saved, so the launcher (which re-reads storage on
       every open) keeps its previous order. Nothing to recover here. */
  }
}
