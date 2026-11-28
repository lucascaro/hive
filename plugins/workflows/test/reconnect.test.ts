// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The plugin's entry across a lost connection (spec 529, B3c): it dials again
// and reads its nodes against the new connection, and ends only when the daemon
// cannot be reached.

import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { agentExecutor } from '../engine/agent.ts';
import { type HiveConnection, start } from '../engine/main.ts';
import { simulate } from './daemon-sim.ts';
import { workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { FakeHive, harness, waitFor } from './harness.ts';

const conn = (h: FakeHive): HiveConnection => h as unknown as HiveConnection;

test('a closed connection is redialled, and the engine reads its running agent against the new one', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-redial-'));
  const hive = new FakeHive();
  const sim = simulate(hive, { onPrompt: () => {} });
  const second = new FakeHive();
  second.welcome = { boot_id: 'boot-1' };
  let dialled = 0;
  let gone = 0;
  let reads = 0; // redialled connections whose nodes have been read
  const engine = await start(conn(hive), {
    dataDir: dir,
    onReconnected: () => reads++,
    testSeam: true,
    redial: async () => {
      dialled++;
      sim.attach(second);
      return conn(second);
    },
    onGone: () => gone++,
    redialDelayMs: 1,
  });
  try {
    const id = engine.startRunFromIR({
      project_id: 'p1',
      file: 'w.ts',
      ir: { ir: 1, name: 'w', input: { type: 'object', properties: {}, required: [], additionalProperties: false }, nodes: [{ id: 'a', kind: 'agent', agent: 'claude', worktree: 'own', timeout_s: 60, prompt: 'do it', output: { type: 'object', properties: { summary: { type: 'string' } }, required: ['summary'], additionalProperties: false } }], edges: [] } as never,
      input: {},
    });
    await waitFor(() => engine.store.loadRows(id)[0].prompt_id !== undefined, 'the turn to start');
    hive.emit('close', undefined); // the daemon closed us: the fan-out overflowed
    const pushedBefore = hive.posts.filter((x) => x.topic === 'run.event').length;
    await waitFor(() => reads === 1, 'the redial and the re-read');
    assert.equal(gone, 0, 'the plugin did not end');
    assert.equal(engine.hive, second, 'the engine speaks on the new connection');
    assert.equal(engine.store.loadRows(id)[0].state, 'running', 'the node did not fail');
    assert.equal(sim.of('PROMPT_ACP').length, 1, 'the running turn was not sent again');
    sim.sessions[0].submit({ status: 'ok', summary: 's', data: { summary: 's' } });
    sim.sessions[0].end();
    await waitFor(() => engine.store.getRun(id)?.state === 'done', 'the run to finish on the new connection');
    // One watcher owns the turn: the one the lost connection left is gone, so the result is committed and recorded once.
    assert.equal(sim.of('COMMIT_WORKTREE').length, 1);
    assert.equal(engine.store.events(id, 0, 500).filter((e) => e.key === 'a' && e.type === 'node.finished').length, 1);
    // Live updates follow the connection: the GUI hears the node finish on the replacement, and nothing more goes to the closed one.
    const pushed = (h: FakeHive): string[] => h.posts.filter((x) => x.topic === 'run.event').flatMap((x) => ((x.data as { events: { type: string }[] }).events ?? []).map((e) => e.type));
    assert.ok(pushed(second).includes('node.finished'), 'run.event reached the replacement connection');
    assert.equal(hive.posts.filter((x) => x.topic === 'run.event').length, pushedBefore, 'and none went to the closed one');
  } finally {
    engine.disconnect(); // nothing a launch still has in flight may reach the store once it is closed
    engine.stopFlush();
    engine.store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('when the daemon cannot be reached the plugin ends, as it always did', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-redial-'));
  const hive = new FakeHive();
  let dialled = 0;
  let gone = 0;
  let reads = 0; // redialled connections whose nodes have been read
  const engine = await start(conn(hive), {
    dataDir: dir,
    onReconnected: () => reads++,
    redial: async () => {
      dialled++;
      throw new Error('connect ENOENT');
    },
    onGone: () => gone++,
    redialDelayMs: 1,
  });
  try {
    hive.emit('close', undefined);
    await waitFor(() => gone === 1, 'the plugin to give up');
    assert.equal(dialled, 3, 'a few tries, then the daemon starts a fresh copy');
  } finally {
    engine.disconnect(); // nothing a launch still has in flight may reach the store once it is closed
    engine.stopFlush();
    engine.store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('without a way to redial a closed connection ends the plugin at once', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-redial-'));
  const hive = new FakeHive();
  let gone = 0;
  let reads = 0; // redialled connections whose nodes have been read
  const engine = await start(conn(hive), { dataDir: dir,
    onReconnected: () => reads++, onGone: () => gone++ });
  try {
    hive.emit('close', undefined);
    await waitFor(() => gone === 1, 'the plugin to end');
  } finally {
    engine.disconnect(); // nothing a launch still has in flight may reach the store once it is closed
    engine.stopFlush();
    engine.store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

// ------------------------------------------------- a close while reading

const IR = { ir: 1, name: 'w', input: { type: 'object', properties: {}, required: [], additionalProperties: false }, nodes: [{ id: 'a', kind: 'agent', agent: 'claude', worktree: 'own', timeout_s: 60, prompt: 'do it', output: { type: 'object', properties: { summary: { type: 'string' } }, required: ['summary'], additionalProperties: false } }], edges: [] } as never;

/** A connection the daemon closes as soon as the engine asks it anything. */
function closesOnFirstRead(c: FakeHive): void {
  c.onSend.LIST_SESSIONS = () => {
    c.request = () => Promise.reject(new Error('hive: connection closed'));
    c.send = () => {
      throw new Error('write after end');
    };
    c.emit('close', undefined);
  };
}

test('a close of the redialled connection while its nodes are read is dialled again, and the surviving turn is not touched', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-redial-'));
  const hive = new FakeHive();
  const sim = simulate(hive, { onPrompt: () => {} });
  const conns: FakeHive[] = [];
  let gone = 0;
  let reads = 0; // redialled connections whose nodes have been read
  const engine = await start(conn(hive), {
    dataDir: dir,
    onReconnected: () => reads++,
    testSeam: true,
    redialDelayMs: 1,
    daemonWaitMs: 5_000, // the old engine waited this out and then interrupted the node
    onGone: () => gone++,
    redial: async () => {
      const c = new FakeHive();
      c.welcome = { boot_id: 'boot-1' };
      sim.attach(c);
      conns.push(c);
      if (conns.length === 1) closesOnFirstRead(c);
      return conn(c);
    },
  });
  try {
    const id = engine.startRunFromIR({ project_id: 'p1', file: 'w.ts', ir: IR, input: {} });
    await waitFor(() => engine.store.loadRows(id)[0].prompt_id !== undefined, 'the turn to start');
    hive.emit('close', undefined);
    await waitFor(() => reads === 1, 'the second redial and its read');
    assert.equal(gone, 0);
    assert.equal(engine.hive, conns[1], 'the engine speaks on the connection that survived');
    const r = engine.store.loadRows(id)[0];
    assert.deepEqual([r.state, r.attempt], ['running', 1], 'not interrupted, not failed');
    assert.equal(sim.of('PROMPT_ACP').length, 1);
    sim.sessions[0].submit({ status: 'ok', summary: 's', data: { summary: 's' } });
    sim.sessions[0].end();
    await waitFor(() => engine.store.getRun(id)?.state === 'done', 'the run to finish');
  } finally {
    engine.disconnect(); // nothing a launch still has in flight may reach the store once it is closed
    engine.stopFlush();
    engine.store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a close during the startup recovery does not fail the node whose turn survived', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-redial-'));
  const h0 = new FakeHive();
  const sim = simulate(h0, { onPrompt: () => {} });
  const ir = IR;
  const first = harness({ agent: agentExecutor({ readyTimeoutMs: 400, settlePollMs: 5 }), check: () => new Promise(() => {}) }, { dir, hive: h0 });
  const id = first.engine.startRunFromIR({ project_id: 'p1', file: 'w.ts', ir, input: {} });
  await waitFor(() => first.engine.store.loadRows(id)[0].prompt_id !== undefined, 'the turn');
  first.engine.disconnect();
  first.close();

  const h1 = new FakeHive();
  h1.welcome = { boot_id: 'boot-1' };
  sim.attach(h1);
  closesOnFirstRead(h1); // the daemon closes us while the startup recovery reads it
  let dials = 0;
  let gone = 0;
  let reads = 0; // redialled connections whose nodes have been read
  const engine = await start(conn(h1), {
    dataDir: dir,
    onReconnected: () => reads++,
    testSeam: true,
    redialDelayMs: 1,
    onGone: () => gone++,
    redial: async () => {
      dials++;
      await new Promise((r) => setTimeout(r, 20)); // a real dial takes a moment
      const h2 = new FakeHive();
      h2.welcome = { boot_id: 'boot-1' };
      sim.attach(h2);
      return conn(h2);
    },
  });
  try {
    await waitFor(() => reads === 1, 'the redial and the re-read');
    const r = engine.store.loadRows(id)[0];
    assert.deepEqual([r.state, r.attempt], ['running', 1], `the node did not fail: ${r.error ?? ''}`);
    assert.equal(sim.of('PROMPT_ACP').length, 1, 'no second turn');
    assert.equal(gone, 0);
    sim.sessions[0].submit({ status: 'ok', summary: 's', data: { summary: 's' } });
    sim.sessions[0].end();
    await waitFor(() => engine.store.getRun(id)?.state === 'done', 'the run to finish');
  } finally {
    engine.disconnect(); // nothing a launch still has in flight may reach the store once it is closed
    engine.stopFlush();
    engine.store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('connections that keep closing are redialled three times, then the plugin ends', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-redial-'));
  const hive = new FakeHive();
  const sim = simulate(hive, { onPrompt: () => {} });
  let dials = 0;
  let gone = 0;
  let reads = 0; // redialled connections whose nodes have been read
  const engine = await start(conn(hive), {
    dataDir: dir,
    onReconnected: () => reads++,
    testSeam: true,
    redialDelayMs: 1,
    onGone: () => gone++,
    redial: async () => {
      dials++;
      const c = new FakeHive();
      c.welcome = { boot_id: 'boot-1' };
      sim.attach(c);
      closesOnFirstRead(c);
      return conn(c);
    },
  });
  try {
    const id = engine.startRunFromIR({ project_id: 'p1', file: 'w.ts', ir: IR, input: {} });
    await waitFor(() => engine.store.loadRows(id)[0].prompt_id !== undefined, 'the turn to start');
    hive.emit('close', undefined);
    await waitFor(() => gone === 1, 'the plugin to give up');
    assert.equal(dials, 3, 'bounded by the redial limit, however many closes');
    assert.equal(engine.store.loadRows(id)[0].state, 'running', 'the row was never failed or interrupted: the next start reads it');
    assert.equal(engine.disconnected, true);
  } finally {
    engine.disconnect(); // nothing a launch still has in flight may reach the store once it is closed
    engine.stopFlush();
    engine.store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a failure from a launch the connection outlived is refused, whenever it arrives', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-redial-'));
  const hive = new FakeHive();
  const sim = simulate(hive, { onPrompt: () => {} });
  const h2 = new FakeHive();
  h2.welcome = { boot_id: 'boot-1' };
  sim.attach(h2);
  let reads = 0; // redialled connections whose nodes have been read
  const engine = await start(conn(hive), { dataDir: dir,
    onReconnected: () => reads++, testSeam: true, redial: async () => conn(h2), redialDelayMs: 1 });
  try {
    const id = engine.startRunFromIR({ project_id: 'p1', file: 'w.ts', ir: IR, input: {} });
    await waitFor(() => engine.store.loadRows(id)[0].prompt_id !== undefined, 'the turn to start');
    const stale = engine.link;
    hive.emit('close', undefined);
    await waitFor(() => reads === 1, 'the reconnect');
    assert.equal(engine.failNode(id, 'a', 'hive: connection closed', { link: stale }), false, 'the stale launch cannot fail the node a newer launch owns');
    assert.equal(engine.store.loadRows(id)[0].state, 'running');
    assert.equal(engine.failNode(id, 'a', 'a current failure', { link: engine.link }), true);
  } finally {
    engine.disconnect(); // nothing a launch still has in flight may reach the store once it is closed
    engine.stopFlush();
    engine.store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a connection that dies right after every read is redialled with growing pauses, and only ten times in a row', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-redial-'));
  const hive = new FakeHive();
  const sim = simulate(hive, { onPrompt: () => {} });
  const dialled: number[] = [];
  let gone = 0;
  let reads = 0; // redialled connections whose nodes have been read
  const engine = await start(conn(hive), {
    dataDir: dir,
    onReconnected: () => reads++,
    testSeam: true,
    redialDelayMs: 4,
    onGone: () => gone++,
    redial: async () => {
      dialled.push(Date.now());
      const c = new FakeHive();
      c.welcome = { boot_id: 'boot-1' };
      sim.attach(c);
      return conn(c);
    },
  });
  try {
    const id = engine.startRunFromIR({ project_id: 'p1', file: 'w.ts', ir: IR, input: {} });
    await waitFor(() => engine.store.loadRows(id)[0].prompt_id !== undefined, 'the turn to start');
    let current: FakeHive = hive;
    for (let i = 0; i < 12 && gone === 0; i++) {
      current.emit('close', undefined);
      // each redial survives its read; the next close comes right after it
      await waitFor(() => gone === 1 || reads === i + 1, 'the redial and its read', 10_000);
      if (gone === 0) current = engine.hive as unknown as FakeHive;
    }
    assert.equal(gone, 1, 'the plugin gave up');
    assert.equal(dialled.length, 10, 'ten redials in a row, however many closes');
    const gaps = dialled.slice(1).map((t, i) => t - dialled[i]);
    assert.ok(gaps[gaps.length - 1] >= 500 && gaps[gaps.length - 1] > gaps[0], `the pauses grow: 4 ms doubling, so the last is about 1 s (${gaps.join(', ')} ms)`);
    assert.equal(engine.store.loadRows(id)[0].state, 'running', 'never failed or interrupted');
  } finally {
    engine.disconnect(); // nothing a launch still has in flight may reach the store once it is closed
    engine.stopFlush();
    engine.store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a connection that stays open clears the streak', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-redial-'));
  const hive = new FakeHive();
  const sim = simulate(hive, { onPrompt: () => {} });
  let dials = 0;
  let gone = 0;
  let reads = 0; // redialled connections whose nodes have been read
  const engine = await start(conn(hive), {
    dataDir: dir,
    onReconnected: () => reads++,
    testSeam: true,
    redialDelayMs: 1,
    stableMs: 5,
    onGone: () => gone++,
    redial: async () => {
      dials++;
      const c = new FakeHive();
      c.welcome = { boot_id: 'boot-1' };
      sim.attach(c);
      return conn(c);
    },
  });
  try {
    let current: FakeHive = hive;
    for (let i = 0; i < 14; i++) {
      current.emit('close', undefined);
      await waitFor(() => reads === i + 1, 'the redial and its read');
      current = engine.hive as unknown as FakeHive;
      await new Promise((r) => setTimeout(r, 15)); // long enough to count as stable
    }
    assert.equal(gone, 0, 'a connection that lasts is not a streak');
    assert.equal(dials, 14);
  } finally {
    engine.disconnect(); // nothing a launch still has in flight may reach the store once it is closed
    engine.stopFlush();
    engine.store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a check that fails after the daemon connection was lost still fails its node: checks do not use the connection', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-redial-'));
  const hive = new FakeHive();
  const h = harness(
    {
      agent: () => new Promise(() => {}),
      check: async (ctx) => {
        ctx.engine.beginNode(ctx.run_id, ctx.key); // the check is running
        await new Promise((r) => setTimeout(r, 20));
        throw new Error('the check could not run');
      },
    },
    { dir, hive },
  );
  try {
    const ir = workflow('c', s.object({}), (g) => void g.check('c', { argv: ['true'], timeoutSec: 5 }));
    const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'w.ts', ir, input: {} });
    h.engine.disconnect(); // the connection is lost while the check runs
    await waitFor(() => h.store.getRun(id)?.state === 'failed', 'the check to fail its node');
  } finally {
    h.close();
    rmSync(dir, { recursive: true, force: true });
  }
});
