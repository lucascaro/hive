// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The stored IR is checked again before every resume and retry (spec 529,
// criterion 2): the engine that wrote it may not be the engine reading it.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { call, harness, runState } from './harness.ts';

const stuck = (): Promise<void> => new Promise(() => {});

test('a stored IR that no longer validates is refused at resume and at retry, not run', async () => {
  const h = harness({ agent: stuck, check: stuck });
  try {
    // An IR with no nodes: shape accepts it, validate does not.
    const ir = { ir: 1, name: 'w', input: { type: 'object', properties: {}, required: [], additionalProperties: false }, nodes: [], edges: [] };
    h.store.createRun({ run_id: 'r1', project_id: 'p', workflow: 'w', file: 'w.ts', source_hash: 'h', ir: JSON.stringify(ir), input: '{}' }, [], [
      { key: null, node_id: null, attempt: 0, type: 'run.started', data: {} },
    ]);
    await h.engine.recover();
    const run = h.store.getRun('r1');
    assert.equal(run?.state, 'failed');
    assert.match(run?.error ?? '', /the stored workflow no longer validates: .*has no nodes/);
    assert.equal(h.store.events('r1', 0, 10).at(-1)?.type, 'run.finished');

    const retry = call(h, 'run.retry', { run_id: 'r1' }) as { ok: boolean; error: { code: string } };
    assert.equal(retry.ok, false);
    assert.equal(retry.error.code, 'invalid_ir');
    assert.equal(runState(h, 'r1'), 'failed', 'a refused retry changes nothing');
  } finally {
    h.close();
  }
});
