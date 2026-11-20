// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Which branch every node instance works on and starts from (spec 529,
// Worktrees): pure over the run model, then the PREPARE requests that follow.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { Row } from '../engine/model.ts';
import { branchOf, conflictMessage, headsOf, inputHeads, isConflict, prepare } from '../engine/worktrees.ts';
import { workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { examples } from '../tools/examples.ts';
import { FakeHive, harness } from './harness.ts';
import { sim } from './sim.ts';

const R = 'r1';
const sum = s.object({ summary: s.string() });

/**
 * Plays a run to the end, giving every agent a branch the way the executor does and a check one only when it merges.
 * Returns what each instance would have started from.
 */
function play(ir: ReturnType<typeof workflow>, input: Record<string, unknown>, out: (key: string) => unknown) {
  const inputs: Record<string, string[]> = {};
  const x = sim(ir, input, {
    node: (key, node, m) => {
      const heads = inputHeads(m, key);
      inputs[key] = heads;
      const row = m.rows.get(key) as Row;
      if (node.kind === 'agent' || (node.kind === 'check' && heads.length >= 2)) m.rows.set(key, { ...row, branch: branchOf(R, key), worktree: `/w/${key}` });
      return { output: out(key) };
    },
    human: () => ({ yes: true }),
  });
  x.run();
  return { x, inputs };
}

const b = (k: string): string => branchOf(R, k);

test('branch names encode the key: loop iterations and map copies become _N', () => {
  assert.equal(b('fix[2]/work'), 'hive/wf/r1/fix_2/work');
  assert.equal(b('work/each[3]/do'), 'hive/wf/r1/work/each_3/do');
  assert.equal(b('a'), 'hive/wf/r1/a');
});

test('the first node starts from the pinned base; a node after it from its branch', () => {
  const ir = workflow('seq', s.object({}), (g) => {
    const a = g.agent('a', { agent: 'claude', prompt: 'a', output: sum });
    const c = g.agent('c', { agent: 'claude', prompt: 'c', output: sum });
    g.edge(a, c);
  });
  const { inputs } = play(ir, {}, () => ({ summary: 'x' }));
  assert.deepEqual(inputs.a, [], 'none: the run base');
  assert.deepEqual(inputs.c, [b('a')]);
});

test('a loop entry starts from its own previous iteration; a reviewer in the worker\'s worktree shares its branch', () => {
  const { inputs, x } = play(examples.reviewLoop(), { task: 't' }, (k) => (k.endsWith('/review') ? { verdict: k.includes('[3]') ? 'approve' : 'revise', feedback: 'f' } : { summary: 's' }));
  assert.deepEqual(inputs['fix[1]/work'], []);
  assert.deepEqual(inputs['fix[2]/work'], [b('fix[1]/work')], 'the worker revises its own change');
  assert.deepEqual(inputs['fix[3]/work'], [b('fix[2]/work')]);
  assert.deepEqual(inputs['fix[1]/review'], [b('fix[1]/work')]);
  assert.deepEqual(inputs['fix[2]/review'], [b('fix[2]/work')], 'this round\'s change, not the last');
  // After the loop, what it ends on is its last iteration's sink.
  assert.deepEqual(headsOf(x.m, 'fix'), [b('fix[3]/review')]);
});

test('a loop entry whose previous copy did nothing falls back to the loop\'s own input', () => {
  const ir = workflow('lp', s.object({}), (g) => {
    const first = g.agent('first', { agent: 'claude', prompt: 'p', output: sum });
    const loop = g.loop('l', { maxIters: 2 }, (body) => {
      const w = body.agent('w', { agent: 'claude', prompt: 'w', output: sum });
      return { ref: `${w.id}.summary`, op: 'eq', value: 'never' };
    });
    g.edge(first, loop);
  });
  const { inputs, x } = play(ir, {}, () => ({ summary: 'x' }));
  assert.deepEqual(inputs['l[1]/w'], [b('first')]);
  assert.deepEqual(inputs['l[2]/w'], [b('l[1]/w')]);
  x.m.rows.set('l[1]/w', { ...(x.m.rows.get('l[1]/w') as Row), state: 'skipped', branch: undefined });
  assert.deepEqual(inputHeads(x.m, 'l[2]/w'), [b('first')], 'skipped copy passes its own input through');
});

test('after a map the next node merges every copy, in order', () => {
  const { inputs, x } = play(examples.fanOutThenVerify(), { goal: 'g' }, (k) => (k === 'split' ? { tasks: ['a', 'b', 'c'] } : k.endsWith('/do') ? { summary: 's' } : { ok: true, exit_code: 0 }));
  assert.deepEqual(inputs['work/each[0]/do'], [b('split')], 'every copy starts from what is upstream of the map');
  assert.deepEqual(inputs['work/each[2]/do'], [b('split')]);
  assert.deepEqual(inputs['work/verify'], [b('work/each[0]/do'), b('work/each[1]/do'), b('work/each[2]/do')]);
  assert.deepEqual(headsOf(x.m, 'work/verify'), [b('work/verify')], 'a merge worktree is a head of its own');
});

test('a check passes its input through: one head is the head, none is none', () => {
  const ir = workflow('chk', s.object({}), (g) => {
    const c1 = g.check('c1', { argv: ['true'] });
    const c2 = g.check('c2', { argv: ['true'] });
    const a = g.agent('a', { agent: 'claude', prompt: 'a', output: sum });
    const c3 = g.check('c3', { argv: ['true'] });
    g.edge(c1, c2);
    g.edge(c2, a);
    g.edge(a, c3);
  });
  const { inputs, x } = play(ir, {}, (k) => (k.startsWith('c') ? { ok: true, exit_code: 0 } : { summary: 'x' }));
  assert.deepEqual(inputs.c2, [], 'a root check\'s base worktree is nobody\'s head (B-4): no agent upstream');
  assert.deepEqual(inputs.a, []);
  assert.deepEqual(inputs.c3, [b('a')], 'a check after an agent runs in the agent\'s worktree');
  assert.deepEqual(headsOf(x.m, 'c3'), [b('a')]);
});

test('a human passes its input through', () => {
  const ir = workflow('h', s.object({}), (g) => {
    const a = g.agent('a', { agent: 'claude', prompt: 'a', output: sum });
    const h = g.human('h', { prompt: 'ok?', output: s.object({ yes: s.boolean() }) });
    const c = g.agent('c', { agent: 'claude', prompt: 'c', output: sum });
    g.edge(a, h);
    g.edge(h, c);
  });
  const { inputs } = play(ir, {}, (k) => (k === 'h' ? { yes: true } : { summary: 'x' }));
  assert.deepEqual(inputs.c, [b('a')]);
});

test('layers of checks that each depend on both checks before them are not walked once per path', () => {
  const ir = workflow('deep', s.object({}), (g) => {
    const a = g.agent('a', { agent: 'claude', prompt: 'a', output: sum });
    let prev: { id: string }[] = [a];
    for (let i = 0; i < 40; i++) {
      const layer = [g.check(`x${i}`, { argv: ['true'] }), g.check(`y${i}`, { argv: ['true'] })];
      for (const from of prev) for (const to of layer) g.edge(from, to);
      prev = layer;
    }
  });
  const { inputs } = play(ir, {}, (k) => (k === 'a' ? { summary: 'x' } : { ok: true, exit_code: 0 }));
  assert.deepEqual(inputs.x39, [b('a')]);
});

// -------------------------------------------------------------------- PREPARE

function engineWith(ir: ReturnType<typeof workflow>, hive: FakeHive, input: Record<string, unknown> = {}) {
  const h = harness({ agent: () => new Promise(() => {}), check: () => new Promise(() => {}) }, { hive });
  const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'w.ts', ir, input });
  return { h, id };
}

const prepared = (hive: FakeHive, extra: (req: Record<string, unknown>) => Record<string, unknown> = () => ({})) => {
  hive.replies.PREPARE_WORKTREE = (req) => ({ path: `/wt/${req.branch}`, branch: req.branch, base_sha: req.base === 'HEAD' ? 'sha-1' : undefined, head_sha: 'sha-1', reattached: false, merged: [], ...extra(req) });
};

test('PREPARE: the root asks for HEAD and pins what came back; a later node branches from its upstream; a join merges the rest', async () => {
  const hive = new FakeHive();
  prepared(hive);
  const ir = workflow('j', s.object({}), (g) => {
    const a = g.agent('a', { agent: 'claude', prompt: 'a', output: sum });
    const c = g.agent('c', { agent: 'claude', prompt: 'c', output: sum });
    const j = g.agent('j', { agent: 'claude', prompt: 'j', output: sum });
    g.edge(a, j);
    g.edge(c, j);
  });
  const { h, id } = engineWith(ir, hive);
  try {
    const first = await prepare(h.engine, id, 'a');
    assert.deepEqual(first, { path: `/wt/hive/wf/${id}/a`, branch: `hive/wf/${id}/a` });
    assert.equal(h.store.getRun(id)?.base_sha, 'sha-1');
    const p = hive.requests.filter((r) => r.name === 'PREPARE_WORKTREE');
    assert.deepEqual(p[0].payload, { project_id: 'p1', workflow_run_id: id, branch: `hive/wf/${id}/a`, base: 'HEAD' });
    // Both upstreams are done with branches: the join starts from the first and merges the rest.
    for (const k of ['a', 'c']) h.engine.run(id).model.rows.set(k, { ...(h.engine.run(id).model.rows.get(k) as Row), state: 'done', branch: `hive/wf/${id}/${k}` });
    await prepare(h.engine, id, 'j');
    assert.deepEqual(hive.requests.filter((r) => r.name === 'PREPARE_WORKTREE')[1].payload, {
      project_id: 'p1',
      workflow_run_id: id,
      branch: `hive/wf/${id}/j`,
      base: `refs/heads/hive/wf/${id}/a`,
      merge: [`hive/wf/${id}/c`],
    });
  } finally {
    h.close();
  }
});

test('PREPARE: a conflict is returned with the files, the kept worktree and the recovery message', async () => {
  const hive = new FakeHive();
  prepared(hive, () => ({ conflict: { branch: 'hive/wf/x/c', files: ['a.txt', 'b.txt'] } }));
  const ir = workflow('j', s.object({}), (g) => void g.agent('a', { agent: 'claude', prompt: 'a', output: sum }));
  const { h, id } = engineWith(ir, hive);
  try {
    const r = await prepare(h.engine, id, 'a');
    assert.ok(isConflict(r));
    if (isConflict(r)) {
      assert.deepEqual(r.conflict, { branch: 'hive/wf/x/c', files: ['a.txt', 'b.txt'], worktree: `/wt/hive/wf/${id}/a` });
      assert.equal(r.message, `Merging hive/wf/x/c conflicted in a.txt, b.txt. The conflicting merge was aborted: the worktree kept at /wt/hive/wf/${id}/a is clean, holds the branches merged before it, and does not contain hive/wf/x/c. Merge hive/wf/x/c there yourself, resolve the conflicts and commit, then retry the run.`);
    }
  } finally {
    h.close();
  }
});

test('conflictMessage names at most eight files', () => {
  const files = Array.from({ length: 12 }, (_, i) => `f${i}`);
  assert.match(conflictMessage('b', files, '/p'), /conflicted in f0, f1, f2, f3, f4, f5, f6, f7, and 4 more\./);
});

test('PREPARE: a check with one upstream head re-attaches that branch, and refuses a branch that is gone', async () => {
  const hive = new FakeHive();
  prepared(hive, (req) => ({ reattached: req.branch !== 'hive/wf/gone' }));
  const ir = workflow('c', s.object({}), (g) => {
    const a = g.agent('a', { agent: 'claude', prompt: 'a', output: sum });
    const c = g.check('c', { argv: ['true'] });
    g.edge(a, c);
  });
  const { h, id } = engineWith(ir, hive);
  try {
    const rows = h.engine.run(id).model.rows;
    rows.set('a', { ...(rows.get('a') as Row), state: 'done', branch: `hive/wf/${id}/a` });
    assert.deepEqual(await prepare(h.engine, id, 'c'), { path: `/wt/hive/wf/${id}/a`, branch: `hive/wf/${id}/a` });
    const req = hive.requests.filter((r) => r.name === 'PREPARE_WORKTREE').at(-1)?.payload;
    assert.equal(req?.branch, `hive/wf/${id}/a`, 'the check runs in the agent\'s branch, not a new one');
    rows.set('a', { ...(rows.get('a') as Row), branch: 'hive/wf/gone' });
    await assert.rejects(prepare(h.engine, id, 'c'), /upstream branch hive\/wf\/gone is gone/);
  } finally {
    h.close();
  }
});

test('PREPARE: worktree {of} reuses the path of the node it names, and never calls the daemon', async () => {
  const hive = new FakeHive();
  prepared(hive);
  const { h, id } = engineWith(examples.reviewLoop(), hive, { task: 't' });
  try {
    const rows = h.engine.run(id).model.rows;
    rows.set('fix[1]/work', { ...(rows.get('fix[1]/work') as Row), state: 'done', branch: `hive/wf/${id}/fix_1/work`, worktree: '/wt/work' });
    assert.deepEqual(await prepare(h.engine, id, 'fix[1]/review'), { path: '/wt/work', branch: `hive/wf/${id}/fix_1/work` });
    assert.equal(hive.requests.filter((r) => r.name === 'PREPARE_WORKTREE').length, 0);
  } finally {
    h.close();
  }
});
