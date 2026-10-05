import assert from 'node:assert/strict';
import { test } from 'node:test';
import { workflow } from './dsl.ts';
import { examples } from './examples.ts';
import { toMermaid } from './mermaid.ts';
import { s } from './schema.ts';

test('renders condition labels on edges', () => {
  const out = toMermaid(examples.planImplementVerify());
  assert.match(out, /feature__approve -->\|"approve\.decision = approve"\| feature__build/);
});

test('renders loop and map as labelled subgraphs', () => {
  assert.match(toMermaid(examples.reviewLoop()), /subgraph fix\["fix · loop ≤3× until review\.verdict = approve"\]/);
  const fan = toMermaid(examples.fanOutThenVerify());
  assert.match(fan, /subgraph work__each\["each · for each split\.tasks, ≤4 at once"\]/);
  assert.match(fan, /work__verify\[\["verify · check"\]\]/);
  // Every subgraph is closed.
  assert.equal((fan.match(/^\s*subgraph /gm) ?? []).length, (fan.match(/^\s*end$/gm) ?? []).length);
});

test('output is deterministic', () => {
  for (const make of Object.values(examples)) assert.equal(toMermaid(make()), toMermaid(make()));
});

test('escapes quotes and keeps Mermaid keywords out of node names', () => {
  const out = toMermaid(
    workflow('kw', s.object({}), (g) => {
      g.agent('end', { agent: 'say "hi"', prompt: 'p', output: s.object({ x: s.string() }) });
    }),
  );
  assert.match(out, /^ {2}end_\["end · say #quot;hi#quot;"\]$/m);
});
