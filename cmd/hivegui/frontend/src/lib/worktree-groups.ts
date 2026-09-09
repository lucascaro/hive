// Sessions that share one git worktree share one working directory: they
// step on each other's files, and killing one keeps the worktree alive until
// the last of them goes (internal/registry: the worktreeShared guard in
// kill()). The sidebar has to say so.
//
// Everything here is derived from `worktree_path`, which is already on every
// SessionInfo the daemon broadcasts (internal/wire: SessionInfo). There is no
// "shared" field on the wire and there should not be one — the daemon's own
// notion of shared is a transient bool recomputed inside kill(), not session
// identity, and a duplicate on the wire would need syncing on every create,
// kill and rename for no behavioural gain.
//
// The colour of the group is the session colour: a session that adopts a
// sibling's worktree inherits its colour (internal/registry/create.go), so
// membership is already visible without a per-group palette. This file only
// decides WHO is in a group, WHERE the group paints, and what a drag does.
import { readProjectId } from './wire.js';

interface OrderedSession {
  id: string;
  projectId?: string;
  project_id?: string;
  order?: number;
  worktreePath?: string;
  worktree_path?: string;
}

// worktreeKey is the grouping key: a session's worktree directory, or '' for
// a session with no worktree. Sessions sharing a plain project cwd are NOT
// grouped — that is the common case (every session in a project), and marking
// it would make the cue noise.
export function worktreeKey(s: {
  worktreePath?: string;
  worktree_path?: string;
}): string {
  return s.worktreePath ?? s.worktree_path ?? '';
}

// worktreeGroups maps worktree path -> member ids, for the paths occupied by
// two or more of the given sessions. Callers pass ONE project's sessions:
// worktree paths are project-scoped, and a cross-project group could not
// paint adjacently anyway (rows live in per-project <ul>s).
//
// Member ids come back in ascending `.order`, which is what makes the anchor
// in clusterSessions() well-defined.
export function worktreeGroups(
  sessions: OrderedSession[],
): Map<string, string[]> {
  const byKey = new Map<string, OrderedSession[]>();
  for (const s of sessions) {
    const key = worktreeKey(s);
    if (!key) continue;
    const bucket = byKey.get(key);
    if (bucket) bucket.push(s);
    else byKey.set(key, [s]);
  }
  const out = new Map<string, string[]>();
  for (const [key, members] of byKey) {
    if (members.length < 2) continue;
    out.set(
      key,
      members
        .slice()
        .sort((a, b) => (a.order ?? 0) - (b.order ?? 0))
        .map((s) => s.id),
    );
  }
  return out;
}

// clusterSessions returns the paint order for one project: `.order`, with
// each shared-worktree group pulled together at the position of its
// lowest-order member.
//
// Anchoring at the lowest-order member (rather than, say, the most recently
// touched one) is what makes this stable: the anchor only moves when that
// member's own order moves, so a re-render never reshuffles rows on its own.
// It is also why the drag path has to move whole clusters — moving a
// non-anchor member alone changes nothing the eye can see, since this
// function puts it straight back next to its group.
export function clusterSessions<T extends OrderedSession>(sessions: T[]): T[] {
  const sorted = sessions
    .slice()
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  const groups = worktreeGroups(sorted);
  if (groups.size === 0) return sorted;

  const byId = new Map(sorted.map((s) => [s.id, s]));
  const emitted = new Set<string>();
  const out: T[] = [];
  for (const s of sorted) {
    if (emitted.has(s.id)) continue;
    const members = groups.get(worktreeKey(s));
    if (!members) {
      out.push(s);
      emitted.add(s.id);
      continue;
    }
    // First member reached in `.order` is the anchor; the whole group
    // paints here, in member order.
    for (const id of members) {
      const m = byId.get(id);
      if (!m || emitted.has(id)) continue;
      out.push(m);
      emitted.add(id);
    }
  }
  return out;
}

export interface ReorderOp {
  id: string;
  order: number;
}

// moveInOrder is the daemon's delete-then-clamp-then-insert (internal/
// registry: moveInOrder), replicated so clusterDropOps can simulate the
// sequence of moves it is about to request. Simulating is not optional: each
// UpdateSession is a separate round-trip that re-broadcasts, so the ops must
// be computed up front against a predicted list rather than recomputed from
// state arriving between calls.
function moveInOrder(order: string[], id: string, newOrder: number): string[] {
  const cur = order.indexOf(id);
  if (cur < 0) return order;
  const out = order.slice();
  out.splice(cur, 1);
  const at = Math.min(Math.max(newOrder, 0), out.length);
  out.splice(at, 0, id);
  return out;
}

// clusterDropOps turns a drop onto `targetID` into the moves that place the
// dragged session — and, when it belongs to a shared-worktree group, its
// whole group — at the drop slot, contiguously.
//
// It returns ops rather than performing them so the caller can issue them in
// order and stop on the first failure; a half-applied cluster is the one way
// this feature can visibly go wrong.
//
// The drop slot is resolved against the sibling list with EVERY mover
// removed — otherwise a group's own trailing members shift the target and the
// block lands beside itself. Each individual move index is then read from the
// list with just that mover removed, which is what the daemon actually
// splices into.
export function clusterDropOps(
  sessions: OrderedSession[],
  draggedID: string,
  targetID: string,
  above: boolean,
): ReorderOp[] {
  if (draggedID === targetID) return [];
  const globalOrdered = sessions
    .slice()
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  const dragged = globalOrdered.find((s) => s.id === draggedID);
  const target = globalOrdered.find((s) => s.id === targetID);
  if (!dragged || !target) return [];

  const pid = readProjectId(target);
  if (readProjectId(dragged) !== pid) return [];

  const groups = worktreeGroups(
    globalOrdered.filter((s) => readProjectId(s) === pid),
  );
  const movers = groups.get(worktreeKey(dragged)) ?? [draggedID];
  // Dropping a group onto one of its own members is a no-op, not a
  // rearrangement of the group's interior.
  if (movers.includes(targetID)) return [];

  const moving = new Set(movers);
  const sibs = globalOrdered.filter(
    (s) => readProjectId(s) === pid && !moving.has(s.id),
  );
  const targetIdx = sibs.findIndex((s) => s.id === targetID);
  if (targetIdx < 0) return [];
  const slot = above ? targetIdx : targetIdx + 1;
  // The sibling the group lands in front of, or the last one it lands after.
  const anchorID =
    slot >= sibs.length ? sibs[sibs.length - 1]?.id : sibs[slot]?.id;
  if (!anchorID) return [];

  // Where the block ends up, expressed against the list with every mover
  // removed. Comparing that projection with the current list is what lets a
  // drop that changes nothing cost nothing.
  const ids = globalOrdered.map((s) => s.id);
  const remaining = ids.filter((x) => !moving.has(x));
  const at = remaining.indexOf(anchorID) + (slot >= sibs.length ? 1 : 0);
  const desired = [
    ...remaining.slice(0, at),
    ...movers,
    ...remaining.slice(at),
  ];
  if (desired.every((id, i) => ids[i] === id)) return [];

  // The head moves first. Its index is read from the list with the head —
  // and only the head — removed, because that is the list the daemon splices
  // into: moveInOrder deletes one id, not the whole group. The other movers
  // are still in place at this point and are pulled in afterwards.
  const ops: ReorderOp[] = [];
  let now = ids;
  const head = movers[0];
  const withoutHead = now.filter((x) => x !== head);
  const headAt = withoutHead.indexOf(anchorID) + (slot >= sibs.length ? 1 : 0);
  if (headAt >= 0 && now[headAt] !== head) {
    ops.push({ id: head, order: headAt });
    now = moveInOrder(now, head, headAt);
  }

  // Then each remaining member is pulled in directly behind its predecessor,
  // its index likewise read from the list with that member already removed.
  // Computing it before the delete lands the member one slot too far
  // whenever it currently sits above where it is going — the spec-305
  // off-by-one, in its cluster-shaped form.
  for (let i = 1; i < movers.length; i++) {
    const id = movers[i];
    const without = now.filter((x) => x !== id);
    const prevIdx = without.indexOf(movers[i - 1]);
    if (prevIdx < 0) continue;
    const want = prevIdx + 1;
    if (now[want] === id) continue; // already in place; no round-trip
    ops.push({ id, order: want });
    now = moveInOrder(now, id, want);
  }
  return ops;
}
