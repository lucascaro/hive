// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Regression tests for the Opus gate's second-round findings on PR #550.
// Nothing here signals a real process: the OS refusing a signal is injected.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { CheckHandle } from '../engine/check.ts';
import { capData, MAX_EVENT_BYTES } from '../engine/events.ts';
import { procStart, reap } from '../engine/killtree.ts';
import { checkExecutor } from '../engine/nodes.ts';
import { redactCut } from '../engine/redact.ts';
import { encodedSize } from '../engine/size.ts';
import { workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { call, FakeHive, harness, rowOf, tick, waitFor } from './harness.ts';

const never = (): Promise<void> => new Promise(() => {});
const oneCheck = () => workflow('c', s.object({}), (g) => void g.check('c', { argv: ['true'] }));
const TOKEN = `ghp_${'a'.repeat(36)}`;
const eperm = (): never => {
  throw Object.assign(new Error('operation not permitted'), { code: 'EPERM' });
};

// ------------------------------------------------ 1. redact first, then cut

test('redactCut replaces a secret that straddles the cut whole', () => {
  const out = redactCut(`${'x'.repeat(28)} ${TOKEN}`, 64);
  assert.ok(!out.includes('ghp'), out);
  assert.match(out, /\[redacted\]/);
});

test('a bad permission id with a token straddling the 64-byte cut leaks nothing into the error or the event', () => {
  const h = harness({ agent: never, check: never });
  try {
    const ir = workflow('a', s.object({}), (g) => void g.agent('a', { agent: 'claude', prompt: 'go', output: s.object({ r: s.string() }) }));
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'a.ts', ir, input: {} });
    h.engine.commit(id, { t: 'start', key: 'a' });
    h.engine.commit(id, { t: 'wait', key: 'a', waiting: { reason: 'permission', session_id: 's', request_id: '1-1', title: 't', options: [{ option_id: `${'x'.repeat(28)} ${TOKEN}`, name: 'n', kind: 'allow_once' }] } });
    const n = call(h, 'node.get', { run_id: id, key: 'a' }) as { error: string };
    assert.ok(!n.error.includes('ghp'), n.error);
    const fin = h.store.events(id, 0, 100).find((e) => e.type === 'node.finished' && e.key === 'a');
    assert.ok(!JSON.stringify(fin?.data).includes('ghp'));
    assert.ok(!JSON.stringify(h.store.getRun(id)).includes('ghp'));
  } finally {
    h.close();
  }
});

test('a conflict whose branch, worktree or file has a token straddling the 256-byte cut leaks nothing', () => {
  const h = harness({ agent: never, check: never });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck(), input: {} });
    h.engine.commit(id, { t: 'start', key: 'c' });
    const padded = `${'d/'.repeat(110)}${TOKEN}`; // the token starts before byte 256 and ends after it
    h.engine.commit(id, { t: 'fail', key: 'c', error: 'conflict', conflict: { branch: padded, worktree: padded, files: [padded] } });
    assert.ok(!JSON.stringify(call(h, 'node.get', { run_id: id, key: 'c' })).includes('ghp'), 'node.get');
    assert.ok(!JSON.stringify(h.store.events(id, 0, 100)).includes('ghp'), 'events');
    assert.ok(!JSON.stringify(h.store.loadRows(id)).includes('ghp'), 'stored rows');
  } finally {
    h.close();
  }
});

test('topics redact before they cut: error replies and kept worktree paths', () => {
  const h = harness({ agent: never, check: never });
  try {
    const ir = { ir: 1, name: 'x', input: { type: 'object', properties: {}, required: [], additionalProperties: false }, nodes: [{ id: `${'x'.repeat(4000)} ${TOKEN}` }], edges: [] };
    const r = call(h, 'test.start-run', { project_id: 'p', ir, input: {} }) as { error: { message: string } };
    assert.ok(!r.error.message.includes('ghp'));
    h.store.createRun({ run_id: 'r1', project_id: 'p', workflow: 'w', file: 'w.ts', source_hash: '', ir: '{}', input: '{}' }, [], []);
    h.store.commit('r1', { rows: [], events: [], run: { kept_worktrees: [`${'p/'.repeat(120)}${TOKEN}`] } });
    const g = call(h, 'run.get', { run_id: 'r1' }) as { run: { kept_worktrees: string[] } };
    assert.ok(!g.run.kept_worktrees[0].includes('ghp'));
  } finally {
    h.close();
  }
});

test('a refused stored IR gets a redacted, cut reason', async () => {
  const h = harness({ agent: never, check: never });
  try {
    const ir = { ir: 1, name: 'w', input: { type: 'object', properties: {}, required: [], additionalProperties: false }, nodes: [{ id: 'ok', kind: 'check', argv: ['true'], timeout_s: 1 }], edges: [{ from: `${'q'.repeat(1000)} ${TOKEN}`, to: 'ok' }] };
    h.store.createRun({ run_id: 'r1', project_id: 'p', workflow: 'w', file: 'w.ts', source_hash: '', ir: JSON.stringify(ir), input: '{}' }, [], [{ key: null, node_id: null, attempt: 0, type: 'run.started', data: {} }]);
    await h.engine.recover();
    assert.ok(!(h.store.getRun('r1')?.error ?? '').includes('ghp'));
  } finally {
    h.close();
  }
});

// ----------------------------------------------------------- 2. EPERM everywhere

test('reap reports a survivor it may not signal as kept, and never throws', async () => {
  const start = procStart(process.pid);
  assert.ok(start, 'this process has a start time');
  assert.equal(await reap({ pid: process.pid, pgid: process.pid, proc_start: start }, { kill: eperm, graceMs: 5 }), 'kept');
});

test('recover() with a survivor the OS will not let it stop resolves, failing the node and naming the pid', async () => {
  const h = harness({ agent: never, check: never }, { kill: { kill: eperm, graceMs: 5 } });
  try {
    const start = procStart(process.pid);
    h.store.createRun(
      { run_id: 'r1', project_id: 'p', workflow: 'c', file: 'c.ts', source_hash: '', ir: JSON.stringify(oneCheck()), input: '{}' },
      [{ key: 'c', node_id: 'c', state: 'running', attempt: 1, ready: true, progress: 0, turns: 0, pid: process.pid, pgid: process.pid, ...(start ? { proc_start: start } : {}) }],
      [{ key: null, node_id: null, attempt: 0, type: 'run.started', data: {} }],
    );
    await h.engine.recover(); // must resolve: a rejection here would end the plugin on every start
    const c = rowOf(h, 'r1', 'c');
    assert.equal(c?.state, 'failed');
    assert.match(c?.error ?? '', new RegExp(`process ${process.pid} is still running and could not be stopped`));
  } finally {
    h.close();
  }
});

test('shutdown() still flushes buffered events when a stop is refused', async () => {
  const h = harness({ agent: never, check: never }, { kill: { kill: eperm, graceMs: 5 } });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck(), input: {} });
    h.engine.commit(id, { t: 'start', key: 'c', patch: { pid: process.pid, pgid: process.pid } });
    h.engine.buffer(id, { key: 'c', node_id: 'c', attempt: 1, type: 'check.output', data: { stream: 'stdout', text: 'buffered' } });
    await h.engine.shutdown(5);
    assert.equal(h.store.events(id, 0, 100).filter((e) => e.type === 'check.output').length, 1, 'the buffered line was written');
  } finally {
    h.close();
  }
});

// ------------------------------------------ M6: a natural exit during cancel+retry

test('an old attempt exiting on its own during cancel and retry neither completes the retried node nor drops its stop handle', async () => {
  let finishOld: () => void = () => {};
  let calls = 0;
  let newStops = 0;
  const fake = (): CheckHandle => {
    if (calls++ === 0) {
      return { pid: 4242, pgid: 4242, proc_start: 'x', result: new Promise((r) => (finishOld = () => r({ kind: 'exit', ok: true, exit_code: 0 }))), stop: async () => {} };
    }
    return { pid: 4343, pgid: 4343, proc_start: 'y', result: new Promise(() => {}), stop: async () => void newStops++ };
  };
  const hive = new FakeHive();
  hive.replies.PREPARE_WORKTREE = (req) => ({ path: process.cwd(), branch: req.branch, base_sha: 'sha-1', head_sha: 'sha-1', reattached: false });
  const h = harness({ agent: never, check: checkExecutor(fake) }, { hive });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck(), input: {} });
    await waitFor(() => rowOf(h, id, 'c')?.state === 'running', 'attempt 1');
    h.engine.cancelRun(id);
    h.engine.retryRun(id);
    await waitFor(() => calls === 2 && rowOf(h, id, 'c')?.state === 'running', 'the retried attempt');
    finishOld();
    await tick();
    await tick();
    const c = rowOf(h, id, 'c');
    assert.equal(c?.state, 'running', 'the old exit did not finish the new attempt');
    assert.equal(c?.output_raw, undefined);
    h.engine.cancelRun(id);
    await tick();
    assert.equal(newStops, 1, "the new attempt's stop handle was still registered");
  } finally {
    h.close();
  }
});

// ------------------------------------------------------ agent.plan is not quadratic

test('a 20,000-entry agent.plan is capped quickly, with an exact omitted count', () => {
  const entries = Array.from({ length: 20_000 }, (_, i) => ({ content: `step ${i}`, status: 'pending' }));
  const t0 = Date.now();
  const d = capData('agent.plan', { session_id: 's', entries });
  assert.ok(Date.now() - t0 < 3000, `took ${Date.now() - t0} ms`);
  assert.ok(encodedSize(d) <= MAX_EVENT_BYTES);
  assert.equal((d.entries as unknown[]).length + (d.entries_omitted as number), 20_000);
});
