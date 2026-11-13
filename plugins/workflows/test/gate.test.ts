// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Regression tests for the Opus gate's blocking findings on PR #550.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { launch, type CheckHandle } from '../engine/check.ts';
import { capData, MAX_EVENT_BYTES, prepareEvent } from '../engine/events.ts';
import { checkExecutor } from '../engine/nodes.ts';
import { encodedSize, MAX_MESSAGE_BYTES } from '../engine/size.ts';
import { workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { call, FakeHive, harness, rowOf, runState, tick, waitFor } from './harness.ts';

const never = (): Promise<void> => new Promise(() => {});
const oneCheck = () => workflow('c', s.object({}), (g) => void g.check('c', { argv: ['true'] }));

// ------------------------------------------------------- 1. capData, any shape

test('a node.finished with 200 conflict files fits the cap and keeps its state and error', () => {
  const files = Array.from({ length: 200 }, (_, i) => `src/${'<'.repeat(80)}/file-${i}.go`);
  const data = capData('node.finished', { state: 'failed', error: 'merge conflicted in hive/wf/r1/m', cap: 'x', conflict: { branch: 'hive/wf/r1/m', files, worktree: '/w/m' } });
  assert.ok(encodedSize(data) <= MAX_EVENT_BYTES, `${encodedSize(data)} bytes`);
  assert.equal(data.state, 'failed');
  assert.equal(data.error, 'merge conflicted in hive/wf/r1/m', 'a discriminator is never emptied');
  const c = data.conflict as { files: string[]; files_omitted: number; branch: string };
  assert.equal(c.branch, 'hive/wf/r1/m');
  assert.ok(c.files_omitted > 0 && c.files.length + c.files_omitted === 200, 'the omitted count is exact');
  assert.equal(data.truncated, true);
});

test('a permission wait with 300 options fits the cap and keeps reason and request_id', () => {
  const options = Array.from({ length: 300 }, (_, i) => ({ option_id: `opt-${i}`, name: `Option ${'<'.repeat(100)} ${i}`, kind: 'allow_once' }));
  const e = prepareEvent({ key: 'a', node_id: 'a', attempt: 1, type: 'node.waiting', data: { reason: 'permission', session_id: 's', request_id: '1-1', title: 't', options } });
  assert.ok(encodedSize(e.data) <= MAX_EVENT_BYTES, `${encodedSize(e.data)} bytes`);
  assert.equal(e.data.reason, 'permission');
  assert.equal(e.data.request_id, '1-1');
  assert.ok((e.data.options_omitted as number) > 0);
  assert.equal((e.data.options as { option_id: string }[])[0].option_id, 'opt-0', 'the head of the list stays whole');
});

test('any nesting is brought under the cap, and when nothing else helps only the keep-fields stay', () => {
  const deep = { state: 'failed', a: { b: [{ c: ['<'.repeat(30_000)], d: Array.from({ length: 500 }, () => 'x'.repeat(200)) }] }, rows: [[1, 2, 3], Array.from({ length: 9000 }, () => 'y')] };
  const d = capData('run.finished', deep);
  assert.ok(encodedSize(d) <= MAX_EVENT_BYTES);
  assert.equal(d.state, 'failed');
  // A huge value under a field that must not be cut: it cannot be shrunk, so it is dropped and the rest summarised.
  const stuck = capData('run.finished', { state: 'failed', session_id: 'z'.repeat(40_000) });
  assert.ok(encodedSize(stuck) <= MAX_EVENT_BYTES);
  assert.equal(stuck.state, 'failed');
  assert.equal(stuck.truncated, true);
});

test('the engine bounds a many-file conflict end to end: node.get, run.events first-of-page and the live push', async () => {
  const h = harness({ agent: never, check: never });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck(), input: {} });
    h.engine.commit(id, { t: 'start', key: 'c' });
    const files = Array.from({ length: 200 }, (_, i) => `src/${'<'.repeat(80)}/file-${i}.go`);
    h.engine.commit(id, { t: 'fail', key: 'c', error: 'merge conflict', conflict: { branch: 'hive/wf/r/c', files, worktree: '/w' } });
    const node = call(h, 'node.get', { run_id: id, key: 'c' }) as { state: string; conflict: { files: string[]; files_omitted: number } };
    assert.equal(node.state, 'failed');
    assert.equal(node.conflict.files.length, 32);
    assert.equal(node.conflict.files_omitted, 168);
    assert.ok(encodedSize({ plugin_id: 'workflows', topic: 'node.get.reply', data: node }) <= MAX_MESSAGE_BYTES);
    const fin = h.store.events(id, 0, 100).find((e) => e.type === 'node.finished' && e.data.state === 'failed');
    assert.ok(fin && encodedSize(fin.data) <= MAX_EVENT_BYTES);
    const page = call(h, 'run.events', { run_id: id, after_seq: (fin?.seq ?? 1) - 1, limit: 1 }) as { events: unknown[] };
    assert.equal(page.events.length, 1, 'the first event of a page always fits');
    assert.ok(encodedSize({ plugin_id: 'workflows', topic: 'run.events.reply', data: page }) <= MAX_MESSAGE_BYTES);
    for (const ev of h.pushed) assert.ok(encodedSize({ plugin_id: 'workflows', topic: 'run.event', data: { run_id: id, events: [ev] } }) < 64 << 10, `a live push of ${ev.type} is under the daemon's 64 KiB`);
  } finally {
    h.close();
  }
});

// ----------------------------------------------------- 2. stop failures never escape

const eperm = (): never => {
  throw Object.assign(new Error('operation not permitted'), { code: 'EPERM' });
};

test('a timeout whose stop fails with EPERM settles the check as an error instead of crashing the plugin', { skip: process.platform === 'win32' ? 'POSIX signals' : false }, async () => {
  const rejections: unknown[] = [];
  process.on('unhandledRejection', (e: never) => void rejections.push(e));
  const h = launch({ argv: [process.execPath, '-e', 'setInterval(()=>{},1000)'], cwd: process.cwd(), timeout_s: 0.2 }, () => {}, { kill: eperm });
  try {
    const r = await h.result;
    assert.equal(r.kind, 'error');
    assert.match(r.kind === 'error' ? r.message : '', /could not stop .*operation not permitted/);
    await tick();
    await tick();
    assert.deepEqual(rejections, [], 'no unhandled rejection');
  } finally {
    process.kill(-(h.pid as number), 'SIGKILL');
  }
});

test('cancelling a run whose node cannot be stopped logs it and does not throw into the event loop', async () => {
  const rejections: unknown[] = [];
  process.on('unhandledRejection', (e: never) => void rejections.push(e));
  const h = harness({ agent: never, check: never });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck(), input: {} });
    h.engine.commit(id, { t: 'start', key: 'c' });
    h.engine.onStop(id, 'c', async () => eperm());
    h.engine.cancelRun(id);
    await tick();
    await tick();
    assert.equal(runState(h, id), 'cancelled');
    assert.deepEqual(rejections, []);
  } finally {
    h.close();
  }
});

// ------------------------------------------- 3. a stale launch cannot fail a retry

test('a cancel and retry while a check is being stopped does not fail the retried attempt', async () => {
  let release: () => void = () => {};
  let calls = 0;
  const fake = (): CheckHandle => {
    if (calls++ > 0) return { pid: undefined, pgid: undefined, proc_start: undefined, result: new Promise(() => {}), stop: async () => {} };
    // The first launch has no readable start time, so the executor stops it, and that stop is slow.
    return { pid: 4242, pgid: 4242, proc_start: undefined, result: new Promise(() => {}), stop: () => new Promise<void>((r) => (release = r)) };
  };
  const hive = new FakeHive();
  hive.replies.PREPARE_WORKTREE = (req) => ({ path: process.cwd(), branch: req.branch, base_sha: 'sha-1', head_sha: 'sha-1', reattached: false });
  const h = harness({ agent: never, check: checkExecutor(fake) }, { hive });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck(), input: {} });
    await waitFor(() => calls === 1 && typeof release === 'function' && h.hive.requests.length > 0, 'the first launch');
    await tick();
    h.engine.cancelRun(id);
    h.engine.retryRun(id);
    await waitFor(() => calls === 2, 'the retried launch');
    release(); // the old stop finishes: its failure report must be dropped
    await tick();
    await tick();
    assert.notEqual(rowOf(h, id, 'c')?.state, 'failed', 'the new attempt is untouched');
    assert.equal(runState(h, id), 'running');
  } finally {
    h.close();
  }
});

test('output from a cancelled attempt that is still dying is not recorded against the retried node', async () => {
  let emit: (text: string) => void = () => {};
  let calls = 0;
  const fake = (_spec: unknown, onOutput: (stream: 'stdout' | 'stderr', text: string) => void): CheckHandle => {
    if (calls++ === 0) emit = (t) => onOutput('stdout', t);
    return { pid: 4242, pgid: 4242, proc_start: 'x', result: new Promise(() => {}), stop: async () => {} };
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
    emit('late output from the old process');
    h.engine.flush();
    assert.equal(h.store.events(id, 0, 100).filter((e) => e.type === 'check.output').length, 0);
  } finally {
    h.close();
  }
});
