// ---------- ACP transcript (spec 496) ----------
//
// The wire shape of an ACP session's transcript (internal/wire/acp.go)
// and the pure fold of ACP_TRANSCRIPT messages into it. Pure, so the
// ordering rules are unit-tested without a store or a DOM.
//
// The daemon queues a GET_ACP_TRANSCRIPT snapshot on the same ordered
// stream as the deltas (Registry.SendAcpTranscript), so a client applies
// messages strictly in arrival order: a snapshot replaces everything,
// and a delta is applied on top of the last snapshot.

export interface AcpPlanEntry {
  content: string;
  priority?: string;
  status?: string;
}

export interface AcpItem {
  id: number;
  kind: 'user' | 'agent' | 'thought' | 'plan' | 'tool' | string;
  text?: string;
  origin?: string;
  tool_call_id?: string;
  title?: string;
  tool_kind?: string;
  status?: string;
  plan?: AcpPlanEntry[];
  append?: boolean;
}

export interface AcpPermissionOption {
  option_id: string;
  name: string;
  kind: string;
}

export interface AcpPermission {
  request_id: string;
  tool_call_id?: string;
  title?: string;
  options: AcpPermissionOption[];
}

export interface AcpTranscriptMsg {
  session_id: string;
  epoch: number;
  reset?: boolean;
  items?: AcpItem[];
  permission?: AcpPermission | null;
}

export interface AcpTranscript {
  epoch: number;
  items: readonly AcpItem[];
  permission: AcpPermission | null;
  // A snapshot has been applied and the deltas since are current.
  loaded: boolean;
}

export const emptyAcp = (): AcpTranscript => ({
  epoch: 0,
  items: [],
  permission: null,
  loaded: false,
});

/**
 * applyAcp folds one message into a transcript and returns the result,
 * or `t` itself when nothing changed.
 *
 * - A snapshot (reset) replaces everything.
 * - A delta before any snapshot is dropped: the snapshot, when it
 *   comes, is ordered after it and already holds it.
 * - A delta from another epoch means the daemon rebuilt the transcript
 *   (a new adapter process) and this copy is stale: it is marked not
 *   loaded so its owner fetches a fresh snapshot.
 * - Otherwise items are merged by id — `append` adds text to the end of
 *   the item, anything else replaces it — and the pending permission is
 *   overwritten, since every message carries it.
 */
export function applyAcp(
  t: AcpTranscript,
  msg: AcpTranscriptMsg,
): AcpTranscript {
  if (msg.reset) {
    return {
      epoch: msg.epoch,
      items: (msg.items ?? []).map((it) => ({ ...it, append: undefined })),
      permission: msg.permission ?? null,
      loaded: true,
    };
  }
  if (!t.loaded) return t;
  if (msg.epoch !== t.epoch) return { ...t, loaded: false };
  let items = t.items;
  const delta = msg.items ?? [];
  if (delta.length > 0) {
    const next = items.slice();
    // Index once per message, not per item: a delta usually touches the
    // newest item, but a tool update can reach back.
    const at = new Map<number, number>();
    for (const [i, it] of next.entries()) at.set(it.id, i);
    for (const it of delta) {
      const i = at.get(it.id);
      if (i === undefined) {
        at.set(it.id, next.length);
        next.push({ ...it, append: undefined });
      } else if (it.append) {
        next[i] = { ...next[i], text: (next[i].text ?? '') + (it.text ?? '') };
      } else {
        next[i] = { ...it, append: undefined };
      }
    }
    items = next;
  }
  const permission = msg.permission ?? null;
  if (items === t.items && samePermission(permission, t.permission)) return t;
  return { ...t, items, permission };
}

function samePermission(
  a: AcpPermission | null,
  b: AcpPermission | null,
): boolean {
  return a === b || (a !== null && b !== null && a.request_id === b.request_id);
}

/** isAllowOption reports whether a permission option grants the request. */
export function isAllowOption(o: AcpPermissionOption): boolean {
  return o.kind.startsWith('allow');
}
