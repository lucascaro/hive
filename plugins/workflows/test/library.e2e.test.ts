// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The three library workflows end to end through the real engine and store,
// with a scripted fake in place of agents and processes (spec 529, Tests).

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { examples } from '../tools/examples.ts';
import { call, type Harness, harness, rowOf, runState, scripted, waitFor } from './harness.ts';

type Verdicts = (key: string) => 'approve' | 'revise';

const agents =
  (verdict: Verdicts) =>
  (key: string): unknown => {
    if (key.endsWith('/review')) return { verdict: verdict(key), feedback: `fb ${key}` };
    if (key.endsWith('/plan')) return { summary: 'the plan', steps: ['one', 'two'] };
    if (key === 'split') return { tasks: ['a', 'b', 'c'] };
    return { summary: `done ${key}` };
  };

async function finish(h: Harness, id: string): Promise<void> {
  await waitFor(() => ['done', 'failed', 'cancelled'].includes(runState(h, id) ?? ''), `run ${id} to finish`);
}

const start = (h: Harness, name: string, input: Record<string, unknown>): string =>
  h.engine.startRunFromIR({ project_id: 'p1', file: `${name}.ts`, ir: examples[name](), input });

test('reviewLoop converges on approve', async () => {
  const h = harness(scripted(agents((k) => (k.includes('[2]') ? 'approve' : 'revise'))));
  try {
    const id = start(h, 'reviewLoop', { task: 'fix it' });
    await finish(h, id);
    assert.equal(runState(h, id), 'done');
    assert.deepEqual(rowOf(h, id, 'fix')?.output_raw, { converged: true, iterations: 2 });
    assert.equal(rowOf(h, id, 'fix[2]/review')?.state, 'done');
    assert.equal(rowOf(h, id, 'fix[3]/review'), undefined, 'no third round');
  } finally {
    h.close();
  }
});

test('reviewLoop exhausts without approval and the run still finishes', async () => {
  const h = harness(scripted(agents(() => 'revise')));
  try {
    const id = start(h, 'reviewLoop', { task: 'fix it' });
    await finish(h, id);
    assert.equal(runState(h, id), 'done');
    assert.deepEqual(rowOf(h, id, 'fix')?.output_raw, { converged: false, iterations: 3 });
  } finally {
    h.close();
  }
});

test('fanOutThenVerify runs N copies then the check', async () => {
  const order: string[] = [];
  const h = harness(
    scripted(
      (k) => {
        order.push(k);
        return agents(() => 'approve')(k);
      },
      (k) => {
        order.push(k);
        return { ok: true, exit_code: 0 };
      },
    ),
  );
  try {
    const id = start(h, 'fanOutThenVerify', { goal: 'g' });
    await finish(h, id);
    assert.equal(runState(h, id), 'done');
    assert.deepEqual(rowOf(h, id, 'work/each')?.output_raw, { results: [{ summary: 'done work/each[0]/do' }, { summary: 'done work/each[1]/do' }, { summary: 'done work/each[2]/do' }] });
    assert.equal(order.at(-1), 'work/verify', 'the check runs after every copy');
    assert.equal(order.filter((k) => k === 'work/verify').length, 1);
  } finally {
    h.close();
  }
});

/** Waits for the node to be waiting and answers it. */
async function answer(h: Harness, id: string, key: string, answerValue: Record<string, unknown>): Promise<void> {
  await waitFor(() => rowOf(h, id, key)?.state === 'waiting', `${key} to wait`);
  const r = call(h, 'human.answer', { run_id: id, key, answer: answerValue });
  assert.equal(r.ok, true, JSON.stringify(r));
}

test('planImplementVerify: a rejected plan skips the rest', async () => {
  const h = harness(scripted(agents(() => 'approve')));
  try {
    const id = start(h, 'planImplementVerify', { spec: 's' });
    await answer(h, id, 'feature/approve', { decision: 'reject', note: 'no' });
    await finish(h, id);
    assert.equal(runState(h, id), 'done');
    for (const k of ['feature/build', 'feature/exhausted', 'feature/tests']) assert.equal(rowOf(h, id, k)?.state, 'skipped', k);
  } finally {
    h.close();
  }
});

test('planImplementVerify: an exhausted review loop asks, and run-checks routes to the tests', async () => {
  const checked: string[] = [];
  const h = harness(
    scripted(agents(() => 'revise'), (k) => {
      checked.push(k);
      return { ok: true, exit_code: 0 };
    }),
  );
  try {
    const id = start(h, 'planImplementVerify', { spec: 's' });
    await answer(h, id, 'feature/approve', { decision: 'approve', note: '' });
    await answer(h, id, 'feature/exhausted', { decision: 'run-checks', note: '' });
    await finish(h, id);
    assert.equal(runState(h, id), 'done');
    assert.deepEqual(rowOf(h, id, 'feature/build')?.output_raw, { converged: false, iterations: 2 });
    assert.deepEqual(checked, ['feature/tests']);
    // The exhausted prompt read the last round's feedback from inside the loop, from outside it.
    const waits = h.store.events(id, 0, 500).filter((e) => e.type === 'node.waiting' && e.key === 'feature/exhausted');
    assert.match(String(waits[0].data.prompt), /ran 2 rounds without approval\. Last feedback: fb feature\/build\[2\]\/review/);
  } finally {
    h.close();
  }
});

test('planImplementVerify: stop skips the checks', async () => {
  const h = harness(scripted(agents(() => 'revise')));
  try {
    const id = start(h, 'planImplementVerify', { spec: 's' });
    await answer(h, id, 'feature/approve', { decision: 'approve', note: '' });
    await answer(h, id, 'feature/exhausted', { decision: 'stop', note: '' });
    await finish(h, id);
    assert.equal(rowOf(h, id, 'feature/tests')?.state, 'skipped');
    assert.equal(runState(h, id), 'done');
  } finally {
    h.close();
  }
});
