// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The executors that start a node (spec 529): a check runs a command in a
// worktree of its own (B-4); an agent node is B3's, and until then fails
// plainly rather than hanging.

import type { CheckNode } from '../sdk/dsl.ts';
import { type CheckHandle, type CheckSpec, launch } from './check.ts';
import type { Engine, Executors, NodeContext } from './engine.ts';
import { encodeKey } from './keys.ts';

type Launch = (spec: CheckSpec, onOutput: (stream: 'stdout' | 'stderr', text: string) => void) => CheckHandle;

interface Prepared {
  path: string;
  branch: string;
}

/**
 * A check with no upstream head runs in its own clean worktree on
 * `hive/wf/<run>/<key>`, prepared from the run's pinned base (B-4): never the
 * project's checkout, which holds the user's uncommitted edits. A run's
 * PREPAREs are serialized so the first one fixes the base every other one uses:
 * PREPARE_WORKTREE refuses an empty base, so that first one asks for the
 * project's HEAD and the daemon reports the commit it resolved (`base_sha`).
 */
const prepareChains = new Map<string, Promise<unknown>>();

async function prepareBase(engine: Engine, runId: string, projectId: string, key: string): Promise<Prepared> {
  const run = async (): Promise<Prepared> => {
    const pinned = engine.store.getRun(runId)?.base_sha ?? '';
    const reply = await engine.hive.request(
      'PREPARE_WORKTREE',
      { project_id: projectId, workflow_run_id: runId, branch: `hive/wf/${runId}/${encodeKey(key)}`, base: pinned || 'HEAD' },
      { timeoutMs: 120_000 },
    );
    if (reply.conflict) throw new Error(`merge conflict in ${reply.conflict.branch}`);
    // A reattached worktree reports no base_sha. A check commits nothing, so its HEAD is still where it was made: the base.
    const base = reply.base_sha ?? (reply.reattached ? reply.head_sha : undefined) ?? '';
    if (!pinned) engine.pinBase(runId, base);
    return { path: reply.path, branch: reply.branch };
  };
  const prev = prepareChains.get(runId) ?? Promise.resolve();
  const next = prev.then(run, run);
  prepareChains.set(runId, next);
  try {
    return await next;
  } finally {
    if (prepareChains.get(runId) === next) prepareChains.delete(runId);
  }
}

export function checkExecutor(doLaunch: Launch = launch): (ctx: NodeContext) => Promise<void> {
  return async (ctx) => {
    const { engine, run_id, key, node } = ctx;
    const n = node as CheckNode;
    const startable = (): boolean => {
      const s = engine.run(run_id).model.rows.get(key)?.state;
      return s === 'pending' || s === 'interrupted';
    };
    const { project_id } = engine.run(run_id);
    const wt = await prepareBase(engine, run_id, project_id, key);
    if (!engine.current(ctx) || !startable()) return; // cancelled, failed or retried while the worktree was being made

    const attempt = (engine.run(run_id).model.rows.get(key)?.attempt ?? 0) + 1;
    const h = doLaunch({ argv: n.argv, cwd: wt.path, timeout_s: n.timeout_s }, (stream, text) => {
      engine.buffer(run_id, { key, node_id: n.id, attempt, type: 'check.output', data: { stream, text } });
    });
    // Every failure below is reported only while this launch still belongs to the live run: a cancel and retry during an await must not fail the new attempt.
    const fail = (why: string): void => {
      if (engine.current(ctx)) engine.failNode(run_id, key, why);
    };
    const stopQuietly = async (): Promise<string | undefined> => h.stop().then(() => undefined, (e: Error) => e.message);
    if (h.pid === undefined) {
      const r = await h.result;
      fail(r.kind === 'error' ? r.message : 'could not start the command');
      return;
    }
    // The pid, group and start time are written with the node going `running`, so a restart can reap it.
    if (h.proc_start === undefined) {
      // Without a start time a restart could not tell this process from a stranger, so it could not reap it: do not run it untracked.
      const err = await stopQuietly();
      fail(`could not read the check process start time${err ? `, and could not stop it: ${err}` : ''}`);
      return;
    }
    const patch = { pid: h.pid, ...(h.pgid === undefined ? {} : { pgid: h.pgid }), ...(h.proc_start === undefined ? {} : { proc_start: h.proc_start }), worktree: wt.path, branch: wt.branch };
    let started = false;
    try {
      started = engine.beginNode(run_id, key, patch);
    } finally {
      if (!started) await stopQuietly();
    }
    if (!started) return;
    engine.onStop(run_id, key, h.stop);
    const r = await h.result;
    if (engine.current(ctx)) engine.onStop(run_id, key, undefined);
    else return; // the run was cancelled and retried meanwhile: this result belongs to the old attempt
    switch (r.kind) {
      case 'exit':
        engine.completeNode(run_id, key, { ok: r.ok, exit_code: r.exit_code });
        break;
      case 'timeout':
        engine.failNode(run_id, key, `timed out after ${n.timeout_s}s`);
        break;
      case 'error':
        engine.failNode(run_id, key, r.message);
        break;
      case 'stopped':
        break; // the engine stopped it: the node is already cancelled
    }
  };
}

export function defaultExecutors(): Executors {
  return {
    check: checkExecutor(),
    agent: async ({ engine, run_id, key }) => {
      engine.failNode(run_id, key, 'agent nodes are not available in this build of the workflow engine yet');
    },
  };
}
