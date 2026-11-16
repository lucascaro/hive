// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Round-4 gate follow-ups on PR #550.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { call, harness, rowOf } from './harness.ts';

const never = (): Promise<void> => new Promise(() => {});
const oneCheck = (argv: string[] = ['true']) => workflow('c', s.object({}), (g) => void g.check('c', { argv, timeoutSec: 60 }));

test('a refused run cancels its live nodes and clears their recorded pids, so nothing stale can be signalled or left running', async () => {
  const calls: unknown[][] = [];
  const h = harness({ agent: never, check: never }, { kill: { kill: (...a: [number, string | number]) => void calls.push(a), graceMs: 1 } });
  try {
    // An IR with no nodes no longer validates: the run is refused at recovery. Its row says a check is running, under a pid.
    const ir = { ir: 1, name: 'w', input: { type: 'object', properties: {}, required: [], additionalProperties: false }, nodes: [], edges: [] };
    h.store.createRun(
      { run_id: 'r1', project_id: 'p', workflow: 'w', file: 'w.ts', source_hash: '', ir: JSON.stringify(ir), input: '{}' },
      [{ key: 'c', node_id: 'c', state: 'running', attempt: 1, ready: true, progress: 0, turns: 0, pid: 4242, pgid: 4242, proc_start: 'x' }],
      [{ key: null, node_id: null, attempt: 0, type: 'run.started', data: {} }],
    );
    await h.engine.recover();
    const c = rowOf(h, 'r1', 'c');
    assert.equal(c?.state, 'cancelled');
    assert.equal(c?.pid, undefined);
    assert.equal(c?.pgid, undefined);
    assert.equal(h.store.getRun('r1')?.state, 'failed');
    const types = h.store.events('r1', 0, 100).map((e) => `${e.type}:${e.key}`);
    assert.deepEqual(types.slice(-2), ['node.finished:c', 'run.finished:null'], 'the node and the run end in one commit');
    await h.engine.shutdown(1);
    await h.engine.recover(); // a second start finds nothing active
    assert.deepEqual(calls, [], 'no signal was ever sent for the stale row');
    assert.equal(call(h, 'run.nodes', { run_id: 'r1' }).ok, true);
  } finally {
    h.close();
  }
});

test('the survivor message names the OS refusal, not a permission the engine never checked', async () => {
  const { spawn } = await import('node:child_process');
  const { procStart } = await import('../engine/killtree.ts');
  const child = spawn(process.execPath, ['-e', 'setInterval(()=>{},1000)'], { stdio: 'ignore' });
  const pid = child.pid as number;
  const eperm = (): never => {
    throw Object.assign(new Error('operation not permitted'), { code: 'EPERM' });
  };
  const h = harness({ agent: never, check: never }, { kill: { kill: eperm, graceMs: 1 } });
  try {
    h.store.createRun(
      { run_id: 'r1', project_id: 'p', workflow: 'c', file: 'c.ts', source_hash: '', ir: JSON.stringify(oneCheck()), input: '{}' },
      [{ key: 'c', node_id: 'c', state: 'running', attempt: 1, ready: true, progress: 0, turns: 0, pid, pgid: pid, proc_start: procStart(pid) as string }],
      [{ key: null, node_id: null, attempt: 0, type: 'run.started', data: {} }],
    );
    await h.engine.recover();
    assert.match(rowOf(h, 'r1', 'c')?.error ?? '', /the operating system refused the signal/);
    assert.doesNotMatch(rowOf(h, 'r1', 'c')?.error ?? '', /permission denied/);
  } finally {
    child.kill();
    h.close();
  }
});
