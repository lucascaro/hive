// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Node instances (spec 529, Run semantics). An IR node is one reviewed
// template; a run has one *instance* of it per loop iteration and per map copy
// it sits under. The instance's key is the IR id with `[i]` after each loop
// (1-based iteration) and map (0-based element) on its path:
// `fix[2]/work`, `work/each[3]/do`. Ids are full paths, so a node's
// containers are the prefixes of its id.

import type { Body, IR, Node } from '../sdk/dsl.ts';

export interface Placed {
  node: Node;
  /** The id of the container the node sits in, '' at the top level. */
  parent: string;
  /** Ids of the enclosing loops and maps, outermost first, not the node itself. */
  lm: string[];
}

export interface Index {
  ir: IR;
  byId: Map<string, Placed>;
}

export function buildIndex(ir: IR): Index {
  const byId = new Map<string, Placed>();
  const walk = (body: Body, parent: string, lm: string[]): void => {
    for (const node of body.nodes) {
      byId.set(node.id, { node, parent, lm });
      if ('nodes' in node) walk(node, node.id, node.kind === 'group' ? lm : [...lm, node.id]);
    }
  };
  walk(ir, '', []);
  return { ir, byId };
}

/** The instance key of `id` under `ctx`: one index per enclosing loop or map, outermost first. */
export function keyOf(ix: Index, id: string, ctx: readonly number[]): string {
  const need = ix.byId.get(id)?.lm.length ?? 0;
  if (ctx.length !== need) throw new Error(`keyOf ${id}: needs ${need} indices, got ${ctx.length}`);
  const segs = id.split('/');
  let used = 0;
  let prefix = '';
  const out: string[] = [];
  for (let i = 0; i < segs.length; i++) {
    prefix = i === 0 ? segs[0] : `${prefix}/${segs[i]}`;
    const kind = ix.byId.get(prefix)?.node.kind;
    out.push(i < segs.length - 1 && (kind === 'loop' || kind === 'map') ? `${segs[i]}[${ctx[used++]}]` : segs[i]);
  }
  return out.join('/');
}

/** The inverse of keyOf, or null for a string that is not a key of this IR. */
export function parseKey(ix: Index, key: string): { id: string; ctx: number[] } | null {
  const ctx: number[] = [];
  const ids: string[] = [];
  for (const seg of key.split('/')) {
    const m = /^([a-z0-9-]+)(?:\[(\d+)\])?$/.exec(seg);
    if (!m) return null;
    ids.push(m[1]);
    if (m[2] !== undefined) ctx.push(Number(m[2]));
  }
  const id = ids.join('/');
  const placed = ix.byId.get(id);
  if (!placed || placed.lm.length !== ctx.length || keyOf(ix, id, ctx) !== key) return null;
  return { id, ctx };
}

/**
 * A key as it appears in a branch name under `hive/wf/<run>/`: `[N]` becomes
 * `_N` and `/` stays (`fix[2]/work` is `fix_2/work`). The same function as the
 * SDK's `encodeWorkflowKey`, pinned equal by a test, so the engine need not
 * import untyped JavaScript.
 */
export const encodeKey = (key: string): string => key.replace(/\[(\d+)\]/g, '_$1');
