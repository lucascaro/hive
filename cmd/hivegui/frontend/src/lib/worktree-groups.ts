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
// membership is already visible without a per-group palette.
//
// THE ONE ORDER. Grouping introduces a second candidate order — `.order`
// (the daemon's flat r.order) and the clustered order rows actually paint in
// — and the first cut of this file kept both, resolving drop slots in
// `.order` space while the user dragged in painted space. Every drag
// involving a group then landed a slot off, or did nothing at all. So:
// `clusterSessions()` IS the order. `app/selectors.ts` clusters, the sidebar
// paints what it is handed, and every reorder below computes its target as a
// painted ARRAY and derives the moves that make the daemon's list equal it.
// No index arithmetic in two spaces, because that was the bug.
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
// It is also idempotent — clustering an already-clustered list changes
// nothing — which is what lets a reorder simply make r.order equal the
// painted array and know the next paint will agree.
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
// registry: moveInOrder), replicated so the ops below can be simulated as
// they are generated. Simulating is not optional: each UpdateSession is a
// separate round-trip that re-broadcasts, so the ops must be computed up
// front against a predicted list rather than recomputed from state arriving
// between calls.
function moveInOrder(order: string[], id: string, newOrder: number): string[] {
  const cur = order.indexOf(id);
  if (cur < 0) return order;
  const out = order.slice();
  out.splice(cur, 1);
  const at = Math.min(Math.max(newOrder, 0), out.length);
  out.splice(at, 0, id);
  return out;
}

// opsToReach returns the moves that turn `current` into `target` — the same
// id set in two orders.
//
// It walks left to right and fixes the first slot that disagrees, which is
// what makes it obviously correct: moveInOrder deletes from a position at or
// after the slot being fixed and re-inserts AT it, so the already-agreeing
// prefix never shifts and each op settles one more slot permanently. That
// beats deriving an index in the daemon's space by hand, which is precisely
// where the spec-305 off-by-one lived.
function opsToReach(current: string[], target: string[]): ReorderOp[] {
  const ops: ReorderOp[] = [];
  let now = current.slice();
  for (let i = 0; i < target.length; i++) {
    if (now[i] === target[i]) continue;
    ops.push({ id: target[i], order: i });
    now = moveInOrder(now, target[i], i);
  }
  return ops;
}

// globalTarget splices one project's painted ids back into the global list at
// the positions that project already occupies, leaving every other project
// untouched. The daemon's r.order is one flat list spanning all projects, and
// a reorder is only ever within a project.
function globalTarget(
  globalSorted: OrderedSession[],
  pid: string,
  projectPainted: string[],
): string[] {
  let n = 0;
  return globalSorted.map((s) =>
    readProjectId(s) === pid ? projectPainted[n++] : s.id,
  );
}

// The painted rows of one project, plus the groups within it.
function paintedProject(sessions: OrderedSession[], pid: string) {
  const painted = clusterSessions(
    sessions.filter((s) => readProjectId(s) === pid),
  );
  return {
    painted,
    ids: painted.map((s) => s.id),
    groups: worktreeGroups(painted),
  };
}

// paintedUnits splits one project's painted rows into the blocks a reorder
// can move: each shared-worktree group is ONE unit, every other session is a
// unit of its own.
//
// This is the slot space. Insertion positions run between units, never inside
// one, because a position inside a group is not a position the list can hold
// — clusterSessions pulls the group back together on the next paint, so a
// move that resolved there produced no ops and the gesture died silently.
function paintedUnits(
  ids: string[],
  groups: Map<string, string[]>,
): string[][] {
  const memberOf = new Map<string, string[]>();
  for (const members of groups.values()) {
    for (const id of members) memberOf.set(id, members);
  }
  const units: string[][] = [];
  const seen = new Set<string>();
  for (const id of ids) {
    if (seen.has(id)) continue;
    const unit = memberOf.get(id) ?? [id];
    for (const m of unit) seen.add(m);
    units.push(unit);
  }
  return units;
}

// The block a drag on `id` moves: its whole shared-worktree group, or just
// itself.
function moversFor(
  painted: OrderedSession[],
  groups: Map<string, string[]>,
  id: string,
): string[] {
  const s = painted.find((x) => x.id === id);
  if (!s) return [];
  return groups.get(worktreeKey(s)) ?? [id];
}

// clusterDropOps turns a drop onto `targetID` into the moves that place the
// dragged session at the drop slot.
//
// Two gestures, told apart by where the drop lands — which is the only
// signal a drag carries, and reads the way the rows look:
//
//   • onto a row OUTSIDE the dragged session's group → the whole group
//     moves, staying contiguous. Dragging one member somewhere else while
//     its group stayed put would be meaningless: the next paint pulls it
//     straight back beside them.
//   • onto another member of its OWN group → the group stays where it is and
//     the two members swap places inside it. The block is contiguous, so
//     there is nowhere else such a drop could mean.
//
// The slot is resolved in PAINTED space, because that is the space the user
// dropped in: `above` means the painted row above, not anything in `.order`.
//
// It returns ops rather than performing them so the caller can issue them in
// order and stop on the first failure; a half-applied cluster is the one way
// this feature can visibly go wrong.
export function clusterDropOps(
  sessions: OrderedSession[],
  draggedID: string,
  targetID: string,
  above: boolean,
): ReorderOp[] {
  if (draggedID === targetID) return [];
  const globalSorted = sessions
    .slice()
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  const dragged = globalSorted.find((s) => s.id === draggedID);
  const target = globalSorted.find((s) => s.id === targetID);
  if (!dragged || !target) return [];

  const pid = readProjectId(target);
  if (readProjectId(dragged) !== pid) return [];

  const { painted, ids, groups } = paintedProject(globalSorted, pid);
  const movers = moversFor(painted, groups, draggedID);
  if (movers.length === 0) return [];

  // Within the group: move the one session, leave the block where it is.
  // Because the block is contiguous in the painted list, moving a member
  // relative to another member cannot take it out of the block.
  if (movers.includes(targetID)) {
    const rest = ids.filter((id) => id !== draggedID);
    const at = rest.indexOf(targetID);
    if (at < 0) return [];
    const slot = above ? at : at + 1;
    const wanted = [...rest.slice(0, slot), draggedID, ...rest.slice(slot)];
    return opsToReach(
      globalSorted.map((s) => s.id),
      globalTarget(globalSorted, pid, wanted),
    );
  }

  // Outside the group: the slots are BETWEEN BLOCKS, never inside one. A
  // slot in a group's interior is not a position the rows can take — the
  // next paint pulls the group back together — so a move that resolved to
  // one silently did nothing at all. Dropping on a group you are not in
  // therefore lands above or below the whole of it.
  const units = paintedUnits(ids, groups);
  const rest = units.filter((u) => !u.includes(draggedID));
  const targetUnit = rest.findIndex((u) => u.includes(targetID));
  if (targetUnit < 0) return [];
  const slot = above ? targetUnit : targetUnit + 1;

  const wanted = [...rest.slice(0, slot), movers, ...rest.slice(slot)].flat();
  return opsToReach(
    globalSorted.map((s) => s.id),
    globalTarget(globalSorted, pid, wanted),
  );
}

// clusterReorderOps is the keyboard half of the same idea: ⇧⌘↑/⇧⌘↓ moves the
// active session one painted slot up or down inside its project, wrapping at
// the ends.
//
// For a session in a shared worktree the key does two jobs, in this order:
//
//   1. While the session has somewhere to go INSIDE its group, it moves
//      there — one place up or down among its own members.
//   2. Once it is at the group's edge and the next press would take it out,
//      the WHOLE group moves instead. A member cannot leave: membership is
//      which worktree it runs in, not where it sits.
//
// Escalating at the edge is what keeps the key from ever being a dead press,
// and it is reachable: to move the group, walk the member to the end first.
// The mouse tells the two apart by where you drop instead (clusterDropOps).
//
// It replaces reorderTarget, which returned a single `.order` index and so
// could not express "move this block of rows". For a session in no group it
// behaves exactly as that did: one row at a time, wrapping within the
// project.
export function clusterReorderOps(
  sessions: OrderedSession[],
  activeID: string | null,
  delta: number,
): ReorderOp[] {
  if (!activeID || delta === 0) return [];
  const globalSorted = sessions
    .slice()
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  const active = globalSorted.find((s) => s.id === activeID);
  if (!active) return [];
  const pid = readProjectId(active);

  const { painted, ids, groups } = paintedProject(globalSorted, pid);
  const movers = moversFor(painted, groups, activeID);
  if (movers.length === 0) return [];

  // Step 1: move within the group while there is room.
  const within = movers.indexOf(activeID);
  const step = Math.sign(delta);
  if (
    movers.length > 1 &&
    within + step >= 0 &&
    within + step < movers.length
  ) {
    const rest = ids.filter((id) => id !== activeID);
    const neighbour = movers[within + step];
    const at = rest.indexOf(neighbour);
    if (at < 0) return [];
    const slot = step < 0 ? at : at + 1;
    const wanted = [...rest.slice(0, slot), activeID, ...rest.slice(slot)];
    return opsToReach(
      globalSorted.map((s) => s.id),
      globalTarget(globalSorted, pid, wanted),
    );
  }

  // Step 2: at the group's edge — the whole block moves, one BLOCK at a
  // time. Counting sibling rows instead of blocks is what made this a dead
  // press: a row-slot that lands inside another group is not a position the
  // list can hold (the next paint undoes it), so the move resolved to no ops
  // and the key did nothing, however many times it was pressed.
  const units = paintedUnits(ids, groups);
  const cur = units.findIndex((u) => u.includes(activeID));
  if (cur < 0) return [];
  const rest = units.filter((_, i) => i !== cur);
  if (rest.length === 0) return []; // the project is one group; nowhere to go

  // Insertion slots run 0…rest.length, so a block at the bottom wraps to the
  // top of its own project — the same wrap the single-row version had.
  const slots = rest.length + 1;
  const next = (((cur + delta) % slots) + slots) % slots;
  if (next === cur) return [];

  const wanted = [...rest.slice(0, next), movers, ...rest.slice(next)].flat();
  return opsToReach(
    globalSorted.map((s) => s.id),
    globalTarget(globalSorted, pid, wanted),
  );
}
