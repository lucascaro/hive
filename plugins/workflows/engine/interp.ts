// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The interpreter (spec 529, Run semantics). It is a pure reducer over a run's
// node rows: `step(model)` says which transitions are due and which effects
// to start, and `apply(model, t)` turns one transition into the rows and
// events to commit. Nothing here does I/O, so every rule is a table-driven
// test, and crash recovery is the same reducer run over whatever was last
// committed. The engine (engine.ts) owns the effects.

import type { AgentNode, Cond, Edge, GroupNode, IR, LoopNode, MapNode, Node } from '../sdk/dsl.ts';
import { encodedSize, truncateEncoded } from './size.ts';
import { type EventDraft, MAX_EVENT_BYTES, type Handles } from './events.ts';
import { buildIndex, type Index, keyOf, parseKey } from './keys.ts';
import type { Change, NodeState, Row, RunState } from './model.ts';
import { redactString, redactValue } from './redact.ts';

/** An `interrupted` node gets one more attempt (spec 529, Attempts); a timeout or a bad answer never does. */
export const MAX_ATTEMPTS = 2;
export const MAX_MAP_COPIES = 32;
export const MAX_AGENT_TURNS = 50;
/** A throttle on starting agents, not a failure (spec correction C-1). */
export const MAX_LIVE_AGENTS = 4;
/** A rendered human prompt, as sent (B-15). */
export const MAX_PROMPT_BYTES = 16 << 10;
/** A stored error text, as sent. */
export const MAX_ERROR_BYTES = 2 << 10;

type Data = Record<string, unknown>;

export interface Model {
  ix: Index;
  input: Data;
  rows: Map<string, Row>;
  state: RunState;
}

export function modelFor(ir: IR, input: Data, rows: Iterable<Row>, state: RunState = 'running'): Model {
  return { ix: buildIndex(ir), input, rows: new Map([...rows].map((r) => [r.key, r])), state };
}

export type Action = { a: 'check' | 'agent'; key: string; node_id: string };

export type Transition =
  | { t: 'ready'; key: string }
  | { t: 'skip'; key: string }
  /** check/agent: after the process or session exists (`patch` records it); containers and humans: at once. */
  | { t: 'start'; key: string; patch?: Partial<Row>; base_sha?: string }
  | { t: 'iterate'; key: string }
  | { t: 'finish'; key: string; output?: unknown }
  | { t: 'fail'; key: string; error: string; cap?: string; conflict?: Row['conflict'] }
  | { t: 'interrupt'; key: string }
  | { t: 'wait'; key: string; waiting: Data }
  | { t: 'resume'; key: string; reason: 'permission' }
  | { t: 'turn'; key: string }
  | { t: 'run.fail' }
  | { t: 'run.cancel' }
  | { t: 'run.retry' }
  | { t: 'run.done' };

export interface Step {
  transitions: Transition[];
  actions: Action[];
}

const settled = (s: NodeState): boolean => s === 'done' || s === 'skipped' || s === 'failed';
const isRecord = (v: unknown): v is Data => typeof v === 'object' && v !== null && !Array.isArray(v);

export function newRow(key: string, node_id: string): Row {
  return { key, node_id, state: 'pending', attempt: 0, ready: false, progress: 0, turns: 0 };
}

/** The rows a body brings into being with its container: every node, and the insides of groups (loops and maps make theirs when they start). */
function materialize(ix: Index, nodes: readonly Node[], ctx: readonly number[], into: Row[]): void {
  for (const n of nodes) {
    into.push(newRow(keyOf(ix, n.id, ctx), n.id));
    if (n.kind === 'group') materialize(ix, n.nodes, ctx, into);
  }
}

/** The rows a run starts with. */
export function initialRows(ir: IR): Row[] {
  const rows: Row[] = [];
  materialize(buildIndex(ir), ir.nodes, [], rows);
  return rows;
}

// ---------------------------------------------------------------- references

interface Scope {
  /** Ids of the enclosing loops and maps, outermost first. */
  lm: readonly string[];
  /** The loop iteration or map index for each of them. */
  ctx: readonly number[];
}

/**
 * Reads `<node>.<field>`, `input.<field>` or `$item[.<field>]` from where a
 * reader stands. A node that was skipped, failed or never ran reads as
 * `undefined`; so does a back-reference on a loop's first iteration.
 */
export function resolveRef(m: Model, ref: string, scope: Scope): unknown {
  const { ix } = m;
  if (ref === '$item' || ref.startsWith('$item.')) {
    let j = scope.lm.length - 1;
    while (j >= 0 && ix.byId.get(scope.lm[j])?.node.kind !== 'map') j--;
    if (j < 0) return undefined;
    const mapNode = ix.byId.get(scope.lm[j])?.node as MapNode;
    const arr = resolveRef(m, mapNode.over, { lm: scope.lm.slice(0, j), ctx: scope.ctx.slice(0, j) });
    const item = Array.isArray(arr) ? arr[scope.ctx[j]] : undefined;
    return ref === '$item' ? item : isRecord(item) ? item[ref.slice('$item.'.length)] : undefined;
  }
  const dot = ref.lastIndexOf('.');
  if (dot < 1) return undefined;
  const target = ref.slice(0, dot);
  const name = ref.slice(dot + 1);
  if (target === 'input') return m.input[name];
  const placed = ix.byId.get(target);
  if (!placed) return undefined;
  const tlm = placed.lm;
  const shared = (p: number): boolean => p < scope.lm.length && scope.lm.slice(0, p + 1).every((id, i) => id === tlm[i]);

  // The target's instance context. Shared loops and maps use the reader's
  // index; a loop only the target is in reads its latest iteration. `back`
  // reads one iteration earlier on the innermost shared loop that has one.
  const ctxFor = (back: number): number[] | null => {
    const out: number[] = [];
    for (let p = 0; p < tlm.length; p++) {
      if (shared(p)) out.push(scope.ctx[p] - (p === back ? 1 : 0));
      else if (ix.byId.get(tlm[p])?.node.kind === 'loop') {
        out.push(m.rows.get(keyOf(ix, tlm[p], out))?.progress ?? 0);
      } else return null; // reaches into a map from outside: validate refuses it
    }
    return out;
  };
  const read = (ctx: number[] | null): Row | undefined => (ctx ? m.rows.get(keyOf(ix, target, ctx)) : undefined);
  const out = (row: Row | undefined): unknown => (row?.state === 'done' && isRecord(row.output_raw) ? row.output_raw[name] : undefined);

  const row = read(ctxFor(-1));
  if (!row || settled(row.state)) return out(row);
  // Not finished: a node later in the same loop body, read as last iteration.
  for (let p = tlm.length - 1; p >= 0; p--) {
    if (shared(p) && ix.byId.get(tlm[p])?.node.kind === 'loop') return scope.ctx[p] > 1 ? out(read(ctxFor(p))) : undefined;
  }
  return undefined;
}

export function evalCond(m: Model, c: Cond, scope: Scope): boolean {
  if ('all' in c) return c.all.every((x) => evalCond(m, x, scope));
  if ('any' in c) return c.any.some((x) => evalCond(m, x, scope));
  if ('not' in c) return !evalCond(m, c.not, scope);
  const v = resolveRef(m, c.ref, scope);
  switch (c.op) {
    case 'truthy':
      return Boolean(v);
    case 'eq':
      return v !== undefined && v === c.value;
    case 'ne':
      return v !== c.value;
    case 'in':
      return v !== undefined && Array.isArray(c.value) && c.value.includes(v as never);
    case 'lt':
      return typeof v === 'number' && typeof c.value === 'number' && v < c.value;
    case 'gt':
      return typeof v === 'number' && typeof c.value === 'number' && v > c.value;
  }
}

/** A prompt with its `{{ref}}` placeholders filled. Strings go in as they are, other values as JSON, an empty reference as nothing. */
export function renderPrompt(m: Model, prompt: string, scope: Scope): string {
  return prompt.replace(/\{\{([^}]*)\}\}/g, (_, ref: string) => {
    const v = resolveRef(m, ref, scope);
    return v === undefined ? '' : typeof v === 'string' ? v : JSON.stringify(v);
  });
}

export function scopeOf(m: Model, key: string): Scope | null {
  const p = parseKey(m.ix, key);
  const placed = p && m.ix.byId.get(p.id);
  return p && placed ? { lm: placed.lm, ctx: p.ctx } : null;
}

// ---------------------------------------------------------------------- step

/**
 * What is due now. `inflight` holds the keys whose effect the engine has
 * started but whose transition has not committed (an agent being created, a
 * check being launched), so the live-agent throttle counts them and no effect
 * is started twice.
 */
export function step(m: Model, inflight: ReadonlySet<string> = new Set()): Step {
  const out: Step = { transitions: [], actions: [] };
  if (m.state !== 'running' && m.state !== 'waiting') return out;
  for (const r of m.rows.values()) {
    if (r.state === 'failed') {
      out.transitions.push({ t: 'run.fail' });
      return out;
    }
  }
  const { ix } = m;
  const kindOf = (r: Row): string => ix.byId.get(r.node_id)?.node.kind ?? '';
  let live = 0;
  for (const r of m.rows.values()) {
    const launching = inflight.has(r.key) && (r.state === 'pending' || r.state === 'interrupted');
    if (kindOf(r) === 'agent' && (r.state === 'running' || r.state === 'waiting' || launching)) live++;
  }

  const body = (nodes: readonly Node[], edges: readonly Edge[], scope: Scope, active: boolean): void => {
    for (const n of nodes) {
      const row = m.rows.get(keyOf(ix, n.id, scope.ctx));
      if (!row) continue;
      if (row.state === 'interrupted') {
        if (row.attempt >= MAX_ATTEMPTS) {
          out.transitions.push({ t: 'fail', key: row.key, error: `interrupted ${row.attempt} times` });
        } else if (!inflight.has(row.key) && (n.kind === 'check' || n.kind === 'agent')) {
          out.actions.push({ a: n.kind, key: row.key, node_id: n.id });
        }
      } else if (row.state === 'pending') {
        const incoming = edges.filter((e) => e.to === n.id);
        let isReady = active;
        let taken = true;
        if (incoming.length > 0) {
          const from = incoming.map((e) => m.rows.get(keyOf(ix, e.from, scope.ctx)));
          isReady = from.every((r) => r !== undefined && settled(r.state));
          taken = incoming.some((e, i) => from[i]?.state === 'done' && (!e.when || evalCond(m, e.when, scope)));
        }
        if (!isReady) continue;
        if (!taken) out.transitions.push({ t: 'skip', key: row.key });
        else if (!row.ready) out.transitions.push({ t: 'ready', key: row.key });
        else if (n.kind === 'check' || n.kind === 'agent') {
          if (inflight.has(row.key)) continue;
          if (n.kind === 'agent' && live >= MAX_LIVE_AGENTS) continue;
          if (n.kind === 'agent') live++;
          out.actions.push({ a: n.kind, key: row.key, node_id: n.id });
        } else out.transitions.push({ t: 'start', key: row.key });
      } else if (row.state === 'running' && 'nodes' in n) {
        container(n, row, scope);
      }
    }
  };

  const allSettled = (nodes: readonly Node[], ctx: readonly number[]): boolean =>
    nodes.every((c) => settled(m.rows.get(keyOf(ix, c.id, ctx))?.state ?? 'pending'));

  const container = (n: GroupNode | LoopNode | MapNode, row: Row, scope: Scope): void => {
    if (n.kind === 'group') {
      body(n.nodes, n.edges, scope, true);
      if (allSettled(n.nodes, scope.ctx)) out.transitions.push({ t: 'finish', key: row.key });
    } else if (n.kind === 'loop') {
      const k = row.progress;
      const inner: Scope = { lm: [...scope.lm, n.id], ctx: [...scope.ctx, k] };
      body(n.nodes, n.edges, inner, true);
      if (!allSettled(n.nodes, inner.ctx)) return;
      const converged = evalCond(m, n.until, inner);
      if (converged || k >= n.max_iters) out.transitions.push({ t: 'finish', key: row.key, output: { converged, iterations: k } });
      else out.transitions.push({ t: 'iterate', key: row.key });
    } else if (n.kind === 'map') {
      const sink = n.nodes.find((c) => !n.edges.some((e) => e.from === c.id));
      let admitted = 0;
      const copies = Array.from({ length: row.progress }, (_, i) => i);
      const started = (i: number): boolean =>
        n.nodes.some((c) => {
          const r = m.rows.get(keyOf(ix, c.id, [...scope.ctx, i]));
          return r !== undefined && (r.state !== 'pending' || r.ready);
        });
      for (const i of copies) if (started(i) && !allSettled(n.nodes, [...scope.ctx, i])) admitted++;
      for (const i of copies) {
        let active = started(i);
        if (!active && admitted < n.concurrency) {
          active = true;
          admitted++;
        }
        body(n.nodes, n.edges, { lm: [...scope.lm, n.id], ctx: [...scope.ctx, i] }, active);
      }
      if (copies.every((i) => allSettled(n.nodes, [...scope.ctx, i]))) {
        const results = copies.map((i) => {
          const r = sink && m.rows.get(keyOf(ix, sink.id, [...scope.ctx, i]));
          return r?.state === 'done' && r.output_raw !== undefined ? r.output_raw : null;
        });
        out.transitions.push({ t: 'finish', key: row.key, output: { results } });
      }
    }
  };

  body(m.ix.ir.nodes, m.ix.ir.edges, { lm: [], ctx: [] }, true);
  if (out.transitions.length === 0 && out.actions.length === 0 && m.ix.ir.nodes.every((n) => settled(m.rows.get(keyOf(ix, n.id, []))?.state ?? 'pending'))) {
    out.transitions.push({ t: 'run.done' });
  }
  return out;
}

// --------------------------------------------------------------------- apply

export interface Applied {
  change: Change;
  /** Rows whose process or session must be stopped: they were running or waiting and are now cancelled. */
  cancelled: Row[];
}

export interface ApplyDeps {
  now: () => string;
  handles?: Handles;
}

function deriveRunState(rows: Iterable<Row>): RunState {
  for (const r of rows) if (r.state === 'waiting') return 'waiting';
  return 'running';
}

const clip = (s: string): string => truncateEncoded(redactString(s).text, MAX_ERROR_BYTES).text;

/** Turns one transition into the rows and events to commit. Does not touch the model: the engine does, after the commit lands. */
export function apply(m: Model, t: Transition, deps: ApplyDeps): Applied {
  const { ix } = m;
  const rows: Row[] = [];
  const events: EventDraft[] = [];
  const cancelled: Row[] = [];
  let run: Change['run'];
  const now = deps.now();

  const get = (key: string): Row => {
    const r = m.rows.get(key);
    if (!r) throw new Error(`no node instance ${key}`);
    return r;
  };
  const nodeOf = (r: Row): Node => ix.byId.get(r.node_id)?.node as Node;
  const ev = (r: Row, type: EventDraft['type'], data: Data = {}): void => {
    events.push({ key: r.key, node_id: r.node_id, attempt: r.attempt, type, data });
  };
  const put = (r: Row, patch: Partial<Row>): Row => {
    const next = { ...r, ...patch };
    rows.push(next);
    return next;
  };
  const finished = (r: Row, state: 'skipped' | 'cancelled' | 'interrupted', patch: Partial<Row> = {}): Row => {
    const next = put(r, { state, ended_at: now, waiting: undefined, ...patch });
    ev(next, 'node.finished', { state });
    return next;
  };
  const failRow = (r: Row, error: string, cap?: string, conflict?: Row['conflict']): Row => {
    const text = clip(error);
    const next = put(r, { state: 'failed', error: text, ended_at: now, waiting: undefined, ...(cap ? { cap } : {}), ...(conflict ? { conflict } : {}) });
    ev(next, 'node.finished', { state: 'failed', error: text, ...(cap ? { cap } : {}), ...(conflict ? { conflict } : {}) });
    return next;
  };
  /** Every row that is running or waiting stops where it is. */
  const cancelLive = (): void => {
    for (const r of m.rows.values()) {
      if (r.state !== 'running' && r.state !== 'waiting') continue;
      cancelled.push(r);
      finished(r, 'cancelled');
    }
  };
  const done = (r: Row, output: unknown): Row => {
    const red = output === undefined ? undefined : redactValue(output);
    const next = put(r, { state: 'done', ended_at: now, waiting: undefined, output_raw: output, output: red?.value });
    if (red) ev(next, 'node.output', { output: red.value });
    ev(next, 'node.finished', { state: 'done' });
    return next;
  };
  const waitingState = (r: Row, waiting: Data): Row => {
    const next = put(r, { state: 'waiting', waiting });
    ev(next, 'node.waiting', waiting);
    return next;
  };

  switch (t.t) {
    case 'ready': {
      const r = put(get(t.key), { ready: true });
      ev(r, 'node.ready');
      break;
    }
    case 'skip': {
      const r = get(t.key);
      finished(r, 'skipped');
      // A skipped group skips what is inside it.
      for (const inner of m.rows.values()) if (inner.key.startsWith(`${r.key}/`) && inner.state === 'pending') finished(inner, 'skipped');
      break;
    }
    case 'start': {
      const r = get(t.key);
      const n = nodeOf(r);
      const base = { state: 'running' as const, started_at: now, ended_at: undefined, error: undefined, ...t.patch };
      // The run pins its base with the first node that needed one, in the same commit (spec 529, R4).
      if (t.base_sha) run = { base_sha: t.base_sha };
      if (n.kind === 'check' || n.kind === 'agent') {
        const next = put(r, { ...base, attempt: r.attempt + 1 });
        ev(next, 'node.started', { kind: n.kind, ...(next.branch ? { branch: next.branch } : {}), ...(next.worktree ? { worktree: next.worktree } : {}), ...(next.session_id ? { session_id: next.session_id } : {}) });
      } else if (n.kind === 'human') {
        const sc = scopeOf(m, r.key) as Scope;
        const prompt = renderPrompt(m, n.prompt, sc);
        const waiting: Data = { reason: 'human', prompt, output_schema: n.output };
        const started = put(r, { ...base, attempt: r.attempt + 1 });
        ev(started, 'node.started', { kind: 'human' });
        if (encodedSize(prompt) > MAX_PROMPT_BYTES) {
          failRow(started, `the prompt is ${encodedSize(prompt)} bytes as sent, the limit is ${MAX_PROMPT_BYTES}`, 'prompt_too_large');
        } else if (encodedSize(waiting) > MAX_EVENT_BYTES) {
          failRow(started, 'the prompt with its answer schema is too large to show', 'waiting_too_large');
        } else {
          const w = redactValue(waiting);
          waitingState(started, w.count > 0 ? { ...w.value, redacted: w.count } : w.value);
        }
      } else if (n.kind === 'group') {
        ev(put(r, base), 'node.started', { kind: 'group' });
      } else if (n.kind === 'loop') {
        const started = put(r, { ...base, progress: Math.max(r.progress, 1) });
        ev(started, 'node.started', { kind: 'loop' });
        if (r.progress === 0) {
          const ctx = [...(scopeOf(m, r.key) as Scope).ctx, 1];
          materialize(ix, n.nodes, ctx, rows);
          ev(started, 'loop.iteration', { iteration: 1, max_iters: n.max_iters });
        }
      } else if (n.kind === 'map') {
        const sc = scopeOf(m, r.key) as Scope;
        const first = r.progress === 0;
        const over = first ? resolveRef(m, n.over, sc) : undefined;
        const items = Array.isArray(over) ? over : [];
        const tooMany = first && items.length > MAX_MAP_COPIES;
        const started = put(r, { ...base, progress: first && !tooMany ? items.length : r.progress });
        ev(started, 'node.started', { kind: 'map' });
        if (tooMany) {
          failRow(started, `${n.id} would expand into ${items.length} copies, the limit is ${MAX_MAP_COPIES}`, 'map_copies');
        } else if (first) {
          ev(started, 'map.expanded', { count: items.length, concurrency: n.concurrency });
          items.forEach((_, i) => materialize(ix, n.nodes, [...sc.ctx, i], rows));
          if (items.length === 0) done(started, { results: [] });
        }
      }
      break;
    }
    case 'iterate': {
      const r = get(t.key);
      const n = nodeOf(r) as Node & { max_iters: number; nodes: Node[] };
      const next = put(r, { progress: r.progress + 1 });
      materialize(ix, n.nodes, [...(scopeOf(m, r.key) as Scope).ctx, next.progress], rows);
      ev(next, 'loop.iteration', { iteration: next.progress, max_iters: n.max_iters });
      break;
    }
    case 'finish':
      done(get(t.key), t.output);
      break;
    case 'fail':
      failRow(get(t.key), t.error, t.cap, t.conflict);
      break;
    case 'interrupt':
      finished(get(t.key), 'interrupted');
      break;
    case 'wait':
      waitingState(get(t.key), t.waiting);
      break;
    case 'resume': {
      const r = put(get(t.key), { state: 'running', waiting: undefined });
      ev(r, 'node.resumed', { reason: t.reason });
      break;
    }
    case 'turn': {
      const r = get(t.key);
      let total = 0;
      for (const x of m.rows.values()) total += x.turns;
      if (total + 1 > MAX_AGENT_TURNS) failRow(r, `the run would pass ${MAX_AGENT_TURNS} agent turns`, 'agent_turns');
      else put(r, { turns: r.turns + 1 });
      break;
    }
    case 'run.fail': {
      const bad = [...m.rows.values()].find((r) => r.state === 'failed');
      cancelLive();
      const error = bad ? `${bad.key}: ${bad.error ?? 'failed'}` : 'failed';
      run = { state: 'failed', error: clip(error) };
      events.push({ key: null, node_id: null, attempt: 0, type: 'run.finished', data: { state: 'failed', error: clip(error), ...(bad?.cap ? { cap: bad.cap } : {}) } });
      break;
    }
    case 'run.cancel':
      cancelLive();
      run = { state: 'cancelled' };
      events.push({ key: null, node_id: null, attempt: 0, type: 'run.finished', data: { state: 'cancelled' } });
      break;
    case 'run.done':
      run = { state: 'done' };
      events.push({ key: null, node_id: null, attempt: 0, type: 'run.finished', data: { state: 'done' } });
      break;
    case 'run.retry': {
      // Failed, interrupted and cancelled nodes go back to pending with their attempts cleared. A container keeps its progress:
      // the loop iteration or map expansion it reached is still there.
      let reset = 0;
      for (const r of [...m.rows.values()].sort((a, b) => (a.key < b.key ? -1 : 1))) {
        if (r.state !== 'failed' && r.state !== 'interrupted' && r.state !== 'cancelled') continue;
        const next: Row = { key: r.key, node_id: r.node_id, state: 'pending', attempt: 0, ready: false, progress: r.progress, turns: 0 };
        rows.push(next);
        ev(next, 'node.reset');
        reset++;
      }
      run = { state: 'running', error: null };
      events.push({ key: null, node_id: null, attempt: 0, type: 'run.retried', data: { reset } });
      break;
    }
  }

  // The run row follows its nodes, in the same commit.
  if (!run?.state) {
    const merged = new Map(m.rows);
    for (const r of rows) merged.set(r.key, r);
    const state = deriveRunState(merged.values());
    if (state !== m.state) run = { ...run, state };
  }
  return { change: { rows, events, ...(run ? { run } : {}) }, cancelled };
}

/** The agent node a key names, for the effects that need its definition. */
export function agentNode(m: Model, key: string): AgentNode | undefined {
  const n = m.ix.byId.get(m.rows.get(key)?.node_id ?? '')?.node;
  return n?.kind === 'agent' ? n : undefined;
}
