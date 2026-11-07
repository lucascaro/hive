// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Workflow builder and IR for spec 495's prototype. Building a workflow runs
// ordinary TypeScript once and records a static graph (the IR): node kinds,
// edges, and conditions stored as data rather than closures. Nothing here
// executes an agent; see docs/design-docs/workflow-engine.md for the engine
// that would interpret this IR.

import { type JsonSchema, type ObjectSchema, type S, s } from './schema.ts';

// ---------------------------------------------------------------- references

/** A typed pointer at a value produced at run time: `<node-id>.<field>`,
 * `input.<field>`, or `$item` inside a map body. */
export interface Ref<T> {
  readonly ref: string;
  readonly __type?: T;
}

export type Refs<T> = { readonly [K in keyof T]-?: Ref<T[K]> };

export type Json = string | number | boolean | null | Json[] | { [k: string]: Json };

export type Op = 'eq' | 'ne' | 'in' | 'truthy' | 'lt' | 'gt';

export type Cond = { ref: string; op: Op; value?: Json } | { all: Cond[] } | { any: Cond[] } | { not: Cond };

export function when<T>(r: Ref<T>, op: 'eq' | 'ne', value: NoInfer<T>): Cond;
export function when<T>(r: Ref<T>, op: 'in', value: readonly NoInfer<T>[]): Cond;
export function when(r: Ref<number>, op: 'lt' | 'gt', value: number): Cond;
export function when(r: Ref<unknown>, op: 'truthy'): Cond;
export function when(r: Ref<unknown>, op: Op, value?: unknown): Cond {
  return value === undefined ? { ref: r.ref, op } : { ref: r.ref, op, value: value as Json };
}

export const all = (...conds: Cond[]): Cond => ({ all: conds });
export const any = (...conds: Cond[]): Cond => ({ any: conds });
export const not = (cond: Cond): Cond => ({ not: cond });

/** Prompt template: interpolated refs become `{{ref}}` placeholders that the
 * engine fills at run time; interpolated strings are inserted verbatim. */
export function p(strings: TemplateStringsArray, ...parts: (Ref<unknown> | string)[]): string {
  let out = strings[0];
  parts.forEach((part, i) => {
    out += (typeof part === 'string' ? part : `{{${part.ref}}}`) + strings[i + 1];
  });
  return out;
}

// ------------------------------------------------------------------------ IR

export interface Edge {
  from: string;
  to: string;
  when?: Cond;
}

export interface Body {
  nodes: Node[];
  edges: Edge[];
}

export interface AgentNode {
  id: string;
  kind: 'agent';
  agent: string;
  mode?: string;
  /** `own`: a fresh worktree for this node, branched from its upstream
   * node's branch. `{ of }`: the same worktree as another agent node, e.g. a
   * reviewer reading the worker's change. */
  worktree: 'own' | { of: string };
  timeout_s: number;
  prompt: string;
  output: ObjectSchema;
}

export interface CheckNode {
  id: string;
  kind: 'check';
  argv: string[];
  timeout_s: number;
}

export interface HumanNode {
  id: string;
  kind: 'human';
  prompt: string;
  output: ObjectSchema;
}

export interface GroupNode extends Body {
  id: string;
  kind: 'group';
}

export interface LoopNode extends Body {
  id: string;
  kind: 'loop';
  max_iters: number;
  until: Cond;
}

export interface MapNode extends Body {
  id: string;
  kind: 'map';
  over: string;
  concurrency: number;
}

export type Node = AgentNode | CheckNode | HumanNode | GroupNode | LoopNode | MapNode;

export interface IR extends Body {
  ir: 1;
  name: string;
  input: ObjectSchema;
}

export const MAX_ITERS = 20;
export const MAX_CONCURRENCY = 16;
const AGENT_TIMEOUT_S = 3600;
const CHECK_TIMEOUT_S = 600;

export type CheckOut = { ok: boolean; exit_code: number };
/** A loop's own output: whether `until` held before `max_iters` ran out. */
export type LoopOut = { converged: boolean; iterations: number };
export const LOOP_OUTPUT = s.object({ converged: s.boolean(), iterations: s.number() }).json as ObjectSchema;
export const CHECK_OUTPUT = s.object({ ok: s.boolean(), exit_code: s.number() }).json as ObjectSchema;

// ------------------------------------------------------------------- builder

export interface Handle<T = Record<never, never>> {
  readonly id: string;
  readonly out: Refs<T>;
}

export interface AgentSpec<T> {
  agent: string;
  prompt: string;
  output: S<T>;
  mode?: string;
  worktree?: 'own' | { id: string };
  timeoutSec?: number;
}

export interface CheckSpec {
  argv: string[];
  timeoutSec?: number;
}

export interface HumanSpec<T> {
  prompt: string;
  output: S<T>;
}

/** A reusable construct: given a scope and an id, it adds one node (usually
 * a group, loop or map holding a subgraph) and returns typed handles. */
export type Fragment<H> = (b: Scope, id: string) => H;

function handle<T>(id: string, schema: JsonSchema): Handle<T> {
  const out: Record<string, Ref<unknown>> = {};
  if (schema.type === 'object') for (const k of Object.keys(schema.properties)) out[k] = { ref: `${id}.${k}` };
  return { id, out: out as Refs<T> };
}

export class Scope {
  readonly prefix: string;
  readonly #body: Body;

  constructor(prefix: string, body: Body) {
    this.prefix = prefix;
    this.#body = body;
  }

  #full(id: string): string {
    return this.prefix ? `${this.prefix}/${id}` : id;
  }

  agent<T extends Record<string, unknown>>(id: string, spec: AgentSpec<T>): Handle<T> {
    const node: AgentNode = {
      id: this.#full(id),
      kind: 'agent',
      agent: spec.agent,
      ...(spec.mode === undefined ? {} : { mode: spec.mode }),
      worktree: spec.worktree === undefined || spec.worktree === 'own' ? 'own' : { of: spec.worktree.id },
      timeout_s: spec.timeoutSec ?? AGENT_TIMEOUT_S,
      prompt: spec.prompt,
      output: spec.output.json as ObjectSchema,
    };
    this.#body.nodes.push(node);
    return handle<T>(node.id, node.output);
  }

  check(id: string, spec: CheckSpec): Handle<CheckOut> {
    const node: CheckNode = {
      id: this.#full(id),
      kind: 'check',
      argv: spec.argv,
      timeout_s: spec.timeoutSec ?? CHECK_TIMEOUT_S,
    };
    this.#body.nodes.push(node);
    return handle<CheckOut>(node.id, CHECK_OUTPUT);
  }

  human<T extends Record<string, unknown>>(id: string, spec: HumanSpec<T>): Handle<T> {
    const node: HumanNode = { id: this.#full(id), kind: 'human', prompt: spec.prompt, output: spec.output.json as ObjectSchema };
    this.#body.nodes.push(node);
    return handle<T>(node.id, node.output);
  }

  /** A named subgraph. It starts when its incoming edges are taken and is
   * done when every node inside it has finished or been skipped. */
  group<H>(id: string, body: (b: Scope) => H): H & { id: string } {
    const node: GroupNode = { id: this.#full(id), kind: 'group', nodes: [], edges: [] };
    this.#body.nodes.push(node);
    return { ...body(new Scope(node.id, node)), id: node.id };
  }

  /** Runs `body` again until the returned condition holds, at most
   * `maxIters` times. The bound is part of the graph, not runtime state.
   * Its output says which way it ended: `converged` is false when the bound
   * ran out first, so the graph can route that case explicitly. */
  loop(id: string, opts: { maxIters: number }, body: (b: Scope) => Cond): Handle<LoopOut> {
    const node: LoopNode = {
      id: this.#full(id),
      kind: 'loop',
      max_iters: opts.maxIters,
      until: { ref: '', op: 'truthy' },
      nodes: [],
      edges: [],
    };
    this.#body.nodes.push(node);
    node.until = body(new Scope(node.id, node));
    return handle<LoopOut>(node.id, LOOP_OUTPUT);
  }

  /** Runs one copy of the template subgraph per element of `over`. The
   * template is static; only the number of copies is known at run time. */
  map<T, U>(
    id: string,
    opts: { over: Ref<T[]>; concurrency?: number },
    body: (b: Scope, item: Ref<T>) => Handle<U>,
  ): Handle<{ results: U[] }> {
    const node: MapNode = {
      id: this.#full(id),
      kind: 'map',
      over: opts.over.ref,
      concurrency: opts.concurrency ?? 1,
      nodes: [],
      edges: [],
    };
    this.#body.nodes.push(node);
    const sink = body(new Scope(node.id, node), { ref: '$item' });
    // validate() types the map's results from the template's one sink, so the
    // handle the body returns has to be that sink (zero or several sinks are
    // reported by validate()).
    const sinkIds = node.nodes.map((n) => n.id).filter((id) => !node.edges.some((e) => e.from === id));
    if (sinkIds.length === 1 && sink.id !== sinkIds[0]) {
      throw new Error(`map ${node.id}: the body must return its sink "${sinkIds[0]}", not "${sink.id}"`);
    }
    return { id: node.id, out: { results: { ref: `${node.id}.results` } } as Refs<{ results: U[] }> };
  }

  /** Applies a reusable fragment under `id`. */
  use<H>(id: string, fragment: Fragment<H>): H {
    return fragment(this, id);
  }

  edge(from: { id: string }, to: { id: string }, cond?: Cond): void {
    this.#body.edges.push(cond === undefined ? { from: from.id, to: to.id } : { from: from.id, to: to.id, when: cond });
  }
}

export function workflow<I extends Record<string, unknown>>(
  name: string,
  input: S<I>,
  build: (g: Scope, input: Refs<I>) => void,
): IR {
  const ir: IR = { ir: 1, name, input: input.json as ObjectSchema, nodes: [], edges: [] };
  build(new Scope('', ir), handle<I>('input', ir.input).out);
  return ir;
}
