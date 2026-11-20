// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Worktrees and heads (spec 529, Worktrees). Every leaf that does work gets a
// branch `hive/wf/<run>/<key>`; the work of an instance lives on its branch,
// and the next instance starts from the branch (or branches) upstream of it.
// The head and input-head functions are pure over the run model; `prepare` is
// the one place a worktree is made.

import type { Body, GroupNode, LoopNode, MapNode, Node } from '../sdk/dsl.ts';
import type { Engine } from './engine.ts';
import { instanceCtx, type Model, scopeOf } from './interp.ts';
import { encodeKey, keyOf } from './keys.ts';

/** The branch an instance's work lives on. The daemon checks the shape; the key grammar keeps it injective. */
export const branchOf = (runId: string, key: string): string => `hive/wf/${runId}/${encodeKey(key)}`;

const uniq = (xs: string[]): string[] => [...new Set(xs)];

/** The nodes of a body nothing else in it points to: where its work ends up. */
function sinks(body: Body): Node[] {
  return body.nodes.filter((n) => !body.edges.some((e) => e.from === n.id));
}

/**
 * The branches an instance's work ends on, in order. A leaf has its own; a
 * node that does no work of its own (a human, a skipped node, a check that runs
 * in its input's worktree) passes its input through; a container reports what
 * its sinks end on (a map: every copy's), and the node after it merges them.
 */
export function headsOf(m: Model, key: string, memo: Memo = new Map()): string[] {
  const hit = memo.get(`h:${key}`);
  if (hit) return hit;
  const heads = computeHeads(m, key, memo);
  memo.set(`h:${key}`, heads);
  return heads;
}

/** Heads already worked out during one question: without it a layered graph is walked once per path, which is exponential in its depth. */
type Memo = Map<string, string[]>;

function computeHeads(m: Model, key: string, memo: Memo): string[] {
  const { ix } = m;
  const row = m.rows.get(key);
  const node = row && ix.byId.get(row.node_id)?.node;
  const sc = scopeOf(m, key);
  if (!row || !node || !sc) return [];
  const through = (): string[] => inputHeads(m, key, memo);
  const ofSinks = (body: GroupNode | LoopNode | MapNode, ctx: readonly number[]): string[] =>
    uniq(sinks(body).flatMap((s) => headsOf(m, keyOf(ix, s.id, ctx), memo)));
  switch (node.kind) {
    case 'agent':
      return row.state === 'skipped' ? through() : row.branch ? [row.branch] : [];
    case 'check': {
      // A check commits nothing, so it passes its input through: the one head it runs in, or none. Only a merge worktree
      // (several inputs) is a new head: what the next node must start from. A base worktree made for a check with no
      // agent upstream (B-4) is nobody's head, or every check after a check would run in the first one's tree.
      const heads = through();
      return row.state !== 'skipped' && row.branch && heads.length >= 2 ? [row.branch] : heads;
    }
    case 'human':
      return through();
    case 'group': {
      const h = ofSinks(node, sc.ctx);
      return h.length > 0 ? h : through();
    }
    case 'loop': {
      const h = row.progress >= 1 ? ofSinks(node, [...sc.ctx, row.progress]) : [];
      return h.length > 0 ? h : through();
    }
    case 'map': {
      const h = uniq(Array.from({ length: row.progress }, (_, i) => ofSinks(node, [...sc.ctx, i])).flat());
      return h.length > 0 ? h : through();
    }
  }
}

/**
 * The branches an instance's own worktree is built from: the heads of the
 * nearest upstream instances that have one. None means the run's pinned base;
 * several mean a merge worktree. A loop's entry node in iteration k > 1 starts
 * from its own iteration k-1 (that is how a worker revises its own change),
 * falling back to the loop's input when that copy did nothing.
 */
export function inputHeads(m: Model, key: string, memo: Memo = new Map()): string[] {
  const hit = memo.get(`i:${key}`);
  if (hit) return hit;
  const heads = computeInputHeads(m, key, memo);
  memo.set(`i:${key}`, heads);
  return heads;
}

function computeInputHeads(m: Model, key: string, memo: Memo): string[] {
  const { ix } = m;
  const row = m.rows.get(key);
  const placed = row && ix.byId.get(row.node_id);
  const sc = scopeOf(m, key);
  if (!row || !placed || !sc) return [];
  const parent = placed.parent ? ix.byId.get(placed.parent)?.node : undefined;
  const body: Body = (parent as Body | undefined) ?? ix.ir;
  const incoming = body.edges.filter((e) => e.to === placed.node.id);
  if (incoming.length > 0) return uniq(incoming.flatMap((e) => headsOf(m, keyOf(ix, e.from, sc.ctx), memo)));
  if (!parent) return [];
  const outerCtx = parent.kind === 'group' ? sc.ctx : sc.ctx.slice(0, -1);
  const iteration = sc.ctx[sc.ctx.length - 1];
  if (parent.kind === 'loop' && iteration > 1) {
    const own = headsOf(m, keyOf(ix, placed.node.id, [...outerCtx, iteration - 1]), memo);
    if (own.length > 0) return own;
  }
  return inputHeads(m, keyOf(ix, parent.id, outerCtx), memo);
}

/** The instance key `of` names, as read from the instance `key` (same copy and iteration, or the latest of a loop only `of` is in). */
export function instanceKeyOf(m: Model, key: string, id: string): string | undefined {
  const sc = scopeOf(m, key);
  const tlm = m.ix.byId.get(id)?.lm;
  const ctx = sc && tlm && instanceCtx(m, tlm, sc);
  return ctx ? keyOf(m.ix, id, ctx) : undefined;
}

export interface Workspace {
  path: string;
  branch: string;
}

export interface Conflicted {
  conflict: { branch: string; files: string[]; worktree: string };
  message: string;
}

export const isConflict = (w: Workspace | Conflicted): w is Conflicted => 'conflict' in w;

/** Branch names from the engine's own derivation are passed as `refs/heads/...`, so a tag of the same name cannot stand in for one. */
const asBranchRef = (b: string): string => `refs/heads/${b}`;

/** A run's PREPAREs are serialized until its base is pinned: the first one fixes the base every other uses (spec 529, R4). */
const chains = new Map<string, Promise<unknown>>();

async function serialized<T>(runId: string, fn: () => Promise<T>): Promise<T> {
  const prev = chains.get(runId) ?? Promise.resolve();
  const next = prev.then(fn, fn);
  chains.set(runId, next);
  try {
    return await next;
  } finally {
    if (chains.get(runId) === next) chains.delete(runId);
  }
}

/** What the engine tells the user when a merge stops on conflicts. */
export function conflictMessage(branch: string, files: readonly string[], path: string): string {
  const shown = files.slice(0, 8).join(', ');
  return `Merging ${branch} conflicted in ${shown}${files.length > 8 ? `, and ${files.length - 8} more` : ''}. The conflicting merge was aborted: the worktree kept at ${path} is clean, holds the branches merged before it, and does not contain ${branch}. Merge ${branch} there yourself, resolve the conflicts and commit, then retry the run.`;
}

/**
 * Makes (or re-attaches) the worktree instance `key` works in.
 *
 * - An agent with `worktree: {of}` uses the path of the `of` instance.
 * - A check with exactly one upstream head runs in that head's worktree.
 * - Everything else gets its own branch: from the pinned base (no upstream), from
 *   the one upstream branch, or merged from several.
 */
export async function prepare(engine: Engine, runId: string, key: string): Promise<Workspace | Conflicted> {
  const { model, project_id } = engine.run(runId);
  const { ix } = model;
  const row = model.rows.get(key);
  const node = row && ix.byId.get(row.node_id)?.node;
  if (!row || !node) throw new Error(`no node instance ${key}`);

  if (node.kind === 'agent' && typeof node.worktree === 'object') {
    const ofKey = instanceKeyOf(model, key, node.worktree.of);
    const of = ofKey ? model.rows.get(ofKey) : undefined;
    if (!of?.worktree || !of.branch) throw new Error(`worktree.of ${node.worktree.of} has no worktree to share`);
    return { path: of.worktree, branch: of.branch };
  }

  const heads = inputHeads(model, key);
  if (node.kind === 'check' && heads.length === 1) {
    const reply = await engine.hive.request(
      'PREPARE_WORKTREE',
      { project_id, workflow_run_id: runId, branch: heads[0], base: asBranchRef(heads[0]) },
      { timeoutMs: 120_000 },
    );
    // Only a branch that already exists may be re-attached: PREPARE would otherwise make a fresh one, and the check would test the wrong tree.
    if (!reply.reattached) throw new Error(`the upstream branch ${heads[0]} is gone, so ${key} has no worktree to run in`);
    return { path: reply.path, branch: reply.branch };
  }

  const branch = branchOf(runId, key);
  const run = async (): Promise<Workspace | Conflicted> => {
    const pinned = engine.store.getRun(runId)?.base_sha ?? '';
    const base = heads.length > 0 ? asBranchRef(heads[0]) : pinned || 'HEAD';
    const merge = heads.slice(1);
    const reply = await engine.hive.request(
      'PREPARE_WORKTREE',
      { project_id, workflow_run_id: runId, branch, base, ...(merge.length > 0 ? { merge } : {}) },
      { timeoutMs: 120_000 },
    );
    if (!pinned && heads.length === 0) engine.pinBase(runId, reply.base_sha ?? (reply.reattached ? reply.head_sha : undefined) ?? '');
    if (reply.conflict) {
      const files: string[] = reply.conflict.files ?? [];
      return { conflict: { branch: reply.conflict.branch, files, worktree: reply.path }, message: conflictMessage(reply.conflict.branch, files, reply.path) };
    }
    return { path: reply.path, branch: reply.branch };
  };
  return heads.length === 0 && !engine.store.getRun(runId)?.base_sha ? serialized(runId, run) : run();
}
