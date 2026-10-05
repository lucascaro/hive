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
   * `maxIters` times. The bound is part of the graph, not runtime state. */
  loop(id: string, opts: { maxIters: number }, body: (b: Scope) => Cond): Handle {
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
    return { id: node.id, out: {} };
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
    body(new Scope(node.id, node), { ref: '$item' });
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

// ---------------------------------------------------------------- validation

// No leading, trailing or doubled hyphens, so mermaid.ts can map ids to
// node names (`-` → `_`, `/` → `__`) without collisions.
const ID_SEGMENT = /^[a-z][a-z0-9]*(-[a-z0-9]+)*$/;

interface Placed {
  node: Node;
  parent: string; // container id, '' at the top level
  map: string | null; // nearest enclosing map id
}

function walk(body: Body, parent: string, map: string | null, into: Placed[]): void {
  for (const node of body.nodes) {
    into.push({ node, parent, map });
    if ('nodes' in node) walk(node, node.id, node.kind === 'map' ? node.id : map, into);
  }
}

function sinks(body: Body): string[] {
  const from = new Set(body.edges.map((e) => e.from));
  return body.nodes.filter((n) => !from.has(n.id)).map((n) => n.id);
}

function outputOf(placed: Map<string, Placed>, n: Node): JsonSchema | null {
  switch (n.kind) {
    case 'agent':
    case 'human':
      return n.output;
    case 'check':
      return CHECK_OUTPUT;
    case 'map': {
      const sink = sinks(n);
      const s0 = sink.length === 1 ? placed.get(sink[0]) : undefined;
      const items = s0 ? outputOf(placed, s0.node) : null;
      return items ? { type: 'object', properties: { results: { type: 'array', items } }, required: ['results'], additionalProperties: false } : null;
    }
    default:
      return null;
  }
}

/** Returns every structural error in the IR; an empty list means valid. */
export function validate(ir: IR): string[] {
  const errors: string[] = [];
  const list: Placed[] = [];
  walk(ir, '', null, list);
  const placed = new Map<string, Placed>();

  for (const p of list) {
    const { node, parent } = p;
    const local = parent ? node.id.slice(parent.length + 1) : node.id;
    if ((parent && !node.id.startsWith(`${parent}/`)) || !ID_SEGMENT.test(local)) {
      errors.push(`${node.id}: id must be ${parent ? `"${parent}/<segment>"` : 'one segment'} of [a-z0-9] words joined by single hyphens`);
    }
    if (node.id === 'input') errors.push('input: reserved id');
    if (placed.has(node.id)) errors.push(`${node.id}: duplicate id`);
    placed.set(node.id, p);
  }

  // Resolves a ref seen from inside `ctx` (a container id, '' for top level).
  const resolve = (ref: string, ctx: string, where: string): JsonSchema | null => {
    const fail = (why: string) => {
      errors.push(`${where}: ref "${ref}" ${why}`);
      return null;
    };
    if (ref === '$item' || ref.startsWith('$item.')) {
      const mapId = enclosingMap(ctx);
      if (!mapId) return fail('used outside a map');
      const m = placed.get(mapId)?.node as MapNode;
      const arr = resolve(m.over, placed.get(mapId)?.parent ?? '', `${mapId}.over`);
      if (!arr || arr.type !== 'array') return null;
      return ref === '$item' ? arr.items : field(arr.items, ref.slice('$item.'.length), fail);
    }
    const dot = ref.lastIndexOf('.');
    if (dot < 1) return fail('is not "<node>.<field>"');
    const target = ref.slice(0, dot);
    const name = ref.slice(dot + 1);
    if (target === 'input') return field(ir.input, name, fail);
    const t = placed.get(target);
    if (!t) return fail('names no node');
    if (t.map && ctx !== t.map && !ctx.startsWith(`${t.map}/`)) return fail(`reaches into map "${t.map}" from outside it`);
    const schema = outputOf(placed, t.node);
    if (!schema) return fail(`names a ${t.node.kind} node, which has no output`);
    return field(schema, name, fail);
  };

  const enclosingMap = (ctx: string): string | null => {
    for (let id = ctx; id; id = placed.get(id)?.parent ?? '') {
      if (placed.get(id)?.node.kind === 'map') return id;
    }
    return null;
  };

  const checkCond = (c: Cond, ctx: string, where: string): void => {
    if ('all' in c) return c.all.forEach((x) => checkCond(x, ctx, where));
    if ('any' in c) return c.any.forEach((x) => checkCond(x, ctx, where));
    if ('not' in c) return checkCond(c.not, ctx, where);
    const schema = resolve(c.ref, ctx, where);
    if (!schema) return;
    const bad = (why: string) => errors.push(`${where}: condition on "${c.ref}" ${why}`);
    if (c.op === 'truthy') {
      if (c.value !== undefined) bad('takes no value for truthy');
    } else if (c.op === 'lt' || c.op === 'gt') {
      if (schema.type !== 'number' || typeof c.value !== 'number') bad(`needs a number field and value for ${c.op}`);
    } else if (c.op === 'in') {
      if (!Array.isArray(c.value)) bad('needs an array value for in');
      else c.value.forEach((v) => fits(schema, v) || bad(`value ${JSON.stringify(v)} does not fit the field`));
    } else if (!fits(schema, c.value)) {
      bad(`value ${JSON.stringify(c.value)} does not fit the field`);
    }
  };

  const checkPrompt = (prompt: string, ctx: string, where: string): void => {
    for (const m of prompt.matchAll(/\{\{([^}]*)\}\}/g)) resolve(m[1], ctx, where);
  };

  const checkBody = (body: Body, ctx: string): void => {
    const ids = new Set(body.nodes.map((n) => n.id));
    const seen = new Set<string>();
    for (const e of body.edges) {
      const where = `edge ${e.from} -> ${e.to}`;
      if (!ids.has(e.from) || !ids.has(e.to)) errors.push(`${where}: both ends must be nodes of the same subgraph`);
      if (e.from === e.to) errors.push(`${where}: self-edge`);
      if (seen.has(`${e.from}>${e.to}`)) errors.push(`${where}: duplicate edge`);
      seen.add(`${e.from}>${e.to}`);
      if (e.when) checkCond(e.when, ctx, where);
    }
    const cycle = findCycle(body);
    if (cycle) errors.push(`${ctx || ir.name}: cycle ${cycle.join(' -> ')}; use loop() for repetition`);
  };

  checkBody(ir, '');
  for (const { node } of list) {
    switch (node.kind) {
      case 'agent':
        if (!node.agent) errors.push(`${node.id}: agent is empty`);
        if (node.output.type !== 'object') errors.push(`${node.id}: output must be an object schema`);
        if (!(node.timeout_s > 0)) errors.push(`${node.id}: timeout_s must be > 0`);
        if (node.worktree !== 'own') {
          const of = placed.get(node.worktree.of);
          if (!of || of.node.kind !== 'agent' || of.node.id === node.id) {
            errors.push(`${node.id}: worktree.of "${node.worktree.of}" must name another agent node`);
          } else if (of.map && !node.id.startsWith(`${of.map}/`)) {
            errors.push(`${node.id}: worktree.of reaches into map "${of.map}" from outside it`);
          }
        }
        checkPrompt(node.prompt, node.id, node.id);
        break;
      case 'human':
        if (node.output.type !== 'object') errors.push(`${node.id}: output must be an object schema`);
        checkPrompt(node.prompt, node.id, node.id);
        break;
      case 'check':
        if (node.argv.length === 0) errors.push(`${node.id}: argv is empty`);
        if (!(node.timeout_s > 0)) errors.push(`${node.id}: timeout_s must be > 0`);
        break;
      case 'group':
      case 'loop':
      case 'map':
        if (node.nodes.length === 0) errors.push(`${node.id}: empty ${node.kind}`);
        checkBody(node, node.id);
        if (node.kind === 'loop') {
          if (!Number.isInteger(node.max_iters) || node.max_iters < 1 || node.max_iters > MAX_ITERS) {
            errors.push(`${node.id}: max_iters must be an integer in 1..${MAX_ITERS}`);
          }
          checkCond(node.until, node.id, `${node.id}.until`);
        }
        if (node.kind === 'map') {
          if (!Number.isInteger(node.concurrency) || node.concurrency < 1 || node.concurrency > MAX_CONCURRENCY) {
            errors.push(`${node.id}: concurrency must be an integer in 1..${MAX_CONCURRENCY}`);
          }
          const over = resolve(node.over, placed.get(node.id)?.parent ?? '', `${node.id}.over`);
          if (over && over.type !== 'array') errors.push(`${node.id}: over "${node.over}" is not an array`);
          if (node.nodes.length > 0 && sinks(node).length !== 1) errors.push(`${node.id}: template must have exactly one sink`);
        }
        break;
    }
  }
  return errors;
}

function field(schema: JsonSchema, name: string, fail: (why: string) => null): JsonSchema | null {
  if (schema.type !== 'object' || !(name in schema.properties)) return fail(`has no field "${name}"`);
  return schema.properties[name];
}

function fits(schema: JsonSchema, v: unknown): boolean {
  switch (schema.type) {
    case 'string':
      return typeof v === 'string' && (!schema.enum || schema.enum.includes(v));
    case 'number':
      return typeof v === 'number';
    case 'boolean':
      return typeof v === 'boolean';
    default:
      return false; // conditions compare scalars only
  }
}

function findCycle(body: Body): string[] | null {
  const next = new Map<string, string[]>();
  for (const e of body.edges) next.set(e.from, [...(next.get(e.from) ?? []), e.to]);
  const state = new Map<string, 'open' | 'done'>();
  const path: string[] = [];
  const visit = (id: string): string[] | null => {
    if (state.get(id) === 'done') return null;
    if (state.get(id) === 'open') return [...path.slice(path.indexOf(id)), id];
    state.set(id, 'open');
    path.push(id);
    for (const to of next.get(id) ?? []) {
      const c = visit(to);
      if (c) return c;
    }
    path.pop();
    state.set(id, 'done');
    return null;
  };
  for (const n of body.nodes) {
    const c = visit(n.id);
    if (c) return c;
  }
  return null;
}
