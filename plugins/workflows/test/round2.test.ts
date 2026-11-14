// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Regression tests for the Opus gate's second-round findings on PR #550.
// Nothing here signals a real process: the OS refusing a signal is injected.

import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { test } from 'node:test';
import { type CheckHandle, Coalescer } from '../engine/check.ts';
import { capData, MAX_EVENT_BYTES } from '../engine/events.ts';
import { killTree, procStart, reap } from '../engine/killtree.ts';
import { checkExecutor } from '../engine/nodes.ts';
import { redactCut } from '../engine/redact.ts';
import { encodedSize } from '../engine/size.ts';
import { workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { call, FakeHive, harness, rowOf, tick, waitFor } from './harness.ts';

const never = (): Promise<void> => new Promise(() => {});
const oneCheck = () => workflow('c', s.object({}), (g) => void g.check('c', { argv: ['true'] }));
const TOKEN = `ghp_${'a'.repeat(36)}`;
/** A real, harmless process whose group we never signal (the kill seam is injected): a live pid that is not ours. */
function sleeper(): { pid: number; start: string; done(): void } {
  const c = spawn(process.execPath, ['-e', 'setInterval(()=>{},1000)'], { stdio: 'ignore' });
  const pid = c.pid as number;
  const start = procStart(pid) as string;
  return { pid, start, done: () => void c.kill() };
}

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

test('a refused stored IR gets a redacted, cut reason, with a token straddling the 1024-byte cut', async () => {
  const h = harness({ agent: never, check: never });
  try {
    // An id is a valid id and a secret at once: the validator's message repeats it, and the token starts before byte 1024 and ends after it.
    const from = `${'a'.repeat(975)}-sk-${'b'.repeat(30)}`;
    const ir = { ir: 1, name: 'w', input: { type: 'object', properties: {}, required: [], additionalProperties: false }, nodes: [{ id: 'ok', kind: 'check', argv: ['true'], timeout_s: 1 }], edges: [{ from, to: 'ok' }] };
    h.store.createRun({ run_id: 'r1', project_id: 'p', workflow: 'w', file: 'w.ts', source_hash: '', ir: JSON.stringify(ir), input: '{}' }, [], [{ key: null, node_id: null, attempt: 0, type: 'run.started', data: {} }]);
    await h.engine.recover();
    const err = h.store.getRun('r1')?.error ?? '';
    assert.match(err, /no longer validates/);
    assert.ok(!err.includes('sk-') && !err.includes('bbbb'), `no part of the token survives the cut: ${err.slice(1000)}`);
  } finally {
    h.close();
  }
});

// ----------------------------------------------------------- 2. EPERM everywhere

test('reap reports a survivor it may not signal as kept, and never throws', async () => {
  const p = sleeper();
  try {
    assert.equal(await reap({ pid: p.pid, pgid: p.pid, proc_start: p.start }, { kill: eperm, graceMs: 5 }), 'kept');
  } finally {
    p.done();
  }
});

test('recover() with a survivor the OS will not let it stop resolves, failing the node and naming the pid', async () => {
  let cleanup = (): void => {};
  const h = harness({ agent: never, check: never }, { kill: { kill: eperm, graceMs: 5 } });
  try {
    const p = sleeper();
    cleanup = p.done;
    h.store.createRun(
      { run_id: 'r1', project_id: 'p', workflow: 'c', file: 'c.ts', source_hash: '', ir: JSON.stringify(oneCheck()), input: '{}' },
      [{ key: 'c', node_id: 'c', state: 'running', attempt: 1, ready: true, progress: 0, turns: 0, pid: p.pid, pgid: p.pid, proc_start: p.start }],
      [{ key: null, node_id: null, attempt: 0, type: 'run.started', data: {} }],
    );
    await h.engine.recover(); // must resolve: a rejection here would end the plugin on every start
    const c = rowOf(h, 'r1', 'c');
    assert.equal(c?.state, 'failed');
    assert.match(c?.error ?? '', /is still running and could not be stopped/);
  } finally {
    cleanup();
    h.close();
  }
});

test('shutdown() still flushes buffered events when a stop is refused', async () => {
  let cleanup = (): void => {};
  const h = harness({ agent: never, check: never }, { kill: { kill: eperm, graceMs: 5 } });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck(), input: {} });
    const p = sleeper();
    h.engine.commit(id, { t: 'start', key: 'c', patch: { pid: p.pid, pgid: p.pid } });
    cleanup = p.done;
    h.engine.buffer(id, { key: 'c', node_id: 'c', attempt: 1, type: 'check.output', data: { stream: 'stdout', text: 'buffered' } });
    await h.engine.shutdown(5);
    assert.equal(h.store.events(id, 0, 100).filter((e) => e.type === 'check.output').length, 1, 'the buffered line was written');
  } finally {
    cleanup();
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

// ------------------------------------------- hostile rows never reach kill()

const hostile: [string, { pid: number; pgid?: number }][] = [
  ['pgid 1 (every process)', { pid: 4242, pgid: 1 }],
  ['pid 1', { pid: 1 }],
  ['pid 0', { pid: 0 }],
  ['pgid 0 (our own group)', { pid: 4242, pgid: 0 }],
  ['negative pgid', { pid: 4242, pgid: -7 }],
  ['negative pid', { pid: -7 }],
  ['non-integer pid', { pid: 12.5 }],
  ['NaN pid', { pid: Number.NaN }],
  ['our own pid', { pid: process.pid }],
  ['our own group as pgid', { pid: 4242, pgid: process.pid }],
  ['a pgid that is not the pid', { pid: 4242, pgid: 4243 }],
];

test('killTree and reap refuse every hostile id without a single kill call', async () => {
  const calls: unknown[][] = [];
  const spy = (...a: unknown[]): void => void calls.push(a);
  for (const platform of ['linux', 'win32']) {
    for (const [what, t] of hostile) {
      assert.equal(await killTree(t, { platform, graceMs: 1, kill: spy, spawn: spy as never }), 'refused', `${what} on ${platform}`);
      assert.equal(await reap({ ...t, proc_start: 'x' }, { platform, graceMs: 1, kill: spy, spawn: spy as never, spawnSync: spy as never }), 'refused', `reap ${what}`);
    }
  }
  assert.deepEqual(calls, [], 'nothing was signalled, no taskkill was run, no process was probed');
});

test('recover() fails a node whose recorded ids are hostile, naming the row, and signals nothing', async () => {
  const calls: unknown[][] = [];
  const h = harness({ agent: never, check: never }, { kill: { kill: (...a: [number, string | number]) => void calls.push(a), graceMs: 1 } });
  try {
    h.store.createRun(
      { run_id: 'r1', project_id: 'p', workflow: 'c', file: 'c.ts', source_hash: '', ir: JSON.stringify(oneCheck()), input: '{}' },
      [{ key: 'c', node_id: 'c', state: 'running', attempt: 1, ready: true, progress: 0, turns: 0, pid: 4242, pgid: 1, proc_start: 'x' }],
      [{ key: null, node_id: null, attempt: 0, type: 'run.started', data: {} }],
    );
    await h.engine.recover();
    assert.equal(rowOf(h, 'r1', 'c')?.state, 'failed');
    assert.match(rowOf(h, 'r1', 'c')?.error ?? '', /recorded process for c cannot be used.*nothing was signalled/);
    assert.deepEqual(calls, []);
  } finally {
    h.close();
  }
});

test('shutdown() signals nothing for hostile ids and still flushes', async () => {
  const calls: unknown[][] = [];
  const h = harness({ agent: never, check: never }, { kill: { kill: (...a: [number, string | number]) => void calls.push(a), graceMs: 1 } });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck(), input: {} });
    h.engine.commit(id, { t: 'start', key: 'c', patch: { pid: 4242, pgid: 1 } });
    h.engine.buffer(id, { key: 'c', node_id: 'c', attempt: 1, type: 'check.output', data: { stream: 'stdout', text: 'x' } });
    await h.engine.shutdown(1);
    assert.deepEqual(calls, []);
    assert.equal(h.store.events(id, 0, 100).filter((e) => e.type === 'check.output').length, 1);
  } finally {
    h.close();
  }
});

// ------------------------------- a token split across timer ticks is never stored in halves

const manual = () => {
  const fns: (() => void)[] = [];
  return { timers: { set: (fn: () => void) => (fns.push(fn), fns.length), clear: () => {} }, fire: () => fns.splice(0).forEach((f) => f()) };
};

for (const [name, first, second] of [
  ['ghp_', `ghp_${'A'.repeat(20)}`, 'A'.repeat(16)],
  ['sk-', `sk-${'b'.repeat(10)}`, 'b'.repeat(15)],
  ['Bearer', `Bearer ${'c'.repeat(8)}`, 'c'.repeat(12)],
] as const) {
  test(`a ${name} token split across two timer ticks is redacted whole`, () => {
    const out: string[] = [];
    const m = manual();
    const co = new Coalescer((t) => out.push(t), m.timers);
    co.write(`token: ${first}`);
    m.fire(); // first tick: the trailing run is held
    assert.ok(out.every((o) => !/ghp_|sk-|Bearer/.test(o)), `nothing of the token was emitted yet: ${JSON.stringify(out)}`);
    co.write(second);
    m.fire(); // second tick: still held
    co.write('\n');
    m.fire();
    co.end();
    const all = out.join('');
    assert.ok(!/ghp_|sk-[a-z]|Bearer [a-z]/.test(all), all);
    assert.match(all, /\[redacted\]/);
  });
}

test('an unfinished line is held raw until its newline or the end, and the timer never ships it', () => {
  const out: string[] = [];
  const m = manual();
  const co = new Coalescer((t) => out.push(t), m.timers);
  co.write('done line\nunfinished');
  m.fire();
  assert.equal(out.join(''), 'done line\n', 'only the complete line is shipped');
  m.fire();
  assert.equal(out.join(''), 'done line\n', 'ticks do not release the partial line');
  co.end();
  assert.equal(out.join(''), 'done line\nunfinished', 'the end releases it');
});
;
