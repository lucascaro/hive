// Applies a reorder as a sequence of daemon moves.
//
// A reorder is no longer one call. Sessions sharing a worktree move as a
// block, so lib/worktree-groups.ts hands back a LIST of moves whose indices
// were computed against a simulated list — which means they have to be
// applied in order, and only one sequence may be in flight at a time.
//
// Without the guard, holding ⇧⌘↓ (key auto-repeat) starts a second sequence
// against state the first has not finished writing, and the two interleave
// into an order neither press asked for. The drag path can do the same with
// a fast second drop.
//
// Concurrent presses are DROPPED, not queued: a queue would replay a move
// computed against an order the user can no longer see, and the sequences
// are short enough that a dropped repeat just means pressing again. The
// sequence also stops at the first failure — half a moved block is bad, but
// continuing to write after the daemon refused is worse.
import { UpdateSession } from '../bridge.js';
import { reportFailure } from './dom.js';

export interface ReorderOp {
  id: string;
  order: number;
}

let inFlight = false;

// Exported for tests: a rejected op leaves the flag set until the promise
// settles, and a test that asserts on the second press needs to know when
// that happened.
export function reorderInFlight(): boolean {
  return inFlight;
}

export async function runReorder(ops: ReorderOp[]): Promise<void> {
  if (inFlight || ops.length === 0) return;
  inFlight = true;
  try {
    for (const op of ops) {
      try {
        await UpdateSession(op.id, '', '', op.order);
      } catch (err) {
        reportFailure('reorder')(err);
        return;
      }
    }
  } finally {
    inFlight = false;
  }
}
