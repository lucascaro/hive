// Generic workflow constructs (spec 495). Each is a higher-order function: it
// takes node specs and options and returns a Fragment, which `g.use(id, …)`
// applies under an id of the caller's choosing. Every node a fragment adds
// is prefixed with that id, so the same construct can be used twice in one
// workflow and its node ids stay stable across rebuilds.

import {
  type AgentSpec,
  type CheckOut,
  type CheckSpec,
  type Fragment,
  type Handle,
  type Ref,
  type Scope,
  when,
} from './dsl.ts';
import { s } from './schema.ts';

export type Verdict = 'approve' | 'revise';

export interface ReviewLoop<W, R> {
  id: string;
  worker: Handle<W>;
  reviewer: Handle<R>;
}

/** worker → reviewer, repeated until the reviewer approves or `maxIters`
 * rounds have run. The worker sees the previous round's feedback (empty on
 * round 1); the reviewer sees the worker's summary and works in the worker's
 * worktree, so it reviews the actual change. */
export function reviewLoop<W extends { summary: string }, R extends { verdict: Verdict; feedback: string }>(
  worker: AgentSpec<W>,
  reviewer: AgentSpec<R>,
  opts: { maxIters: number },
): Fragment<ReviewLoop<W, R>> {
  return (b, id) => {
    let work: Handle<W> | undefined;
    let review: Handle<R> | undefined;
    const loop = b.loop(id, { maxIters: opts.maxIters }, (body) => {
      // The reviewer is added after the worker, so its feedback ref is
      // spelled out: inside a loop body it reads the previous round.
      work = body.agent('work', {
        ...worker,
        prompt: `${worker.prompt}\n\nReviewer feedback from the previous round, if any: {{${body.prefix}/review.feedback}}`,
      });
      review = body.agent('review', {
        ...reviewer,
        worktree: work,
        prompt: `${reviewer.prompt}\n\nThe worker reports: {{${work.out.summary.ref}}}`,
      });
      body.edge(work, review);
      return when(review.out.verdict as Ref<Verdict>, 'eq', 'approve');
    });
    return { id: loop.id, worker: work as Handle<W>, reviewer: review as Handle<R> };
  };
}

export interface FanOut<U> {
  id: string;
  each: Handle<{ results: U[] }>;
  verify: Handle<CheckOut>;
}

/** Runs `step` once per element of `items` (at most `concurrency` at a
 * time), then one check node over the combined result. */
export function fanOutThenVerify<T, U>(
  items: Ref<T[]>,
  step: (b: Scope, item: Ref<T>) => Handle<U>,
  verify: CheckSpec,
  opts: { concurrency?: number } = {},
): Fragment<FanOut<U>> {
  return (b, id) =>
    b.group(id, (g) => {
      const each = g.map('each', { over: items, concurrency: opts.concurrency }, step);
      const check = g.check('verify', verify);
      g.edge(each, check);
      return { each, verify: check };
    });
}

export interface PlanImplementVerifySpec {
  planner: AgentSpec<{ summary: string; steps: string[] }>;
  implementer: AgentSpec<{ summary: string }>;
  reviewer: AgentSpec<{ verdict: Verdict; feedback: string }>;
  check: CheckSpec;
  maxIters: number;
}

/** plan → human approval → (approved only) review-looped implementation →
 * check. A rejected plan skips everything after the approval node. */
export function planImplementVerify(spec: PlanImplementVerifySpec) {
  return ((b: Scope, id: string) =>
    b.group(id, (g) => {
      const plan = g.agent('plan', spec.planner);
      const approve = g.human('approve', {
        prompt: `Approve this plan?\n\n{{${plan.out.summary.ref}}}`,
        output: s.object({ decision: s.enum('approve', 'reject'), note: s.string() }),
      });
      const build = g.use(
        'build',
        reviewLoop(
          { ...spec.implementer, prompt: `${spec.implementer.prompt}\n\nApproved plan: {{${plan.out.steps.ref}}}` },
          spec.reviewer,
          { maxIters: spec.maxIters },
        ),
      );
      const tests = g.check('tests', spec.check);
      g.edge(plan, approve);
      g.edge(approve, build, when(approve.out.decision, 'eq', 'approve'));
      g.edge(build, tests);
      return { plan, approve, build, tests };
    })) satisfies Fragment<unknown>;
}
