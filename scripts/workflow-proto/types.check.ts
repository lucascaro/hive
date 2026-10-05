// Type-level tests (spec 495): `tsc -p scripts/workflow-proto` fails if any
// `@ts-expect-error` line below stops being an error. Never run by node.

import { when, workflow } from './dsl.ts';
import { s } from './schema.ts';

workflow('types', s.object({ task: s.string() }), (g, input) => {
  const review = g.agent('review', {
    agent: 'codex',
    prompt: 'x',
    output: s.object({ verdict: s.enum('approve', 'revise'), score: s.number() }),
  });
  const next = g.agent('next', { agent: 'claude', prompt: 'y', output: s.object({ summary: s.string() }) });

  // A value outside the enum.
  // @ts-expect-error 'aprove' is not 'approve' | 'revise'
  g.edge(review, next, when(review.out.verdict, 'eq', 'aprove'));

  // A field the output schema does not have.
  // @ts-expect-error no such field
  g.edge(review, next, when(review.out.nope, 'truthy'));

  // lt/gt only on numbers.
  // @ts-expect-error verdict is not a number
  g.edge(review, next, when(review.out.verdict, 'lt', 3));

  // map over a value that is not an array.
  // @ts-expect-error input.task is a string
  g.map('m', { over: input.task }, (b) => b.agent('do', { agent: 'pi', prompt: 'z', output: s.object({ x: s.string() }) }));

  // The valid forms compile.
  g.edge(review, next, when(review.out.verdict, 'eq', 'approve'));
  g.edge(review, next, when(review.out.score, 'gt', 3));
  g.edge(review, next, when(review.out.verdict, 'in', ['approve', 'revise']));
});
