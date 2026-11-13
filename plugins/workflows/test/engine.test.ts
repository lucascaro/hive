// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Engine behaviour that is neither pure reducer nor a topic.

import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import type { NodeContext } from '../engine/engine.ts';
import type { Row } from '../engine/model.ts';
import { Store } from '../engine/store.ts';
import { workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { harness, runState, tick } from './harness.ts';

const row = (key: string, state: Row['state'], extra: Partial<Row> = {}): Row => ({ key, node_id: key, state, attempt: 1, ready: true, progress: 0, turns: 0, ...extra });

test('a run that is about to fail launches nothing from the step that decided it', async () => {
  const started: string[] = [];
  const spy = async ({ key }: NodeContext) => {
    started.push(key);
  };
  const h = harness({ agent: spy, check: spy });
  try {
    const ir = workflow('two', s.object({}), (g) => {
      g.check('a', { argv: ['true'] });
      g.check('b', { argv: ['true'] });
    });
    // `a` was interrupted twice; `b` is ready and waiting to start. One step wants to fail `a` and start `b`.
    h.store.createRun(
      { run_id: 'r1', project_id: 'p', workflow: 'two', file: 'w.ts', source_hash: '', ir: JSON.stringify(ir), input: '{}' },
      [row('a', 'interrupted', { attempt: 2 }), row('b', 'pending', { attempt: 0 })],
      [{ key: null, node_id: null, attempt: 0, type: 'run.started', data: {} }],
    );
    await h.engine.recover();
    await tick();
    assert.equal(runState(h, 'r1'), 'failed');
    assert.deepEqual(started, [], 'b was never started: no worktree is prepared and no process spawned for a failing run');
  } finally {
    h.close();
  }
});

test('an executor that returns without starting its node fails it, instead of being launched again forever', async () => {
  const h = harness({ agent: async () => {}, check: async () => {} });
  try {
    const ir = workflow('one', s.object({}), (g) => void g.check('a', { argv: ['true'] }));
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'w.ts', ir, input: {} });
    await tick();
    await tick();
    assert.equal(runState(h, id), 'failed');
    assert.match(h.store.getRun(id)?.error ?? '', /executor returned without starting the node/);
  } finally {
    h.close();
  }
});

test('two stores on one runs.db interleave commits and seq stays gapless (BEGIN IMMEDIATE serializes them)', () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-two-'));
  const a = new Store(dir);
  const b = new Store(dir);
  try {
    a.createRun({ run_id: 'r1', project_id: 'p', workflow: 'w', file: 'w.ts', source_hash: '', ir: '{}', input: '{}' }, [row('n', 'running')], [{ key: null, node_id: null, attempt: 0, type: 'run.started', data: {} }]);
    for (let i = 0; i < 20; i++) {
      (i % 2 ? a : b).commit('r1', { rows: [], events: [{ key: 'n', node_id: 'n', attempt: 1, type: 'check.output', data: { stream: 'stdout', text: `line ${i}` } }] });
    }
    const seqs = a.events('r1', 0, 100).map((e) => e.seq);
    assert.deepEqual(seqs, Array.from({ length: 21 }, (_, i) => i + 1));
    assert.equal(b.getRun('r1')?.last_seq, 21);
  } finally {
    a.close();
    b.close();
    rmSync(dir, { recursive: true, force: true });
  }
});
