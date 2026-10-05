import assert from 'node:assert/strict';
import { test } from 'node:test';
import { type GroupNode, type LoopNode, type MapNode, validate, workflow } from './dsl.ts';
import { examples } from './examples.ts';
import { reviewLoop } from './library.ts';
import { s } from './schema.ts';

test('reviewLoop builds worker→reviewer with a bounded back-edge on revise', () => {
  const loop = examples.reviewLoop().nodes[0] as LoopNode;
  assert.equal(loop.kind, 'loop');
  assert.deepEqual(
    loop.nodes.map((n) => [n.id, n.kind]),
    [
      ['fix/work', 'agent'],
      ['fix/review', 'agent'],
    ],
  );
  assert.deepEqual(loop.edges, [{ from: 'fix/work', to: 'fix/review' }]);
  assert.deepEqual(loop.until, { ref: 'fix/review.verdict', op: 'eq', value: 'approve' });
  const [work, review] = loop.nodes;
  assert.ok(work.kind === 'agent' && work.prompt.includes('{{fix/review.feedback}}'), 'worker sees last feedback');
  assert.ok(review.kind === 'agent' && review.prompt.includes('{{fix/work.summary}}'), 'reviewer sees the work');
});

test('fanOutThenVerify maps step over items, then joins into verify', () => {
  const ir = examples.fanOutThenVerify();
  assert.deepEqual(ir.edges, [{ from: 'split', to: 'work' }]);
  const group = ir.nodes[1] as GroupNode;
  assert.deepEqual(
    group.nodes.map((n) => [n.id, n.kind]),
    [
      ['work/each', 'map'],
      ['work/verify', 'check'],
    ],
  );
  assert.deepEqual(group.edges, [{ from: 'work/each', to: 'work/verify' }]);
  const map = group.nodes[0] as MapNode;
  const step = map.nodes[0];
  assert.ok(step.kind === 'agent' && step.prompt.includes('{{$item}}'));
});

test('planImplementVerify chains plan→approve→implement→check, and asks a human when the loop runs out', () => {
  const group = examples.planImplementVerify().nodes[0] as GroupNode;
  assert.deepEqual(
    group.nodes.map((n) => [n.id, n.kind]),
    [
      ['feature/plan', 'agent'],
      ['feature/approve', 'human'],
      ['feature/build', 'loop'],
      ['feature/exhausted', 'human'],
      ['feature/tests', 'check'],
    ],
  );
  assert.deepEqual(group.edges, [
    { from: 'feature/plan', to: 'feature/approve' },
    {
      from: 'feature/approve',
      to: 'feature/build',
      when: { ref: 'feature/approve.decision', op: 'eq', value: 'approve' },
    },
    { from: 'feature/build', to: 'feature/tests', when: { ref: 'feature/build.converged', op: 'eq', value: true } },
    { from: 'feature/build', to: 'feature/exhausted', when: { ref: 'feature/build.converged', op: 'eq', value: false } },
    {
      from: 'feature/exhausted',
      to: 'feature/tests',
      when: { ref: 'feature/exhausted.decision', op: 'eq', value: 'run-checks' },
    },
  ]);
  const build = group.nodes[2] as LoopNode;
  assert.deepEqual(
    build.nodes.map((n) => n.id),
    ['feature/build/work', 'feature/build/review'],
  );
});

test('library outputs validate', () => {
  for (const [name, make] of Object.entries(examples)) assert.deepEqual(validate(make()), [], name);
});

test('a construct used twice gets distinct, prefixed ids', () => {
  const spec = (agent: string) => ({ agent, prompt: 'p', output: s.object({ summary: s.string() }) });
  const rev = { agent: 'codex', prompt: 'r', output: s.object({ verdict: s.enum('approve', 'revise'), feedback: s.string() }) };
  const ir = workflow('twice', s.object({}), (g) => {
    const one = g.use('one', reviewLoop(spec('claude'), rev, { maxIters: 2 }));
    const two = g.use('two', reviewLoop(spec('pi'), rev, { maxIters: 2 }));
    g.edge(one, two);
  });
  assert.deepEqual(validate(ir), []);
  const ids = (ir.nodes as LoopNode[]).flatMap((l) => [l.id, ...l.nodes.map((n) => n.id)]);
  assert.deepEqual(ids, ['one', 'one/work', 'one/review', 'two', 'two/work', 'two/review']);
});

test('reviewLoop puts the reviewer in the worker worktree', () => {
  const loop = examples.reviewLoop().nodes[0] as LoopNode;
  const review = loop.nodes[1];
  assert.ok(review.kind === 'agent');
  assert.deepEqual(review.worktree, { of: 'fix/work' });
});
