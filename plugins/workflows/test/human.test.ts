// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Human nodes (spec 529): a question shown in the GUI, a typed answer, and a
// wait that survives every restart unchanged.

import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { p, workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { call, harness, rowOf, runState, scripted } from './harness.ts';

const ir = () =>
  workflow('ask', s.object({ who: s.string() }), (g, input) => {
    const ask = g.human('ask', { prompt: p`Ship it, ${input.who}?`, output: s.object({ decision: s.enum('yes', 'no'), note: s.string() }) });
    const after = g.check('after', { argv: ['true'] });
    g.edge(ask, after);
  });

const err = (r: Record<string, unknown>) => r.error as { code: string; message: string };

test('an answer that does not fit the schema is refused with the path of the problem, and the node keeps waiting', () => {
  const h = harness(scripted(() => ({})));
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'a.ts', ir: ir(), input: { who: 'Ada' } });
    const bad = call(h, 'human.answer', { run_id: id, key: 'ask', answer: { decision: 'maybe', note: 'x' } });
    assert.equal(bad.ok, false);
    assert.equal(err(bad).code, 'invalid_answer');
    assert.match(err(bad).message, /answer\.decision: "maybe" is not one of yes\|no/);
    const missing = call(h, 'human.answer', { run_id: id, key: 'ask', answer: { decision: 'yes' } });
    assert.match(err(missing).message, /answer\.note: required field is missing/);
    const extra = call(h, 'human.answer', { run_id: id, key: 'ask', answer: { decision: 'yes', note: '', other: 1 } });
    assert.match(err(extra).message, /answer\.other: unexpected field/);
    assert.equal(rowOf(h, id, 'ask')?.state, 'waiting');
    assert.equal(runState(h, id), 'waiting');
  } finally {
    h.close();
  }
});

test('answering a node that is not waiting is refused with not_waiting', () => {
  const stuck = (): Promise<void> => new Promise(() => {});
  const h = harness({ agent: stuck, check: stuck });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'a.ts', ir: ir(), input: { who: 'Ada' } });
    assert.equal(err(call(h, 'human.answer', { run_id: id, key: 'nope', answer: {} })).code, 'not_waiting');
    assert.equal(err(call(h, 'human.answer', { run_id: id, key: 'after', answer: {} })).code, 'not_waiting', 'a check is not a question');
    assert.equal(call(h, 'human.answer', { run_id: id, key: 'ask', answer: { decision: 'yes', note: '' } }).ok, true);
    assert.equal(err(call(h, 'human.answer', { run_id: id, key: 'ask', answer: { decision: 'yes', note: '' } })).code, 'not_waiting', 'answered once');
  } finally {
    h.close();
  }
});

test('a waiting human survives a restart unchanged and can still be answered', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'wf-human-'));
  try {
    const first = harness(scripted(() => ({})), { dir });
    const id = first.engine.startRunFromIR({ project_id: 'p', file: 'a.ts', ir: ir(), input: { who: 'Ada' } });
    const before = call(first, 'node.get', { run_id: id, key: 'ask' });
    const eventsBefore = first.store.events(id, 0, 100).length;
    first.store.close();

    // A new process: nothing in memory but the database.
    const second = harness(scripted(() => ({}), () => ({ ok: true, exit_code: 0 })), { dir });
    try {
      await second.engine.recover();
      const after = call(second, 'node.get', { run_id: id, key: 'ask' });
      assert.deepEqual({ ...after, nonce: undefined }, { ...before, nonce: undefined }, 'the question reads the same');
      assert.equal(second.store.events(id, 0, 100).length, eventsBefore, 'recovery wrote nothing for a waiting human');
      assert.equal((call(second, 'runs.list', { state: 'waiting' }).runs as unknown[]).length, 1);
      assert.equal(call(second, 'human.answer', { run_id: id, key: 'ask', answer: { decision: 'yes', note: 'go' } }).ok, true);
      for (let i = 0; i < 100 && runState(second, id) !== 'done'; i++) await new Promise((r) => setTimeout(r, 10));
      assert.equal(runState(second, id), 'done');
      assert.deepEqual(rowOf(second, id, 'ask')?.output_raw, { decision: 'yes', note: 'go' });
    } finally {
      second.store.close();
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
