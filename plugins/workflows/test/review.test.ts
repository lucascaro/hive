// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Regression tests for the PR #550 review findings.

import assert from 'node:assert/strict';
import { mkdtempSync, realpathSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { launch, type CheckHandle } from '../engine/check.ts';
import type { NodeContext } from '../engine/engine.ts';
import { killTree } from '../engine/killtree.ts';
import { checkExecutor } from '../engine/nodes.ts';
import { redactString } from '../engine/redact.ts';
import { workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { call, FakeHive, harness, rowOf, runState, waitFor } from './harness.ts';

const node = (script: string): string[] => [process.execPath, '-e', script];
const tmp = (): string => realpathSync(mkdtempSync(join(tmpdir(), 'wf-rev-')));
const never = (): Promise<void> => new Promise(() => {});
const posix = process.platform === 'win32' ? 'POSIX process groups' : false;

test('output pieces are bounded in UTF-8 bytes and never split a surrogate pair', async () => {
  const dir = tmp();
  try {
    const got: string[] = [];
    const r = await launch({ argv: node("process.stdout.write(('é'.repeat(60)+'\\n').repeat(150)+('x😀'.repeat(30)+'\\n').repeat(200))"), cwd: dir, timeout_s: 20 }, (_s, t) => got.push(t)).result;
    assert.equal(r.kind, 'exit');
    for (const g of got) {
      assert.ok(new TextEncoder().encode(g).length <= 8192, 'a piece is over 8 KiB of UTF-8');
      assert.ok(!/[\ud800-\udbff]$/.test(g) && !/^[\udc00-\udfff]/.test(g), 'a surrogate pair was split');
    }
    assert.equal(got.join(''), ('é'.repeat(60) + '\n').repeat(150) + ('x😀'.repeat(30) + '\n').repeat(200));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a secret that straddles the 8 KiB cut is redacted, not stored in two halves', async () => {
  const dir = tmp();
  try {
    const got: string[] = [];
    const token = `ghp_${'a'.repeat(36)}`;
    await launch({ argv: node(`process.stdout.write(('x'.repeat(100)+'\\n').repeat(81)+' '+${JSON.stringify(token)}+' done\\n')`), cwd: dir, timeout_s: 20 }, (_s, t) => got.push(t)).result;
    const all = got.join('');
    assert.ok(!all.includes('ghp'), 'no fragment of the token survives');
    assert.match(all, /\[redacted\] done\n$/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('redaction covers Authorization=, bare Bearer and Basic, and quoted token fields', () => {
  assert.equal(redactString('Authorization="Bearer abc.def.ghi.jkl"').count >= 1, true);
  assert.ok(!redactString('Authorization="Bearer abc.def.ghi.jkl"').text.includes('abc.def'));
  assert.ok(!redactString('curl -H "Bearer abcdefghijklmnop0123"').text.includes('abcdefgh'));
  assert.ok(!redactString('Basic dXNlcjpwYXNzd29yZDEyMw==').text.includes('dXNlcj'));
  assert.ok(!redactString('{"authToken": "s3cr3t-value-123"}').text.includes('s3cr3t'));
  assert.equal(redactString('Bearer tokens are fine').count, 0, 'prose is left alone');
});

test('a timeout that fires during a stop does not turn a cancel into a timeout', { skip: posix }, async () => {
  const dir = tmp();
  try {
    const h = launch({ argv: node("process.on('SIGTERM',()=>{});setInterval(()=>{},1000)"), cwd: dir, timeout_s: 0.2 }, () => {}, { graceMs: 700 });
    void h.stop();
    assert.deepEqual(await h.result, { kind: 'stopped' });
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('killTree reports EPERM as kept instead of throwing or claiming the group gone, and ESRCH as gone', async () => {
  const eperm = () => {
    throw Object.assign(new Error('nope'), { code: 'EPERM' });
  };
  assert.equal(await killTree({ pid: 4242, pgid: 4242 }, { platform: 'linux', graceMs: 5, kill: eperm }), 'kept');
  const esrch = () => {
    throw Object.assign(new Error('gone'), { code: 'ESRCH' });
  };
  assert.equal(await killTree({ pid: 4242, pgid: 4242 }, { platform: 'linux', graceMs: 5, kill: esrch }), 'gone');
});

function prepared(dir: string, extra: Record<string, unknown> = {}): FakeHive {
  const hive = new FakeHive();
  hive.replies.PREPARE_WORKTREE = (req) => ({ path: dir, branch: req.branch, base_sha: req.base === 'HEAD' ? 'sha-1' : undefined, head_sha: 'sha-1', reattached: false, ...extra });
  return hive;
}
const oneCheck = () => workflow('c', s.object({}), (g) => void g.check('c', { argv: node('process.exit(0)') }));

test('a check whose start time cannot be read is not run untracked: the node fails', async () => {
  const dir = tmp();
  let stopped = false;
  const fake = (): CheckHandle => ({ pid: 123, pgid: 123, proc_start: undefined, result: new Promise(() => {}), stop: async () => void (stopped = true) });
  const h = harness({ agent: never, check: checkExecutor(fake) }, { hive: prepared(dir) });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck(), input: {} });
    await waitFor(() => runState(h, id) === 'failed', 'the run to fail');
    assert.match(rowOf(h, id, 'c')?.error ?? '', /start time/);
    assert.equal(stopped, true, 'the process was stopped');
  } finally {
    h.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('the base is pinned as soon as the worktree exists, and a reattached worktree supplies it from its HEAD', async () => {
  const dir = tmp();
  const hive = prepared(dir, { reattached: true, base_sha: undefined });
  const h = harness({ agent: never, check: checkExecutor(() => ({ pid: undefined, pgid: undefined, proc_start: undefined, result: Promise.resolve({ kind: 'error' as const, message: 'x' }), stop: async () => {} })) }, { hive });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck(), input: {} });
    await waitFor(() => runState(h, id) === 'failed', 'the run');
    assert.equal(h.store.getRun(id)?.base_sha, 'sha-1', 'pinned from head_sha even though the check never started');
  } finally {
    h.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('check output for a node that is no longer running is dropped, so nothing follows node.finished', async () => {
  const h = harness({ agent: never, check: never });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck(), input: {} });
    const ev = { key: 'c', node_id: 'c', attempt: 1, type: 'check.output' as const, data: { stream: 'stdout', text: 'late' } };
    h.engine.buffer(id, ev); // c is pending: dropped
    h.engine.commit(id, { t: 'start', key: 'c' });
    h.engine.buffer(id, ev); // running: kept
    h.engine.commit(id, { t: 'finish', key: 'c', output: { ok: true, exit_code: 0 } });
    h.engine.buffer(id, ev); // done: dropped
    h.engine.flush();
    const lines = h.store.events(id, 0, 100).filter((e) => e.type === 'check.output');
    assert.equal(lines.length, 1);
    assert.ok(lines[0].seq < (h.store.events(id, 0, 100).find((e) => e.type === 'node.finished')?.seq ?? 0), 'the kept line precedes node.finished');
  } finally {
    h.close();
  }
});

test('a stale executor from before a retry cannot fail the retried node', async () => {
  let release: (e: Error) => void = () => {};
  let first = true;
  const executor = (ctx: NodeContext): Promise<void> => {
    if (!first) return never();
    first = false;
    return new Promise<void>((_, reject) => {
      release = reject;
      void ctx;
    });
  };
  const h = harness({ agent: never, check: executor });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck(), input: {} });
    await waitFor(() => rowOf(h, id, 'c')?.ready === true, 'the node to be launched');
    h.engine.cancelRun(id);
    h.engine.retryRun(id);
    release(new Error('late failure from the old attempt'));
    await new Promise((r) => setTimeout(r, 30));
    assert.notEqual(rowOf(h, id, 'c')?.state, 'failed', 'the new attempt is untouched');
    assert.equal(runState(h, id), 'running');
  } finally {
    h.close();
  }
});

test('runs.list pages through runs created in the same millisecond with before_id', () => {
  const h = harness({ agent: never, check: never }, { store: { now: () => new Date('2026-01-01T00:00:00.000Z') } });
  try {
    const ids = Array.from({ length: 4 }, () => h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck(), input: {} }));
    const seen: string[] = [];
    let cursor: { before: string; before_id: string } | undefined;
    for (let i = 0; i < 10; i++) {
      const r = call(h, 'runs.list', { limit: 1, ...cursor }) as { runs: { run_id: string; created_at: string }[] };
      if (r.runs.length === 0) break;
      seen.push(r.runs[0].run_id);
      cursor = { before: r.runs[0].created_at, before_id: r.runs[0].run_id };
    }
    assert.deepEqual(new Set(seen), new Set(ids));
    assert.equal(seen.length, 4, 'every run once');
  } finally {
    h.close();
  }
});

test('an error reply is bounded too: a validator with thousands of errors still fits one message', () => {
  const h = harness({ agent: never, check: never });
  try {
    const nodes = Array.from({ length: 3000 }, () => null);
    const ir = { ir: 1, name: 'x', input: { type: 'object', properties: {}, required: [], additionalProperties: false }, nodes, edges: [] };
    const r = call(h, 'test.start-run', { project_id: 'p', ir, input: {} }) as { ok: boolean; error: { message: string } };
    assert.equal(r.ok, false);
    assert.ok(new TextEncoder().encode(r.error.message).length <= 4 << 10);
  } finally {
    h.close();
  }
});
