// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The executors that start a node (spec 529): a check runs a command in the
// worktree of its upstream node, or in one of its own when there is none
// (B-4, worktrees.ts); an agent node is agent.ts's.

import type { CheckNode } from '../sdk/dsl.ts';
import { type AgentDeps, agentExecutor } from './agent.ts';
import { type CheckHandle, type CheckSpec, launch, type OutputMeta } from './check.ts';
import type { Executors, NodeContext } from './engine.ts';
import { isConflict, prepare } from './worktrees.ts';

type Launch = (spec: CheckSpec, onOutput: (stream: 'stdout' | 'stderr', text: string, meta: OutputMeta) => void) => CheckHandle;

export function checkExecutor(doLaunch: Launch = launch): (ctx: NodeContext) => Promise<void> {
  return async (ctx) => {
    const { engine, run_id, key, node } = ctx;
    const n = node as CheckNode;
    const startable = (): boolean => {
      const s = engine.run(run_id).model.rows.get(key)?.state;
      return s === 'pending' || s === 'interrupted';
    };
    const wt = await prepare(engine, run_id, key);
    if (!engine.current(ctx) || !startable()) return; // cancelled, failed or retried while the worktree was being made
    if (isConflict(wt)) {
      engine.failNode(run_id, key, wt.message, { conflict: wt.conflict });
      return;
    }

    const attempt = (engine.run(run_id).model.rows.get(key)?.attempt ?? 0) + 1;
    const h = doLaunch({ argv: n.argv, cwd: wt.path, timeout_s: n.timeout_s }, (stream, text, meta) => {
      if (engine.current(ctx)) engine.buffer(run_id, { key, node_id: n.id, attempt, type: 'check.output', data: { stream, text, ...meta } }); // output of a cancelled attempt is not the retry's
    });
    // Every failure below is reported only while this launch still belongs to the live run: a cancel and retry during an await must not fail the new attempt.
    const fail = (why: string): void => {
      if (engine.current(ctx)) engine.failNode(run_id, key, why);
    };
    const stopQuietly = async (): Promise<string | undefined> => h.stop().then((r) => (r === 'kept' ? 'the operating system refused the signal (EPERM)' : undefined), (e: Error) => e.message);
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

export function defaultExecutors(deps: AgentDeps = {}): Executors {
  return { check: checkExecutor(), agent: agentExecutor(deps) };
}
