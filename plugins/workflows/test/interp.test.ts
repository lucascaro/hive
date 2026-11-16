// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The reducer's rules, one table-driven case each (spec 529, Run semantics).
// No store, no processes: test/sim.ts drives the pure step/apply loop.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_AGENT_TURNS, MAX_ATTEMPTS, MAX_LIVE_AGENTS, MAX_MAP_COPIES, renderPrompt, scopeOf, step } from '../engine/interp.ts';
import { validate } from '../ir/validate.ts';
import { shape } from '../ir/shape.ts';
import { type IR, p, when, workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { reviewLoop } from '../sdk/library.ts';
import { keys, out, sim, state } from './sim.ts';

const ok = { output: { ok: true, exit_code: 0 } };
const no = { output: { ok: false, exit_code: 1 } };

function valid(ir: IR): IR {
  assert.deepEqual(shape(ir), []);
  assert.deepEqual(validate(ir), []);
  return ir;
}

const empty = s.object({});
const argv = { argv: ['true'] };

test('a node is ready only when every incoming edge has settled', () => {
  const ir = valid(
    workflow('t', empty, (g) => {
      const a = g.check('a', argv);
      const b = g.check('b', argv);
      const c = g.check('c', argv);
      g.edge(a, c);
      g.edge(b, c);
    }),
  );
  const x = sim(ir, {}, { node: (k) => (k === 'a' ? ok : 'hang') });
  x.run();
  assert.equal(state(x, 'a'), 'done');
  assert.equal(state(x, 'b'), 'running');
  assert.equal(state(x, 'c'), 'pending', 'c waits for b');
});

test('a node runs on any taken edge and is skipped when none is taken', () => {
  const build = (aOk: boolean, bOk: boolean) => {
    const ir = valid(
      workflow('t', empty, (g) => {
        const a = g.check('a', argv);
        const b = g.check('b', argv);
        const c = g.check('c', argv);
        g.edge(a, c, when(a.out.ok, 'eq', true));
        g.edge(b, c, when(b.out.ok, 'eq', true));
      }),
    );
    const x = sim(ir, {}, { node: (k) => (k === 'a' ? (aOk ? ok : no) : k === 'b' ? (bOk ? ok : no) : ok) });
    x.run();
    return x;
  };
  assert.equal(state(build(true, false), 'c'), 'done', 'one taken edge is enough');
  assert.equal(state(build(false, true), 'c'), 'done');
  const none = build(false, false);
  assert.equal(state(none, 'c'), 'skipped', 'no taken edge skips');
  assert.equal(none.m.state, 'done', 'a run that skipped a node still finishes');
});

test('skipping propagates down a chain', () => {
  const ir = valid(
    workflow('t', empty, (g) => {
      const a = g.check('a', argv);
      const b = g.check('b', argv);
      const c = g.check('c', argv);
      g.edge(a, b, when(a.out.ok, 'eq', true));
      g.edge(b, c);
    }),
  );
  const x = sim(ir, {}, { node: () => no });
  x.run();
  assert.deepEqual([state(x, 'b'), state(x, 'c')], ['skipped', 'skipped']);
});

test('a reference to a skipped node reads empty: truthy and eq are false, ne is true', () => {
  const ir = valid(
    workflow('t', empty, (g) => {
      const a = g.check('a', argv);
      const b = g.check('b', argv);
      const j = g.check('j', argv);
      const ask = g.human('ask', { prompt: p`seen: [${b.out.exit_code}]`, output: s.object({ go: s.boolean() }) });
      const t = g.check('t', argv);
      const e = g.check('e', argv);
      const n = g.check('n', argv);
      g.edge(a, b, when(a.out.ok, 'eq', true));
      g.edge(a, j);
      g.edge(b, j);
      g.edge(j, ask);
      g.edge(j, t, when(b.out.exit_code, 'truthy'));
      g.edge(j, e, when(b.out.exit_code, 'eq', 0));
      g.edge(j, n, when(b.out.exit_code, 'ne', 0));
    }),
  );
  const x = sim(ir, {}, { node: () => no });
  x.run();
  assert.equal(state(x, 'b'), 'skipped');
  assert.equal(state(x, 'j'), 'done', 'a taken edge from a is enough');
  assert.deepEqual([state(x, 't'), state(x, 'e'), state(x, 'n')], ['skipped', 'skipped', 'done']);
  assert.equal(x.events.find((e) => e.type === 'node.waiting')?.data.prompt, 'seen: []', 'a placeholder for a skipped node renders as nothing');
});

test('a group is done when every node inside it has settled', () => {
  const ir = valid(
    workflow('t', empty, (g) => {
      g.group('grp', (b) => {
        const a = b.check('a', argv);
        const c = b.check('c', argv);
        b.edge(a, c);
        return { a, c };
      });
    }),
  );
  const x = sim(ir, {}, { node: (k) => (k === 'grp/c' ? 'hang' : ok) });
  x.run();
  assert.equal(state(x, 'grp'), 'running');
  x.commit({ t: 'finish', key: 'grp/c', output: ok.output });
  x.run();
  assert.equal(state(x, 'grp'), 'done');
  assert.equal(x.m.state, 'done');
});

test('a skipped group skips what is inside it', () => {
  const ir = valid(
    workflow('t', empty, (g) => {
      const a = g.check('a', argv);
      const grp = g.group('grp', (b) => ({ x: b.check('x', argv) }));
      g.edge(a, grp, when(a.out.ok, 'eq', true));
    }),
  );
  const x = sim(ir, {}, { node: () => no });
  x.run();
  assert.deepEqual([state(x, 'grp'), state(x, 'grp/x')], ['skipped', 'skipped']);
});

// ---------------------------------------------------------------------- loops

const loopIr = (maxIters: number) =>
  valid(
    workflow('t', empty, (g) => {
      g.loop('lp', { maxIters }, (b) => {
        const w = b.check('w', argv);
        const r = b.check('r', argv);
        b.edge(w, r);
        return when(r.out.ok, 'eq', true);
      });
    }),
  );

test('a loop stops when until holds, and reports the iteration', () => {
  let round = 0;
  const x = sim(loopIr(5), {}, { node: (k) => (k.endsWith('/w') ? (round++, ok) : round >= 2 ? ok : no) });
  x.run();
  assert.deepEqual(out(x, 'lp'), { converged: true, iterations: 2 });
  assert.deepEqual(keys(x).filter((k) => k.startsWith('lp[')), ['lp[1]/r', 'lp[1]/w', 'lp[2]/r', 'lp[2]/w']);
  const iters = x.events.filter((e) => e.type === 'loop.iteration').map((e) => e.data);
  assert.deepEqual(iters, [{ iteration: 1, max_iters: 5 }, { iteration: 2, max_iters: 5 }]);
});

test('a loop that runs out of rounds ends with converged false', () => {
  const x = sim(loopIr(3), {}, { node: (k) => (k.endsWith('/r') ? no : ok) });
  x.run();
  assert.deepEqual(out(x, 'lp'), { converged: false, iterations: 3 });
  assert.equal(x.m.state, 'done', 'exhausting is an outcome, not a failure');
});

test('a back-reference inside a loop body reads the previous iteration, and is empty on the first', () => {
  const ir = valid(
    workflow('t', empty, (g) => {
      g.use(
        'fix',
        reviewLoop(
          { agent: 'claude', prompt: 'work', output: s.object({ summary: s.string() }) },
          { agent: 'codex', prompt: 'review', output: s.object({ verdict: s.enum('approve', 'revise'), feedback: s.string() }) },
          { maxIters: 3 },
        ),
      );
    }),
  );
  const seen: Record<string, string> = {};
  const loop = ir.nodes[0] as unknown as { nodes: { id: string; prompt: string }[] };
  const workPrompt = (loop.nodes.find((n) => n.id === 'fix/work') as { prompt: string }).prompt;
  const x = sim(ir, {}, {
    node: (k, _n, m) => {
      if (k.endsWith('/work')) {
        // Rendered when the node starts, which is when the engine renders it.
        seen[k] = renderPrompt(m, workPrompt, scopeOf(m, k) as never);
        return { output: { summary: `s-${k}` } };
      }
      return { output: { verdict: k.includes('[2]') ? 'approve' : 'revise', feedback: `fb-${k}` } };
    },
  });
  x.run();
  assert.match(seen['fix[1]/work'], /previous round, if any: $/, 'empty on round 1');
  assert.match(seen['fix[2]/work'], /previous round, if any: fb-fix\[1\]\/review$/, 'round 2 reads round 1');
  assert.deepEqual(out(x, 'fix'), { converged: true, iterations: 2 });
});

test('a body node skipped in this iteration reads empty, not the previous iteration', () => {
  const ir = valid(
    workflow('t', s.object({}), (g) => {
      g.loop('lp', { maxIters: 2 }, (b) => {
        const a = b.check('a', argv);
        const c = b.check('c', argv);
        const d = b.human('d', { prompt: p`got [${c.out.exit_code}]`, output: s.object({ x: s.string() }) });
        b.edge(a, c, when(a.out.ok, 'eq', true));
        b.edge(a, d);
        b.edge(c, d);
        return when(a.out.exit_code, 'gt', 100);
      });
    }),
  );
  const x = sim(ir, {}, { node: (k) => (k === 'lp[2]/a' ? no : ok), human: () => ({ x: 'y' }) });
  x.run();
  const waits = x.events.filter((e) => e.type === 'node.waiting').map((e) => e.data.prompt);
  assert.deepEqual(waits, ['got [0]', 'got []'], 'iteration 2 skipped c, so d sees nothing, not iteration 1');
});

// ----------------------------------------------------------------------- maps

const mapIr = (concurrency: number) =>
  valid(
    workflow('t', s.object({ items: s.array(s.string()) }), (g, input) => {
      g.map('m', { over: input.items, concurrency }, (b, item) => b.agent('do', { agent: 'claude', prompt: p`do ${item}`, output: s.object({ r: s.string() }) }));
    }),
  );

test('a map expands one copy per element, one row per node instance, in the same commit as map.expanded', () => {
  const ir = valid(
    workflow('t', s.object({ items: s.array(s.string()) }), (g, input) => {
      g.map('m', { over: input.items, concurrency: 2 }, (b, item) => {
        const a = b.agent('a', { agent: 'claude', prompt: p`do ${item}`, output: s.object({ r: s.string() }) });
        const c = b.check('c', argv);
        b.edge(a, c);
        return c;
      });
    }),
  );
  const x = sim(ir, { items: ['x', 'y', 'z'] }, {});
  x.commit({ t: 'ready', key: 'm' });
  const started = x.commit({ t: 'start', key: 'm' });
  assert.deepEqual([...new Set(started.change.rows.map((r) => r.key))].sort(), ['m', 'm[0]/a', 'm[0]/c', 'm[1]/a', 'm[1]/c', 'm[2]/a', 'm[2]/c']);
  const expanded = started.change.events.filter((e) => e.type === 'map.expanded');
  assert.deepEqual(expanded.map((e) => e.data), [{ count: 3, concurrency: 2 }], 'rows and event travel together');
  assert.ok(started.change.rows.filter((r) => r.key !== 'm').every((r) => r.state === 'pending'));
});

test('a map admits at most `concurrency` copies at once and gives results in input order', () => {
  const x = sim(mapIr(2), { items: ['a', 'b', 'c', 'd'] }, { node: () => 'hang' });
  x.run();
  assert.deepEqual(x.started, ['m[0]/do', 'm[1]/do'], 'two copies in flight, not four');
  // Finish the second copy first: the next one is admitted, order is kept in the results.
  x.commit({ t: 'finish', key: 'm[1]/do', output: { r: 'B' } });
  x.run();
  assert.deepEqual(x.started, ['m[0]/do', 'm[1]/do', 'm[2]/do']);
  x.commit({ t: 'finish', key: 'm[0]/do', output: { r: 'A' } });
  x.run();
  x.commit({ t: 'finish', key: 'm[2]/do', output: { r: 'C' } });
  x.run();
  x.commit({ t: 'finish', key: 'm[3]/do', output: { r: 'D' } });
  x.run();
  assert.deepEqual(out(x, 'm'), { results: [{ r: 'A' }, { r: 'B' }, { r: 'C' }, { r: 'D' }] });
  assert.equal(x.m.state, 'done');
});

test('an empty map is done at once with no results', () => {
  const x = sim(mapIr(2), { items: [] }, {});
  x.run();
  assert.deepEqual(out(x, 'm'), { results: [] });
  assert.equal(x.m.state, 'done');
});

test('a map over more than 32 elements fails the run naming the cap', () => {
  const x = sim(mapIr(4), { items: Array.from({ length: MAX_MAP_COPIES + 1 }, (_, i) => String(i)) }, {});
  x.run();
  assert.equal(state(x, 'm'), 'failed');
  assert.equal(x.m.rows.get('m')?.cap, 'map_copies');
  assert.equal(x.m.state, 'failed');
  const fin = x.events.find((e) => e.type === 'run.finished');
  assert.equal(fin?.data.cap, 'map_copies');
});

test('turns over 50 fail the run naming agent_turns', () => {
  // 32 copies of an agent and a second agent after it: 64 turns.
  const ir = valid(
    workflow('t', s.object({ items: s.array(s.string()) }), (g, input) => {
      g.map('m', { over: input.items, concurrency: 16 }, (b) => {
        const a = b.agent('a', { agent: 'claude', prompt: 'a', output: s.object({ r: s.string() }) });
        const c = b.agent('c', { agent: 'claude', prompt: 'c', output: s.object({ r: s.string() }) });
        b.edge(a, c);
        return c;
      });
    }),
  );
  const x = sim(ir, { items: Array.from({ length: 32 }, (_, i) => String(i)) }, { node: () => ({ output: { r: 'x' } }) });
  x.run();
  assert.equal(x.m.state, 'failed');
  const failed = [...x.m.rows.values()].find((r) => r.state === 'failed' && r.cap === 'agent_turns');
  assert.ok(failed, 'a node failed on the turn cap');
  assert.equal(x.events.find((e) => e.type === 'run.finished')?.data.cap, 'agent_turns');
  const used = [...x.m.rows.values()].reduce((n, r) => n + r.turns, 0);
  assert.equal(used, MAX_AGENT_TURNS, 'exactly the cap was spent');
});

test('live agents are capped at 4 by throttle, not by failure', () => {
  const x = sim(mapIr(16), { items: Array.from({ length: 16 }, (_, i) => String(i)) }, { node: () => 'hang' });
  x.run();
  assert.equal(x.started.length, MAX_LIVE_AGENTS);
  assert.equal(x.m.state, 'running', 'waiting for headroom is not a failure');
  const readied = [...x.m.rows.values()].filter((r) => r.ready && r.key.endsWith('/do')).length;
  assert.equal(readied, 16, 'every admitted copy is ready; only the start waits');
  x.commit({ t: 'finish', key: 'm[0]/do', output: { r: 'x' } });
  x.run();
  assert.equal(x.started.length, MAX_LIVE_AGENTS + 1, 'one finished, one more starts');
});

// -------------------------------------------------------------------- failure

test('a failed node fails the run, cancels running siblings and leaves pending nodes pending', () => {
  const ir = valid(
    workflow('t', empty, (g) => {
      const a = g.check('a', argv);
      const b = g.check('b', argv);
      const c = g.check('c', argv);
      const h = g.human('h', { prompt: 'wait', output: s.object({ x: s.string() }) });
      g.edge(a, c);
      g.edge(b, h);
    }),
  );
  const x = sim(ir, {}, { node: (k) => (k === 'a' ? { fail: 'boom' } : 'hang') });
  x.run();
  assert.equal(x.m.state, 'failed');
  assert.equal(state(x, 'a'), 'failed');
  assert.equal(state(x, 'b'), 'cancelled', 'a running sibling is cancelled');
  assert.equal(state(x, 'c'), 'pending', 'what never ran stays pending so run.retry can resume it');
  const fin = x.events.find((e) => e.type === 'run.finished');
  assert.equal(fin?.data.state, 'failed');
  assert.match(String(fin?.data.error), /^a: boom/);
});

test('an interrupted node gets one more attempt, then fails the run', () => {
  const ir = valid(workflow('t', empty, (g) => void g.check('a', argv)));
  const x = sim(ir, {}, { node: () => 'hang' });
  x.run();
  assert.equal(x.m.rows.get('a')?.attempt, 1);
  x.commit({ t: 'interrupt', key: 'a' });
  let due = step(x.m);
  assert.deepEqual(due.actions.map((a) => a.key), ['a'], 'attempt 2 is offered');
  x.commit({ t: 'start', key: 'a' });
  assert.equal(x.m.rows.get('a')?.attempt, 2);
  x.commit({ t: 'interrupt', key: 'a' });
  due = step(x.m);
  assert.deepEqual(due.actions, []);
  assert.equal(due.transitions[0].t, 'fail');
  assert.equal(MAX_ATTEMPTS, 2);
  x.run();
  assert.equal(x.m.state, 'failed');
  assert.match(x.m.rows.get('a')?.error ?? '', /interrupted 2 times/);
});

test('a human node waits with its rendered prompt and schema, and an answer finishes it with output and finished together', () => {
  const ir = valid(
    workflow('t', s.object({ who: s.string() }), (g, input) => {
      g.human('ask', { prompt: p`Hello ${input.who}`, output: s.object({ yes: s.boolean() }) });
    }),
  );
  const x = sim(ir, { who: 'Ada' }, {});
  x.run();
  assert.equal(state(x, 'ask'), 'waiting');
  assert.equal(x.m.state, 'waiting', 'the run is waiting while a human is');
  const w = x.events.find((e) => e.type === 'node.waiting');
  assert.deepEqual(w?.data, { reason: 'human', prompt: 'Hello Ada', output_schema: (ir.nodes[0] as { output: unknown }).output });
  const before = x.events.length;
  const a = x.commit({ t: 'finish', key: 'ask', output: { yes: true } });
  assert.deepEqual(a.change.events.map((e) => e.type), ['node.output', 'node.finished'], 'one transaction, no node.resumed');
  assert.equal(x.events.length, before + 2);
});

test('a human prompt over 16 KiB as sent fails the node with prompt_too_large', () => {
  const ir = valid(
    workflow('t', s.object({ who: s.string() }), (g, input) => {
      g.human('ask', { prompt: p`Hello ${input.who}`, output: s.object({ yes: s.boolean() }) });
    }),
  );
  // 3,000 `<` is 3 KB raw and 18 KB once Go escapes them.
  const x = sim(ir, { who: '<'.repeat(3000) }, {});
  x.run();
  assert.equal(state(x, 'ask'), 'failed');
  assert.equal(x.m.rows.get('ask')?.cap, 'prompt_too_large');
  assert.equal(x.m.state, 'failed');
});

test('run.cancel cancels what is live; run.retry resets failed, interrupted and cancelled nodes and the run resumes', () => {
  const ir = valid(
    workflow('t', empty, (g) => {
      const a = g.check('a', argv);
      const b = g.check('b', argv);
      const c = g.check('c', argv);
      g.edge(a, c);
      g.edge(b, c);
    }),
  );
  const x = sim(ir, {}, { node: (k) => (k === 'a' ? { fail: 'boom' } : 'hang') });
  x.run();
  assert.equal(x.m.state, 'failed');
  const retry = x.commit({ t: 'run.retry' });
  assert.deepEqual(
    retry.change.events.map((e) => [e.type, e.key]),
    [['node.reset', 'a'], ['node.reset', 'b'], ['run.retried', null]],
    'node.reset per node, in key order, then run.retried, all in one commit',
  );
  assert.deepEqual(retry.change.events.at(-1)?.data, { reset: 2 });
  assert.deepEqual(retry.change.rows.map((r) => [r.key, r.state, r.attempt]), [['a', 'pending', 0], ['b', 'pending', 0]]);
  assert.equal(x.m.state, 'running');
  const y = sim(ir, {}, { node: () => ok });
  y.commit({ t: 'run.cancel' });
  assert.equal(y.m.state, 'cancelled');
  y.commit({ t: 'run.retry' });
  y.run();
  assert.equal(y.m.state, 'done', 'a cancelled run retries from the start');
});

test('retrying keeps a loop where it was: the iteration reached survives the reset', () => {
  let boom = true;
  const x = sim(loopIr(5), {}, {
    node: (k) => {
      if (k === 'lp[2]/w' && boom) {
        boom = false;
        return { fail: 'flaky' };
      }
      return k.endsWith('/r') ? (k.includes('[3]') ? ok : no) : ok;
    },
  });
  x.run();
  assert.equal(x.m.state, 'failed');
  assert.equal(x.m.rows.get('lp')?.progress, 2);
  x.commit({ t: 'run.retry' });
  assert.equal(x.m.rows.get('lp')?.state, 'pending');
  assert.equal(x.m.rows.get('lp')?.progress, 2, 'progress is kept');
  x.run();
  assert.deepEqual(out(x, 'lp'), { converged: true, iterations: 3 });
  const rounds = x.events.filter((e) => e.type === 'loop.iteration').map((e) => e.data.iteration);
  assert.deepEqual(rounds, [1, 2, 3], 'iteration 2 was not announced twice');
});

test('keys encode loop and map instances', () => {
  const ir = valid(
    workflow('t', s.object({ items: s.array(s.string()) }), (g, input) => {
      g.group('work', (w) => {
        w.map('each', { over: input.items, concurrency: 2 }, (b) => b.check('do', argv));
      });
    }),
  );
  const x = sim(ir, { items: ['a', 'b'] }, { node: () => ok });
  x.run();
  assert.deepEqual(keys(x), ['work', 'work/each', 'work/each[0]/do', 'work/each[1]/do']);
});
