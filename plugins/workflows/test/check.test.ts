// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Check nodes (spec 529, Check and human nodes): real processes, in their own
// process groups, stopped as a group, reaped on restart only when the pid still
// is the process the engine recorded.

import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtempSync, realpathSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { checkEnv, launch } from '../engine/check.ts';
import { alive, killTree, procStart, reap } from '../engine/killtree.ts';
import { checkExecutor } from '../engine/nodes.ts';
import { workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { call, FakeHive, harness, rowOf, runState, waitFor } from './harness.ts';

const posix = process.platform === 'win32' ? 'POSIX process groups' : false;
const node = (script: string): string[] => [process.execPath, '-e', script];
const quiet = (): void => {};
const tmp = (): string => realpathSync(mkdtempSync(join(tmpdir(), 'wf-check-')));
const sleepForever = 'setInterval(()=>{},1000)';

test('a check reports ok and the exit code; a non-zero exit is an outcome, not an error', async () => {
  const dir = tmp();
  try {
    assert.deepEqual(await launch({ argv: node('process.exit(0)'), cwd: dir, timeout_s: 20 }, quiet).result, { kind: 'exit', ok: true, exit_code: 0 });
    assert.deepEqual(await launch({ argv: node('process.exit(3)'), cwd: dir, timeout_s: 20 }, quiet).result, { kind: 'exit', ok: false, exit_code: 3 });
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a command that cannot start is an error with a reason, and has no pid', async () => {
  const dir = tmp();
  try {
    const h = launch({ argv: ['definitely-not-a-binary-9f3a'], cwd: dir, timeout_s: 20 }, quiet);
    assert.equal(h.pid, undefined);
    const r = await h.result;
    assert.equal(r.kind, 'error');
    assert.match(r.kind === 'error' ? r.message : '', /could not start definitely-not-a-binary-9f3a/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('output is coalesced in pieces of at most 8 KiB per stream, and a multi-byte character is never split', async () => {
  const dir = tmp();
  try {
    const got: { stream: string; text: string }[] = [];
    const script = "process.stdout.write('é'.repeat(10000)); process.stderr.write('warn'); process.stdout.write('tail')";
    const r = await launch({ argv: node(script), cwd: dir, timeout_s: 20 }, (stream, text) => got.push({ stream, text })).result;
    assert.equal(r.kind, 'exit');
    const out = got.filter((g) => g.stream === 'stdout');
    assert.equal(out.map((g) => g.text).join(''), `${'é'.repeat(10000)}tail`);
    assert.ok(out.every((g) => g.text.length <= 8192), 'no piece is over 8 KiB');
    assert.ok(out.length >= 2);
    assert.equal(got.filter((g) => g.stream === 'stderr').map((g) => g.text).join(''), 'warn');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a check gets PATH, LANG, TZ and HOME and nothing else', async () => {
  assert.deepEqual(checkEnv({ PATH: '/bin', LANG: 'C', TZ: 'UTC', HOME: '/h', AWS_SECRET_ACCESS_KEY: 'x', HIVE_SOCKET: '/s' }), { PATH: '/bin', LANG: 'C', TZ: 'UTC', HOME: '/h' });
  const dir = tmp();
  try {
    let text = '';
    await launch({ argv: node('console.log(JSON.stringify(Object.keys(process.env)))'), cwd: dir, timeout_s: 20 }, (_s, t) => (text += t), { env: { PATH: process.env.PATH, HOME: '/h', SECRET: 'nope' } }).result;
    const keys = JSON.parse(text) as string[];
    assert.ok(keys.includes('PATH') && keys.includes('HOME'));
    assert.ok(!keys.includes('SECRET'), 'the parent environment is not inherited');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a timeout sends TERM to the group and KILL after the grace, and a grandchild dies too', { skip: posix }, async () => {
  const dir = tmp();
  try {
    // Parent and grandchild both ignore TERM, so only the KILL after the grace can end them.
    const script = `const {spawn}=require('node:child_process');process.on('SIGTERM',()=>{});
      const g=spawn(process.execPath,['-e',"process.on('SIGTERM',()=>{});setInterval(()=>{},1000)"],{stdio:'ignore'});
      console.log(g.pid);setInterval(()=>{},1000)`;
    let out = '';
    const t0 = Date.now();
    const h = launch({ argv: node(script), cwd: dir, timeout_s: 0.4 }, (_s, t) => (out += t), { graceMs: 400 });
    const r = await h.result;
    const grand = Number(out.trim());
    assert.equal(r.kind, 'timeout');
    assert.ok(Date.now() - t0 >= 700, 'the KILL waited out the grace');
    assert.ok(grand > 0 && h.pid !== undefined);
    assert.equal(alive(h.pid as number), false, 'the leader is dead');
    assert.equal(alive(grand), false, 'its grandchild, in the same group, is dead');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('TERM alone ends a check that honours it, well inside the grace', { skip: posix }, async () => {
  const dir = tmp();
  try {
    const h = launch({ argv: node(sleepForever), cwd: dir, timeout_s: 60 }, quiet, { graceMs: 5000 });
    const t0 = Date.now();
    await h.stop();
    assert.deepEqual(await h.result, { kind: 'stopped' });
    assert.ok(Date.now() - t0 < 3000);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('windows stops a tree with taskkill /T, then /T /F after the grace, hidden', async () => {
  const calls: { cmd: string; args: readonly string[]; windowsHide?: boolean }[] = [];
  const fake = ((cmd: string, args: readonly string[], opts: { windowsHide?: boolean }) => {
    calls.push({ cmd, args, windowsHide: opts.windowsHide });
    return { on: (ev: string, fn: () => void) => ev === 'close' && setImmediate(fn) };
  }) as unknown as typeof spawn;
  await killTree({ pid: 123 }, { platform: 'win32', graceMs: 5, spawn: fake });
  assert.deepEqual(
    calls.map((c) => [c.cmd, ...c.args]),
    [['taskkill', '/T', '/PID', '123'], ['taskkill', '/T', '/F', '/PID', '123']],
  );
  assert.ok(calls.every((c) => c.windowsHide === true), 'no console window per spawn');
});

test('startup reaps a recorded group only when the start time matches', { skip: posix }, async () => {
  const dir = tmp();
  try {
    const h = launch({ argv: node(sleepForever), cwd: dir, timeout_s: 120 }, quiet);
    const pid = h.pid as number;
    assert.ok(h.proc_start, 'the start time is read at launch');
    // The same pid with another start time is a stranger (a reused pid): left alone.
    assert.equal(await reap({ pid, pgid: h.pgid, proc_start: 'Thu Jan  1 00:00:00 1970' }, { graceMs: 200 }), 'stranger');
    assert.equal(alive(pid), true, 'a reused pid is never killed');
    assert.equal(procStart(pid), h.proc_start, 'and its start time is stable');
    assert.equal(await reap({ pid, pgid: h.pgid, proc_start: h.proc_start }, { graceMs: 200 }), 'killed');
    await h.result;
    assert.equal(alive(pid), false);
    assert.equal(await reap({ pid, pgid: h.pgid, proc_start: h.proc_start }), 'gone');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// ----------------------------------------------------- the engine, a real check

function prepared(dir: string): FakeHive {
  const hive = new FakeHive();
  let n = 0;
  hive.replies.PREPARE_WORKTREE = (req) => ({ path: dir, branch: req.branch, base_sha: req.base === 'HEAD' ? 'sha-1' : undefined, reattached: false, head_sha: 'sha-1', merged: [], seq: n++ });
  return hive;
}
const never = (): Promise<void> => new Promise(() => {});
const checkOnly = { agent: never, check: checkExecutor() };
const chain = (argv: string[], timeout_s = 60) =>
  workflow('chain', s.object({}), (g) => {
    const a = g.check('a', { argv, timeoutSec: timeout_s });
    const b = g.check('b', { argv, timeoutSec: timeout_s });
    g.edge(a, b);
  });

test('a check with no upstream head runs in its own prepared worktree, on the run branch, from a pinned base', async () => {
  const dir = tmp();
  const hive = prepared(dir);
  const h = harness(checkOnly, { hive });
  try {
    h.engine.startFlush(20);
    const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'c.ts', ir: chain(node('console.log(process.cwd())')), input: {} });
    await waitFor(() => runState(h, id) === 'done', 'the run');
    const prep = hive.requests.filter((r) => r.name === 'PREPARE_WORKTREE');
    assert.equal(prep.length, 2);
    assert.deepEqual(prep[0].payload, { project_id: 'p1', workflow_run_id: id, branch: `hive/wf/${id}/a`, base: 'HEAD' }, 'the first asks for the project HEAD: PREPARE refuses an empty base');
    assert.deepEqual(prep[1].payload, { project_id: 'p1', workflow_run_id: id, branch: `hive/wf/${id}/b`, base: 'sha-1' }, 'the second is pinned to what the first resolved');
    assert.equal(h.store.getRun(id)?.base_sha, 'sha-1');
    const a = rowOf(h, id, 'a');
    assert.deepEqual(a?.output_raw, { ok: true, exit_code: 0 });
    assert.equal(a?.worktree, dir);
    assert.equal(a?.branch, `hive/wf/${id}/a`);
    assert.ok(a?.pid && a.proc_start, 'pid and start time were recorded with the node');
    h.engine.flush();
    const lines = h.store.events(id, 0, 200).filter((e) => e.type === 'check.output' && e.key === 'a');
    assert.equal(lines.map((e) => (e.data as { text: string }).text).join('').trim(), dir, 'the check ran with the prepared worktree as its cwd');
  } finally {
    h.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a check that exits non-zero finishes its node with ok false; the graph decides what that means', async () => {
  const dir = tmp();
  const h = harness(checkOnly, { hive: prepared(dir) });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'c.ts', ir: chain(node('process.exit(1)')), input: {} });
    await waitFor(() => runState(h, id) === 'done', 'the run');
    assert.deepEqual(rowOf(h, id, 'a')?.output_raw, { ok: false, exit_code: 1 });
  } finally {
    h.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a check that times out fails its node and the run, naming the timeout', async () => {
  const dir = tmp();
  const h = harness(checkOnly, { hive: prepared(dir) });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'c.ts', ir: chain(node(sleepForever), 0.3), input: {} });
    await waitFor(() => runState(h, id) === 'failed', 'the run to fail');
    assert.equal(rowOf(h, id, 'a')?.state, 'failed');
    assert.match(rowOf(h, id, 'a')?.error ?? '', /timed out after 0\.3s/);
    assert.equal(rowOf(h, id, 'b')?.state, 'pending', 'what never ran stays pending');
  } finally {
    h.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a worktree that cannot be prepared fails the node with the reason', async () => {
  const hive = new FakeHive();
  hive.replies.PREPARE_WORKTREE = () => {
    throw new Error('git is on fire');
  };
  const h = harness(checkOnly, { hive });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'c.ts', ir: chain(node('0')), input: {} });
    await waitFor(() => runState(h, id) === 'failed', 'the run to fail');
    assert.match(rowOf(h, id, 'a')?.error ?? '', /git is on fire/);
  } finally {
    h.close();
  }
});

test('run.cancel stops a running check and its group', { skip: posix }, async () => {
  const dir = tmp();
  const h = harness(checkOnly, { hive: prepared(dir) });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'c.ts', ir: chain(node(sleepForever)), input: {} });
    await waitFor(() => rowOf(h, id, 'a')?.state === 'running', 'the check to run');
    const pid = rowOf(h, id, 'a')?.pid as number;
    assert.equal(call(h, 'run.cancel', { run_id: id }).ok, true);
    assert.equal(rowOf(h, id, 'a')?.state, 'cancelled');
    assert.equal(runState(h, id), 'cancelled');
    await waitFor(() => !alive(pid), 'the process to die');
  } finally {
    h.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

// ------------------------------------------- a plugin crash, and a hived restart

async function crashedEngine(dir: string, marker: string, worktree: string): Promise<{ id: string; pid: number }> {
  const child = spawn(process.execPath, [fileURLToPath(new URL('./check-child.ts', import.meta.url)), dir, marker, worktree], { stdio: ['ignore', 'pipe', 'pipe'] });
  let buf = '';
  let err = '';
  child.stderr?.on('data', (c: Uint8Array) => (err += new TextDecoder().decode(c)));
  const line = await new Promise<string>((resolve, reject) => {
    child.stdout?.on('data', (c: Uint8Array) => {
      buf += new TextDecoder().decode(c);
      if (buf.includes('\n')) resolve(buf.split('\n')[0]);
    });
    child.on('close', () => reject(new Error(`the engine process ended early: ${err}`)));
  });
  const closed = new Promise<void>((r) => child.on('close', () => r()));
  child.kill('SIGKILL'); // a plugin crash: no cleanup runs
  await closed;
  return JSON.parse(line) as { id: string; pid: number };
}

test('plugin crash: the check outlives the engine, is reaped on restart, and the node is retried as attempt 2', { skip: posix }, async () => {
  const dir = tmp();
  const wt = tmp();
  const marker = join(dir, 'ran-once');
  try {
    const { id, pid } = await crashedEngine(dir, marker, wt);
    assert.equal(alive(pid), true, 'a check is in its own group: killing the engine does not kill it');
    const h = harness(checkOnly, { dir, hive: prepared(wt) });
    try {
      await h.engine.recover();
      await waitFor(() => runState(h, id) === 'done', 'the retried run');
      assert.equal(alive(pid), false, 'the survivor was reaped');
      const c = rowOf(h, id, 'c');
      assert.equal(c?.attempt, 2);
      assert.deepEqual(c?.output_raw, { ok: true, exit_code: 0 });
      const types = h.store.events(id, 0, 100).filter((e) => e.key === 'c').map((e) => `${e.type}:${e.attempt}`);
      assert.deepEqual(types.filter((t) => !t.startsWith('node.ready') && !t.startsWith('check.output')), ['node.started:1', 'node.finished:1', 'node.started:2', 'node.output:2', 'node.finished:2']);
      const interrupted = h.store.events(id, 0, 100).find((e) => e.type === 'node.finished' && e.attempt === 1);
      assert.equal(interrupted?.data.state, 'interrupted');
    } finally {
      h.close();
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
    rmSync(wt, { recursive: true, force: true });
  }
});

test('hived restart: the check is already gone, the node becomes interrupted and is retried', { skip: posix }, async () => {
  const dir = tmp();
  const wt = tmp();
  const marker = join(dir, 'ran-once');
  try {
    const { id, pid } = await crashedEngine(dir, marker, wt);
    process.kill(-pid, 'SIGKILL'); // hived took its children down with it
    await waitFor(() => !alive(pid), 'the check to die');
    const h = harness(checkOnly, { dir, hive: prepared(wt) });
    try {
      await h.engine.recover();
      await waitFor(() => runState(h, id) === 'done', 'the retried run');
      assert.equal(rowOf(h, id, 'c')?.attempt, 2);
    } finally {
      h.close();
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
    rmSync(wt, { recursive: true, force: true });
  }
});

test('shutdown terminates running checks so a normal hived stop leaves none behind', { skip: posix }, async () => {
  const dir = tmp();
  const h = harness(checkOnly, { hive: prepared(dir) });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'c.ts', ir: chain(node(sleepForever)), input: {} });
    await waitFor(() => rowOf(h, id, 'a')?.state === 'running', 'the check to run');
    const pid = rowOf(h, id, 'a')?.pid as number;
    await h.engine.shutdown(2000);
    assert.equal(alive(pid), false);
    assert.equal(rowOf(h, id, 'a')?.state, 'running', 'the row stays running: the next start reaps and retries it');
  } finally {
    h.close();
    rmSync(dir, { recursive: true, force: true });
  }
});
