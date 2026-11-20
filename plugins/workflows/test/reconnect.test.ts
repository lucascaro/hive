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
import { type HiveConnection, start } from '../engine/main.ts';
import { simulate } from './daemon-sim.ts';
import { FakeHive, waitFor } from './harness.ts';

const conn = (h: FakeHive): HiveConnection => h as unknown as HiveConnection;

test('a closed connection is redialled, and the engine reads its running agent against the new one', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-redial-'));
  const hive = new FakeHive();
  const sim = simulate(hive, { onPrompt: () => {} });
  const second = new FakeHive();
  second.welcome = { boot_id: 'boot-1' };
  let dialled = 0;
  let gone = 0;
  const engine = await start(conn(hive), {
    dataDir: dir,
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
    await waitFor(() => dialled === 1 && !engine.disconnected, 'the redial and the re-read');
    assert.equal(gone, 0, 'the plugin did not end');
    assert.equal(engine.hive, second, 'the engine speaks on the new connection');
    assert.equal(engine.store.loadRows(id)[0].state, 'running', 'the node did not fail');
    assert.equal(sim.of('PROMPT_ACP').length, 1, 'the running turn was not sent again');
    sim.sessions[0].submit({ status: 'ok', summary: 's', data: { summary: 's' } });
    sim.sessions[0].end();
    await waitFor(() => engine.store.getRun(id)?.state === 'done', 'the run to finish on the new connection');
  } finally {
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
  const engine = await start(conn(hive), {
    dataDir: dir,
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
    engine.stopFlush();
    engine.store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('without a way to redial a closed connection ends the plugin at once', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-redial-'));
  const hive = new FakeHive();
  let gone = 0;
  const engine = await start(conn(hive), { dataDir: dir, onGone: () => gone++ });
  try {
    hive.emit('close', undefined);
    await waitFor(() => gone === 1, 'the plugin to end');
  } finally {
    engine.stopFlush();
    engine.store.close();
    rmSync(dir, { recursive: true, force: true });
  }
});
