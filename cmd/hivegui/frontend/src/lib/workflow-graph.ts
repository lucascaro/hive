// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Pure layout and labels for the native workflow graph. Edges never leave
// their body (docs/design-docs/workflow-engine.md, IR), so each body is a
// DAG and a ranked layout per body is enough.
//
// ponytail: no crossing minimisation. Add a barycentre pass if real
// workflows tangle; xyflow is the recorded fallback (exec plan 530).

import { type Body, type Node, condLabel } from './workflow-types.js';

export interface LayoutEdge {
  from: string;
  to: string;
  /** The edge's condition, as the reviewer reads it. */
  label?: string;
}

export interface LayoutBody {
  /** One row per rank, nodes in IR order within a rank. */
  rows: Node[][];
  edges: LayoutEdge[];
}

/** Rank 0 for entry nodes, otherwise 1 + the deepest predecessor. */
export function layoutBody(body: Body): LayoutBody {
  const preds = new Map<string, string[]>();
  for (const e of body.edges) {
    const list = preds.get(e.to) ?? [];
    list.push(e.from);
    preds.set(e.to, list);
  }
  const ids = new Set(body.nodes.map((n) => n.id));
  const rank = new Map<string, number>();
  const visiting = new Set<string>();
  const rankOf = (id: string): number => {
    const known = rank.get(id);
    if (known !== undefined) return known;
    // The validator rejects cycles; a malformed body must not hang the UI.
    if (visiting.has(id)) return 0;
    visiting.add(id);
    let r = 0;
    for (const p of preds.get(id) ?? []) {
      if (ids.has(p)) r = Math.max(r, rankOf(p) + 1);
    }
    visiting.delete(id);
    rank.set(id, r);
    return r;
  };
  const rows: Node[][] = [];
  for (const n of body.nodes) {
    const r = rankOf(n.id);
    while (rows.length <= r) rows.push([]);
    rows[r].push(n);
  }
  return {
    rows,
    edges: body.edges.map((e) => ({
      from: e.from,
      to: e.to,
      ...(e.when ? { label: condLabel(e.when) } : {}),
    })),
  };
}

/** The node's own segment: `fix/review` -> `review`. */
export const localId = (id: string): string =>
  id.slice(id.lastIndexOf('/') + 1);

// `build/fix/review.verdict` -> `review.verdict`.
function shortRef(ref: string): string {
  const dot = ref.lastIndexOf('.');
  return dot < 0 ? ref : `${localId(ref.slice(0, dot))}${ref.slice(dot)}`;
}

export function loopHeader(n: Extract<Node, { kind: 'loop' }>): string {
  return `${localId(n.id)} - at most ${n.max_iters} rounds until ${condLabel(n.until)}`;
}

export function mapHeader(n: Extract<Node, { kind: 'map' }>): string {
  return `${localId(n.id)} - for each ${shortRef(n.over)}, ${n.concurrency} at once`;
}

export function groupHeader(n: Extract<Node, { kind: 'group' }>): string {
  return localId(n.id);
}

/** Live loop suffix: "round 2 of 3". */
export const roundLabel = (iteration: number, max: number): string =>
  `round ${iteration} of ${max}`;

/** Live map suffix: "3 of 5 done". */
export const doneLabel = (done: number, count: number): string =>
  `${done} of ${count} done`;

// ---------- instance keys ----------
//
// `node_id` is the IR id (`work/do`); an instance key puts `[k]` after each
// loop (1-based iteration) and map (0-based element) on its path
// (`work[2]/do`, `fix[1]/review`).

const INDEX = /\[(\d+)\]/g;

/** The IR id of an instance key. */
export const nodeIdOfKey = (key: string): string => key.replace(INDEX, '');

/** The `[k]` a key carries for the container `containerId` (a prefix of the
 * key's node id), or null when it is not under that container. */
export function indexUnder(key: string, containerId: string): number | null {
  const last = containerId.slice(containerId.lastIndexOf('/') + 1);
  const parent = containerId.slice(0, containerId.length - last.length);
  const tail = new RegExp(
    `^${parent
      .split('/')
      .filter(Boolean)
      .map((s) => `${escapeRe(s)}(?:\\[\\d+\\])?/`)
      .join('')}${escapeRe(last)}\\[(\\d+)\\]`,
  );
  const m = tail.exec(key);
  return m ? Number(m[1]) : null;
}

const escapeRe = (s: string): string =>
  s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Every node of a body tree with its id, depth first. */
export function allNodes(body: Body): Node[] {
  const out: Node[] = [];
  const walk = (b: Body) => {
    for (const n of b.nodes) {
      out.push(n);
      if (n.kind === 'group' || n.kind === 'loop' || n.kind === 'map') walk(n);
    }
  };
  walk(body);
  return out;
}
