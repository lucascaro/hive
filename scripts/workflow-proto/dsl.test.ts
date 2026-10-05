import assert from 'node:assert/strict';
import { test } from 'node:test';
import { type Cond, type IR, type Ref, all, not, validate, when, workflow } from './dsl.ts';
import { examples } from './examples.ts';
import { s } from './schema.ts';

const verdict = s.object({ verdict: s.enum('approve', 'revise'), feedback: s.string() });
const summary = s.object({ summary: s.string() });

function twoNodes(): IR {
  return workflow('two', s.object({ task: s.string() }), (g) => {
    const a = g.agent('a', { agent: 'claude', prompt: 'x', output: summary });
    const b = g.agent('b', { agent: 'codex', prompt: 'y', output: verdict });
    g.edge(a, b);
  });
}

test('build emits stable ids and edges', () => {
  for (const make of Object.values(examples)) {
    assert.equal(JSON.stringify(make()), JSON.stringify(make()));
  }
  const ir = twoNodes();
  assert.deepEqual(
    ir.nodes.map((n) => n.id),
    ['a', 'b'],
  );
  assert.deepEqual(ir.edges, [{ from: 'a', to: 'b' }]);
});

test('when() stores data conditions', () => {
  const ir = workflow('cond', s.object({}), (g) => {
    const r = g.agent('r', { agent: 'codex', prompt: 'x', output: verdict });
    const n = g.agent('n', { agent: 'claude', prompt: 'y', output: summary });
    g.edge(r, n, all(when(r.out.verdict, 'eq', 'revise'), not(when(r.out.feedback, 'eq', ''))));
  });
  assert.deepEqual(ir.edges[0].when, {
    all: [{ ref: 'r.verdict', op: 'eq', value: 'revise' }, { not: { ref: 'r.feedback', op: 'eq', value: '' } }],
  });
  assert.deepEqual(JSON.parse(JSON.stringify(ir)), ir);
  assert.deepEqual(validate(ir), []);
});

test('validate rejects an unbounded cycle', () => {
  const ir = twoNodes();
  ir.edges.push({ from: 'b', to: 'a' });
  assert.match(validate(ir).join('\n'), /cycle a -> b -> a; use loop\(\)/);
});

test('validate rejects a ref to an unknown output field', () => {
  const ir = workflow('bad-ref', s.object({}), (g) => {
    const a = g.agent('a', { agent: 'claude', prompt: 'x', output: summary });
    const b = g.agent('b', { agent: 'codex', prompt: 'y', output: verdict });
    const missing = { ref: 'a.nope' } as Ref<string>;
    g.edge(a, b, when(missing, 'eq', 'z'));
  });
  assert.match(validate(ir).join('\n'), /ref "a\.nope" has no field "nope"/);
});

test('validate rejects a value outside the enum', () => {
  const ir = workflow('bad-enum', s.object({}), (g) => {
    const a = g.agent('a', { agent: 'codex', prompt: 'x', output: verdict });
    const b = g.agent('b', { agent: 'claude', prompt: 'y', output: summary });
    g.edge(a, b, when(a.out.verdict as Ref<string>, 'eq', 'aprove'));
  });
  assert.match(validate(ir).join('\n'), /value "aprove" does not fit the field/);
});

test('validate rejects unresolved prompt placeholders', () => {
  const ir = workflow('bad-prompt', s.object({ task: s.string() }), (g) => {
    g.agent('a', { agent: 'claude', prompt: 'Do {{input.task}} then {{input.nope}}', output: summary });
  });
  const errors = validate(ir);
  assert.equal(errors.length, 1);
  assert.match(errors[0], /ref "input\.nope" has no field "nope"/);
});

test('validate rejects map over a non-array output', () => {
  const ir = workflow('bad-map', s.object({}), (g) => {
    const a = g.agent('a', { agent: 'claude', prompt: 'x', output: summary });
    g.map('m', { over: a.out.summary as unknown as Ref<string[]> }, (b) =>
      b.agent('do', { agent: 'codex', prompt: 'y', output: summary }),
    );
  });
  assert.match(validate(ir).join('\n'), /over "a\.summary" is not an array/);
});

test('validate rejects $item outside a map and refs into a map from outside', () => {
  const ir = workflow('bad-scope', s.object({}), (g) => {
    const a = g.agent('a', { agent: 'claude', prompt: 'x', output: s.object({ items: s.array(s.string()) }) });
    g.map('m', { over: a.out.items }, (b) => b.agent('do', { agent: 'codex', prompt: 'y', output: summary }));
    g.agent('after', { agent: 'claude', prompt: 'Use {{$item}} and {{m/do.summary}}', output: summary });
  });
  const errors = validate(ir).join('\n');
  assert.match(errors, /ref "\$item" used outside a map/);
  assert.match(errors, /ref "m\/do\.summary" reaches into map "m" from outside it/);
});

test('validate rejects duplicate ids after composition', () => {
  const ir = workflow('dupe', s.object({}), (g) => {
    g.group('x', (b) => b.agent('a', { agent: 'claude', prompt: 'p', output: summary }));
    g.group('x', (b) => b.agent('a', { agent: 'claude', prompt: 'p', output: summary }));
  });
  const errors = validate(ir).join('\n');
  assert.match(errors, /x: duplicate id/);
  assert.match(errors, /x\/a: duplicate id/);
});

test('validate rejects ids that could collide in Mermaid', () => {
  const ir = workflow('ids', s.object({}), (g) => {
    g.agent('a--b', { agent: 'claude', prompt: 'p', output: summary });
    g.agent('Upper', { agent: 'claude', prompt: 'p', output: summary });
  });
  const errors = validate(ir).join('\n');
  assert.match(errors, /^a--b: id must be one segment/m);
  assert.match(errors, /^Upper: id must be one segment/m);
});

test('validate rejects an edge that crosses subgraphs', () => {
  const ir = workflow('cross', s.object({}), (g) => {
    let inner: { id: string } = { id: '' };
    const grp = g.group('grp', (b) => {
      inner = b.agent('in', { agent: 'claude', prompt: 'p', output: summary });
    });
    const out = g.agent('out', { agent: 'claude', prompt: 'p', output: summary });
    g.edge(out, inner);
    g.edge(out, grp);
  });
  assert.match(validate(ir).join('\n'), /edge out -> grp\/in: both ends must be nodes of the same subgraph/);
});

test('loop records maxIters and its until condition', () => {
  const ir = examples.reviewLoop();
  const loop = ir.nodes[0];
  assert.equal(loop.kind, 'loop');
  if (loop.kind !== 'loop') return;
  assert.equal(loop.max_iters, 3);
  assert.deepEqual(loop.until, { ref: 'fix/review.verdict', op: 'eq', value: 'approve' });
  loop.max_iters = 0;
  assert.match(validate(ir).join('\n'), /fix: max_iters must be an integer in 1\.\.20/);
  loop.max_iters = 21;
  assert.match(validate(ir).join('\n'), /fix: max_iters must be an integer in 1\.\.20/);
});

test('map records concurrency and its template subgraph', () => {
  const ir = examples.fanOutThenVerify();
  const group = ir.nodes[1];
  assert.equal(group.kind, 'group');
  if (group.kind !== 'group') return;
  const map = group.nodes[0];
  assert.equal(map.kind, 'map');
  if (map.kind !== 'map') return;
  assert.equal(map.over, 'split.tasks');
  assert.equal(map.concurrency, 4);
  assert.deepEqual(
    map.nodes.map((n) => n.id),
    ['work/each/do'],
  );
  map.concurrency = 17;
  assert.match(validate(ir).join('\n'), /concurrency must be an integer in 1\.\.16/);
});

test('map template needs exactly one sink', () => {
  const ir = workflow('two-sinks', s.object({}), (g) => {
    const a = g.agent('a', { agent: 'claude', prompt: 'x', output: s.object({ items: s.array(s.string()) }) });
    g.map('m', { over: a.out.items }, (b) => {
      b.agent('one', { agent: 'codex', prompt: 'y', output: summary });
      return b.agent('two', { agent: 'codex', prompt: 'y', output: summary });
    });
  });
  assert.match(validate(ir).join('\n'), /m: template must have exactly one sink/);
});

test('worktree.of must name another agent node', () => {
  const ir = workflow('wt', s.object({}), (g) => {
    const c = g.check('c', { argv: ['true'] });
    g.agent('a', { agent: 'claude', prompt: 'p', output: summary, worktree: c });
  });
  assert.match(validate(ir).join('\n'), /a: worktree\.of "c" must name another agent node/);
});

// One mutation per validate() branch not pinned above. Each starts from a
// valid IR, so a deleted or inverted check fails here with its own message.
test('validate rejects each malformed node, edge, ref and condition', () => {
  const base = (): IR =>
    workflow('base', s.object({}), (g) => {
      const a = g.agent('a', { agent: 'claude', prompt: 'x', output: summary });
      const b = g.agent('b', { agent: 'codex', prompt: 'y', output: verdict });
      const c = g.check('c', { argv: ['true'] });
      const list = g.agent('list', { agent: 'claude', prompt: 'z', output: s.object({ items: s.array(s.string()) }) });
      g.group('grp', (sub) => sub.agent('x', { agent: 'claude', prompt: 'p', output: summary }));
      g.loop('lp', { maxIters: 2 }, (sub) => {
        const r = sub.agent('r', { agent: 'codex', prompt: 'p', output: verdict });
        return when(r.out.verdict, 'eq', 'approve');
      });
      g.map('m', { over: list.out.items }, (sub) => sub.agent('do', { agent: 'codex', prompt: 'p', output: summary }));
      g.edge(a, b);
      g.edge(a, c);
    });
  const node = (ir: IR, id: string) => ir.nodes.find((n) => n.id === id) as unknown as Record<string, unknown>;
  const cond = (c: Cond) => (ir: IR) => {
    ir.edges[0].when = c;
  };
  const cases: [string, (ir: IR) => void, RegExp][] = [
    ['reserved id', (ir) => void (node(ir, 'list').id = 'input'), /input: reserved id/],
    ['self-edge', (ir) => void ir.edges.push({ from: 'a', to: 'a' }), /edge a -> a: self-edge/],
    ['duplicate edge', (ir) => void ir.edges.push({ from: 'a', to: 'b' }), /edge a -> b: duplicate edge/],
    ['empty agent', (ir) => void (node(ir, 'a').agent = ''), /a: agent is empty/],
    ['agent output not an object', (ir) => void (node(ir, 'a').output = { type: 'string' }), /a: output must be an object schema/],
    ['agent timeout', (ir) => void (node(ir, 'a').timeout_s = 0), /a: timeout_s must be > 0/],
    ['empty argv', (ir) => void (node(ir, 'c').argv = []), /c: argv is empty/],
    ['check timeout', (ir) => void (node(ir, 'c').timeout_s = -1), /c: timeout_s must be > 0/],
    ['empty group', (ir) => void (node(ir, 'grp').nodes = []), /grp: empty group/],
    ['empty loop', (ir) => void (node(ir, 'lp').nodes = []), /lp: empty loop/],
    ['empty map', (ir) => void (node(ir, 'm').nodes = []), /m: empty map/],
    ['malformed ref', (ir) => void (node(ir, 'b').prompt = '{{nodot}}'), /ref "nodot" is not "<node>\.<field>"/],
    ['ref to no node', (ir) => void (node(ir, 'b').prompt = '{{zz.f}}'), /ref "zz\.f" names no node/],
    ['ref to a node with no output', (ir) => void (node(ir, 'b').prompt = '{{grp.f}}'), /ref "grp\.f" names a group node, which has no output/],
    ['truthy with a value', cond({ ref: 'a.summary', op: 'truthy', value: 'x' }), /takes no value for truthy/],
    ['lt on a string', cond({ ref: 'a.summary', op: 'lt', value: 1 }), /needs a number field and value for lt/],
    ['in without an array', cond({ ref: 'a.summary', op: 'in', value: 'x' }), /needs an array value for in/],
    ['in with a misfit element', cond({ ref: 'a.summary', op: 'in', value: ['ok', 1] }), /value 1 does not fit the field/],
  ];
  assert.deepEqual(validate(base()), []);
  for (const [name, mutate, want] of cases) {
    const ir = base();
    mutate(ir);
    assert.match(validate(ir).join('\n'), want, name);
  }
});
