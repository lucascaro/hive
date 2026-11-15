// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The checkpoint store (spec 529, Store): atomic row-plus-event commits,
// gapless seq, redaction, and file modes.

import assert from 'node:assert/strict';
import { mkdtempSync, rmSync, statSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import type { NodeContext } from '../engine/engine.ts';
import type { Row } from '../engine/model.ts';
import { Store } from '../engine/store.ts';
import { examples } from '../tools/examples.ts';
import { call, harness, rowOf, runState, scripted, tick, waitFor } from './harness.ts';

const tmp = (): string => mkdtempSync(join(tmpdir(), 'wf-store-'));
const row = (key: string, state: Row['state'], extra: Partial<Row> = {}): Row => ({ key, node_id: key, state, attempt: 1, ready: true, progress: 0, turns: 0, ...extra });
const newRun = { run_id: 'r1', project_id: 'p', workflow: 'w', file: 'w.ts', source_hash: 'h', ir: '{}', input: '{}' };
const started = { key: null, node_id: null, attempt: 0, type: 'run.started' as const, data: {} };

test('seq is gapless across commits and interleaved buffered events', async () => {
  const h = harness(scripted(() => ({ summary: 's' })));
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'f.ts', ir: examples.fanOutThenVerify(), input: { goal: 'g' } });
    // Buffered output lines interleave with transitions; seq is assigned at commit.
    for (let i = 0; i < 20; i++) {
      h.store.buffer(id, { key: 'work/verify', node_id: 'work/verify', attempt: 1, type: 'check.output', data: { stream: 'stdout', text: `line ${i}` } });
      if (i % 5 === 0) await tick();
    }
    await waitFor(() => runState(h, id) === 'done', 'run to finish');
    h.engine.flush();
    const seqs = h.store.events(id, 0, 10_000).map((e) => e.seq);
    assert.ok(seqs.length > 30);
    assert.deepEqual(seqs, seqs.map((_, i) => i + 1), 'seq runs 1..N with no gap and no repeat');
    assert.equal(h.store.getRun(id)?.last_seq, seqs.length, 'the run row reflects the last event');
  } finally {
    h.close();
  }
});

test('every done row has its output, raw and redacted', async () => {
  const h = harness(scripted(() => ({ summary: 's', steps: ['a'], tasks: ['t1', 't2'], verdict: 'approve', feedback: 'f' })));
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'f.ts', ir: examples.fanOutThenVerify(), input: { goal: 'g' } });
    await waitFor(() => runState(h, id) === 'done', 'run to finish');
    const rows = h.store.loadRows(id);
    const leaves = rows.filter((r) => r.state === 'done' && r.node_id !== 'work');
    assert.ok(leaves.length >= 4);
    for (const r of leaves) {
      assert.notEqual(r.output_raw, undefined, `${r.key} has output_raw`);
      assert.notEqual(r.output, undefined, `${r.key} has output`);
    }
  } finally {
    h.close();
  }
});

test('runs.db, its WAL and shm are 0600 after real writes', { skip: process.platform === 'win32' ? 'POSIX modes do not apply on Windows; the data dir ACL does' : false }, () => {
  const dir = tmp();
  const store = new Store(dir);
  try {
    store.createRun(newRun, [row('a', 'pending')], [started]);
    store.commit('r1', { rows: [row('a', 'running')], events: [] });
    for (const f of ['runs.db', 'runs.db-wal', 'runs.db-shm']) assert.equal(statSync(join(dir, f)).mode & 0o777, 0o600, f);
  } finally {
    store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a crash inside a commit, after the rows and before the events, leaves the last committed state', () => {
  const dir = tmp();
  try {
    const child = spawnSync(process.execPath, [fileURLToPath(new URL('./crash-child.ts', import.meta.url)), dir], { encoding: 'utf8' });
    assert.equal(child.signal, 'SIGKILL', `the child died mid-commit (stdout: ${child.stdout}, stderr: ${child.stderr})`);
    const store = new Store(dir);
    try {
      const a = store.loadRows('r1').find((r) => r.key === 'a') as Row;
      assert.equal(a.state, 'running', 'the half-written transition is gone: the row is as the first commit left it');
      assert.equal(a.output_raw, undefined);
      const evs = store.events('r1', 0, 100);
      assert.deepEqual(evs.map((e) => e.type), ['run.started', 'node.started']);
      assert.equal(store.getRun('r1')?.last_seq, 2, 'last_seq is not ahead of the rows');
      assert.equal(store.getRun('r1')?.state, 'running');
      const page = store.nodePage('r1', undefined, 10);
      assert.deepEqual([page.last_seq, page.rows.map((r) => r.state)], [2, ['running']]);
    } finally {
      store.close();
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a failure inside a commit rolls back the rows and the events together', () => {
  const dir = tmp();
  let boom = false;
  const store = new Store(dir, {
    afterRows: () => {
      if (boom) throw new Error('disk on fire');
    },
  });
  try {
    store.createRun(newRun, [row('a', 'pending', { attempt: 0 })], [started]);
    boom = true;
    assert.throws(
      () => store.commit('r1', { rows: [row('a', 'done', { output: { x: 1 } })], events: [{ key: 'a', node_id: 'a', attempt: 1, type: 'node.finished', data: { state: 'done' } }], run: { state: 'done' } }),
      /disk on fire/,
    );
    boom = false;
    assert.equal(store.loadRows('r1')[0].state, 'pending');
    assert.equal(store.events('r1', 0, 10).length, 1);
    assert.deepEqual([store.getRun('r1')?.last_seq, store.getRun('r1')?.state], [1, 'running']);
    // The store is still usable and seq did not skip.
    const ev = store.commit('r1', { rows: [], events: [{ key: null, node_id: null, attempt: 0, type: 'run.retried', data: { reset: 0 } }] });
    assert.equal(ev[0].seq, 2);
  } finally {
    store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('events are redacted before insert, with a count, and the stored copy is what is served', () => {
  const dir = tmp();
  const store = new Store(dir);
  try {
    store.createRun(newRun, [row('a', 'running')], [started]);
    store.commit('r1', {
      rows: [],
      events: [{ key: 'a', node_id: 'a', attempt: 1, type: 'check.output', data: { stream: 'stdout', text: 'key AKIAABCDEFGHIJKLMNOP and ghp_' + 'a'.repeat(36) } }],
    });
    const [ev] = store.events('r1', 1, 10);
    assert.equal(ev.data.text, 'key [redacted] and [redacted]');
    assert.equal(ev.data.redacted, 2);
  } finally {
    store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('run.retry resets are visible through run.nodes after the plugin restarts', async () => {
  const dir = tmp();
  // The first node fails; after the retry every node is left pending, as if the plugin died at that moment.
  let calls = 0;
  const once = scripted(() => ({ fail: 'boom' }), () => ({ fail: 'boom' }));
  const parked = async (ctx: NodeContext) => (calls++ === 0 ? once.agent(ctx) : new Promise<void>(() => {}));
  const first = harness({ agent: parked, check: parked }, { dir });
  let id = '';
  try {
    id = first.engine.startRunFromIR({ project_id: 'p', file: 'f.ts', ir: examples.reviewLoop(), input: { task: 't' } });
    await waitFor(() => runState(first, id) === 'failed', 'run to fail');
    assert.equal(call(first, 'run.retry', { run_id: id }).ok, true);
    first.engine.stopFlush();
  } finally {
    first.store.close();
  }
  // A new process: nothing in memory.
  const second = harness({ agent: () => new Promise<void>(() => {}), check: () => new Promise<void>(() => {}) }, { dir });
  try {
    const page = call(second, 'run.nodes', { run_id: id }) as { nodes: { key: string; state: string; attempt: number }[]; last_seq: number };
    const work = page.nodes.find((n) => n.key === 'fix[1]/work');
    assert.deepEqual([work?.state, work?.attempt], ['pending', 0], 'the reset row, not the failure, is what a client reads');
    const all = second.store.events(id, 0, 1000);
    const at = all.findIndex((e) => e.type === 'run.retried');
    const tail = all.slice(at - 2, at + 1);
    assert.deepEqual(tail.map((e) => [e.type, e.key]), [['node.reset', 'fix'], ['node.reset', 'fix[1]/work'], ['run.retried', null]]);
    assert.deepEqual(tail[2].data, { reset: 2 });
    assert.equal(rowOf(second, id, 'fix[1]/work')?.state, 'pending');
    assert.equal(page.last_seq, second.store.getRun(id)?.last_seq);
  } finally {
    second.close();
    rmSync(dir, { recursive: true, force: true });
  }
});
