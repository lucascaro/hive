// The worked examples in docs/design-docs/workflow-engine.md (spec 495).
// check-doc.ts regenerates each one's IR and Mermaid and fails when the doc
// differs, so the doc can only show graphs this prototype really builds.
//
// Between them they cover every construct the doc explains:
//   reviewLoop           — a bounded loop with a typed `until` condition
//   fanOutThenVerify     — dynamic fan-out (map) joined into a check node
//   planImplementVerify  — a conditional edge on a typed human decision, and
//                          subgraph composition (it nests reviewLoop)

import { type IR, p, workflow } from './dsl.ts';
import { fanOutThenVerify, planImplementVerify, reviewLoop } from './library.ts';
import { s } from './schema.ts';

const reviewer = {
  agent: 'codex',
  mode: 'read-only',
  prompt: 'Review the change on this branch. Approve only if it is correct and tested.',
  output: s.object({ verdict: s.enum('approve', 'revise'), feedback: s.string() }),
};

export const examples: Record<string, () => IR> = {
  reviewLoop: () =>
    workflow('review-loop', s.object({ task: s.string() }), (g, input) => {
      g.use(
        'fix',
        reviewLoop(
          {
            agent: 'claude',
            mode: 'acceptEdits',
            prompt: p`Implement this task: ${input.task}`,
            output: s.object({ summary: s.string() }),
          },
          reviewer,
          { maxIters: 3 },
        ),
      );
    }),

  fanOutThenVerify: () =>
    workflow('fan-out-then-verify', s.object({ goal: s.string() }), (g, input) => {
      const split = g.agent('split', {
        agent: 'claude',
        mode: 'plan',
        prompt: p`Split this goal into independent tasks: ${input.goal}`,
        output: s.object({ tasks: s.array(s.string()) }),
      });
      const work = g.use(
        'work',
        fanOutThenVerify(
          split.out.tasks,
          (b, task) =>
            b.agent('do', {
              agent: 'codex',
              mode: 'workspace-write',
              prompt: p`Do this task and nothing else: ${task}`,
              output: s.object({ summary: s.string() }),
            }),
          { argv: ['go', 'test', './...'] },
          { concurrency: 4 },
        ),
      );
      g.edge(split, work);
    }),

  planImplementVerify: () =>
    workflow('plan-implement-verify', s.object({ spec: s.string() }), (g, input) => {
      g.use(
        'feature',
        planImplementVerify({
          planner: {
            agent: 'claude',
            mode: 'plan',
            prompt: p`Write an implementation plan for: ${input.spec}`,
            output: s.object({ summary: s.string(), steps: s.array(s.string()) }),
          },
          implementer: {
            agent: 'pi',
            mode: 'unattended',
            prompt: 'Implement the approved plan on this branch.',
            output: s.object({ summary: s.string() }),
          },
          reviewer,
          check: { argv: ['scripts/test.sh', 'go'] },
          maxIters: 2,
        }),
      );
    }),
};
