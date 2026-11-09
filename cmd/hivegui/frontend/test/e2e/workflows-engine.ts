// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// A scripted fake of the `workflows` plugin (spec 530), behind the mock
// bridge's SendPluginMessage. It speaks the contract in exec plan 529
// ("Contract for C"): nonce replies on `<topic>.reply`, `run.get {run, ir}`
// with no nodes, materialized `run.nodes` pages each carrying their own
// `last_seq`, `node.get`, lazy `run.events`, `runs.list`, and pushed
// `run.event` batches.
//
// It keeps a GROUND-TRUTH `nodes` table beside the event log, updated by a
// reducer that is deliberately independent of the GUI's (lib/workflow-
// overlay.ts), so a test can compare the GUI's final graph with it.

import type { IR, JsonSchema } from '../../src/lib/workflow-types.js';

type Json = Record<string, unknown>;

export interface MockNode {
  key: string;
  node_id: string;
  state: string;
  attempt: number;
  cap?: string;
}

export interface MockEvent {
  run_id: string;
  seq: number;
  ts: string;
  key: string | null;
  node_id: string | null;
  attempt: number;
  type: string;
  data: Json;
}

export interface MockRunSpec {
  run_id: string;
  project_id?: string;
  workflow: string;
  file?: string;
  state?: string;
  created_at?: string;
  source_changed?: boolean;
  ir: IR;
  /** Initial rows, as if events up to now had committed. */
  nodes?: MockNode[];
  /** node.get output text by key. */
  outputs?: Record<string, string>;
  /** Events already committed (no push), so the log, seq and rows agree. */
  events?: Partial<MockEvent>[];
}

export interface MockBuild {
  name: string;
  ir: IR;
  input_schema: JsonSchema;
  source_hash: string;
}

export interface MockWorkflowsSpec {
  folder?: string;
  trust?: 'none' | 'trusted' | 'changed' | 'invalid';
  invalid_reason?: string;
  files?: { path: string; bytes: number }[];
  workflows?: { file: string; name?: string }[];
  /** A build per file, or an error for it. */
  builds?: Record<string, MockBuild | { error: Json }>;
  /** Trust describe fails with this code (symlink, too_large, missing). */
  describeError?: string;
  runs?: MockRunSpec[];
  /** Rows per run.nodes page (the real cap is bytes). Default 100. */
  pageRows?: number;
  /** node.get output slice length in characters. Default 16384. */
  sliceChars?: number;
  /** Starting a run uses this run (cloned with a fresh id). */
  startedRun?: MockRunSpec;
}

interface Run extends Required<Pick<MockRunSpec, 'run_id' | 'workflow' | 'ir'>> {
  spec: MockRunSpec;
  state: string;
  nodes: Map<string, MockNode>;
  log: MockEvent[];
  seq: number;
}

export type Send = (topic: string, data: unknown) => void;

const sleep0 = () => new Promise<void>((r) => setTimeout(r, 0));

export class WorkflowsEngine {
  spec: MockWorkflowsSpec = {};
  runs = new Map<string, Run>();
  trust: string = 'none';
  hash = 'a1b2c3d4e5f6a7b8c9d0e1f2';
  /** Every request seen, for assertions: topic and request data. */
  requests: { topic: string; data: Json }[] = [];
  private held: { topic: string; data: Json }[] = [];
  private holdNodesAt = 0;
  private nodesCalls = 0;
  private started = 0;

  constructor(private send: Send) {}

  seed(spec: MockWorkflowsSpec): void {
    this.spec = spec;
    this.trust = spec.trust ?? 'trusted';
    this.runs.clear();
    this.requests = [];
    this.held = [];
    this.holdNodesAt = 0;
    this.nodesCalls = 0;
    for (const r of spec.runs ?? []) this.addRun(r);
  }

  private addRun(r: MockRunSpec): Run {
    const run: Run = {
      run_id: r.run_id,
      workflow: r.workflow,
      ir: r.ir,
      spec: r,
      state: r.state ?? 'running',
      nodes: new Map((r.nodes ?? []).map((n) => [n.key, { ...n }])),
      log: [],
      seq: 0,
    };
    this.runs.set(r.run_id, run);
    for (const e of r.events ?? []) this.commit(run, e);
    return run;
  }

  /** Holds the processing of the nth `run.nodes` request of the next load
   * (1-based) until releaseHeld(), so a test can push events between
   * pages. */
  holdNodesPage(n: number): void {
    this.holdNodesAt = n;
    this.nodesCalls = 0;
  }
  heldCount(): number {
    return this.held.length;
  }
  releaseHeld(): void {
    const h = this.held;
    this.held = [];
    for (const m of h) void this.process(m.topic, m.data);
  }

  truth(runId: string): MockNode[] {
    const run = this.runs.get(runId);
    return run ? [...run.nodes.values()].sort(byKey) : [];
  }

  /** Commits events to a run: assigns seq, updates the truth table and
   * pushes one `run.event` batch. */
  push(runId: string, events: Partial<MockEvent>[]): MockEvent[] {
    const run = this.runs.get(runId);
    if (!run) throw new Error(`no mock run ${runId}`);
    const out = events.map((e) => this.commit(run, e));
    this.send('run.event', { run_id: runId, events: out });
    return out;
  }

  private commit(run: Run, e: Partial<MockEvent>): MockEvent {
    run.seq += 1;
    const ev: MockEvent = {
      run_id: run.run_id,
      seq: run.seq,
      ts: new Date().toISOString(),
      key: null,
      node_id: null,
      attempt: 0,
      type: 'node.ready',
      data: {},
      ...e,
    };
    this.reduce(run, ev);
    run.log.push(ev);
    return ev;
  }

  private reduce(run: Run, ev: MockEvent): void {
    const k = ev.key;
    const row = (): MockNode => {
      let r = run.nodes.get(k as string);
      if (!r) {
        r = {
          key: k as string,
          node_id: ev.node_id ?? (k as string).replace(/\[\d+\]/g, ''),
          state: 'pending',
          attempt: 0,
        };
        run.nodes.set(r.key, r);
      }
      return r;
    };
    switch (ev.type) {
      case 'run.started':
      case 'run.retried':
        run.state = 'running';
        break;
      case 'run.finished':
        run.state = String(ev.data.state ?? 'done');
        break;
      case 'node.ready':
        if (k) row();
        break;
      case 'node.started':
        if (k) Object.assign(row(), { state: 'running', attempt: ev.attempt });
        break;
      case 'node.waiting':
        if (k) Object.assign(row(), { state: 'waiting', attempt: ev.attempt || row().attempt });
        break;
      case 'node.resumed':
        if (k) row().state = 'running';
        break;
      case 'node.finished':
        if (k)
          Object.assign(row(), {
            state: String(ev.data.state ?? 'done'),
            attempt: ev.attempt || row().attempt,
          });
        break;
      case 'node.reset':
        if (k) Object.assign(row(), { state: 'pending', attempt: 0 });
        break;
      default:
    }
  }

  handle(topic: string, data: Json): void {
    this.requests.push({ topic, data });
    if (topic === 'run.nodes') {
      this.nodesCalls += 1;
      if (this.holdNodesAt && this.nodesCalls === this.holdNodesAt) {
        this.held.push({ topic, data });
        return;
      }
    }
    void this.process(topic, data);
  }

  private reply(topic: string, nonce: unknown, body: Json): void {
    this.send(`${topic}.reply`, { nonce, ok: true, ...body });
  }
  private fail(
    topic: string,
    nonce: unknown,
    code: string,
    message: string,
    extra: Json = {},
  ): void {
    this.send(`${topic}.reply`, {
      nonce,
      ok: false,
      error: { code, message, ...extra },
    });
  }

  private async process(topic: string, data: Json): Promise<void> {
    await sleep0();
    const n = data.nonce;
    const runOf = () => this.runs.get(String(data.run_id));
    switch (topic) {
      case 'workflows.list':
        return this.reply(topic, n, {
          folder: this.spec.folder ?? '/work/p1/.hive/workflows',
          trust: this.trust,
          ...(this.spec.invalid_reason
            ? { invalid_reason: this.spec.invalid_reason }
            : {}),
          workflows: this.spec.workflows ?? [],
        });
      case 'workflow.trust.describe':
        if (this.spec.describeError)
          return this.fail(topic, n, this.spec.describeError, 'refused');
        return this.reply(topic, n, {
          folder: this.spec.folder ?? '/work/p1/.hive/workflows',
          hash: this.hash,
          files: this.spec.files ?? [],
        });
      case 'workflow.trust.confirm':
        if (data.hash !== this.hash)
          return this.fail(topic, n, 'changed', 'changed');
        this.trust = 'trusted';
        return this.reply(topic, n, {});
      case 'workflow.build': {
        if (this.trust !== 'trusted')
          return this.fail(topic, n, 'untrusted', 'not trusted');
        const b = this.spec.builds?.[String(data.file)];
        if (!b) return this.fail(topic, n, 'build_failed', 'no such file', { errors: ['no such file'] });
        if ('error' in b) {
          const { code, message, ...extra } = b.error as Json;
          return this.fail(topic, n, String(code), String(message ?? code), extra);
        }
        return this.reply(topic, n, { ...b });
      }
      case 'run.start': {
        const tpl = this.spec.startedRun;
        if (!tpl) return this.fail(topic, n, 'build_failed', 'no run template');
        this.started += 1;
        const run_id = `${tpl.run_id}-${this.started}`;
        this.addRun({ ...tpl, run_id });
        return this.reply(topic, n, { run_id });
      }
      case 'runs.list': {
        const all = [...this.runs.values()]
          .filter((r) => !data.project_id || (r.spec.project_id ?? 'p1') === data.project_id)
          .filter((r) => !data.state || this.displayState(r) === data.state)
          .map((r) => this.summary(r));
        return this.reply(topic, n, { runs: all.slice(0, Number(data.limit ?? 50)) });
      }
      case 'run.get': {
        const r = runOf();
        if (!r) return this.fail(topic, n, 'not_found', 'no such run');
        return this.reply(topic, n, { run: this.detail(r), ir: r.ir });
      }
      case 'run.nodes': {
        const r = runOf();
        if (!r) return this.fail(topic, n, 'not_found', 'no such run');
        const rows = [...r.nodes.values()]
          .sort(byKey)
          .filter((x) => !data.after || byKey(x, { key: String(data.after) } as MockNode) > 0);
        const per = this.spec.pageRows ?? 100;
        const page = rows.slice(0, per);
        const more = rows.length > per;
        return this.reply(topic, n, {
          nodes: page,
          last_seq: r.seq,
          ...(more ? { more: true, next_after: page[page.length - 1].key } : {}),
        });
      }
      case 'node.get': {
        const r = runOf();
        const key = String(data.key);
        const row = r?.nodes.get(key);
        if (!r || !row) return this.fail(topic, n, 'not_found', 'no such node');
        const facts = this.facts(r, key);
        const full = r.spec.outputs?.[key] ?? '';
        const off = Number(data.offset ?? 0);
        const len = this.spec.sliceChars ?? 16384;
        const out = full.slice(off, off + len);
        const end = off + out.length;
        return this.reply(topic, n, {
          key,
          node_id: row.node_id,
          state: row.state,
          attempt: row.attempt,
          ...(full ? { output: out, total_bytes: full.length, next_offset: end, more: end < full.length } : {}),
          ...facts,
        });
      }
      case 'run.events': {
        const r = runOf();
        if (!r) return this.fail(topic, n, 'not_found', 'no such run');
        const after = Number(data.after_seq ?? 0);
        const limit = Number(data.limit ?? 500);
        const evs = r.log.filter((e) => e.seq > after);
        const page = evs.slice(0, limit);
        return this.reply(topic, n, {
          events: page,
          last_seq: page.length ? page[page.length - 1].seq : after,
          more: evs.length > limit,
        });
      }
      case 'human.answer': {
        const r = runOf();
        const key = String(data.key);
        if (!r || r.nodes.get(key)?.state !== 'waiting')
          return this.fail(topic, n, 'not_waiting', 'not waiting');
        const ans = data.answer as Json | undefined;
        if (!ans || typeof ans !== 'object' || !('verdict' in ans))
          return this.fail(topic, n, 'invalid_answer', 'answer.verdict: required');
        this.reply(topic, n, {});
        this.push(r.run_id, [
          { type: 'node.output', key, node_id: r.nodes.get(key)?.node_id ?? null, data: { output: ans } },
          { type: 'node.finished', key, node_id: r.nodes.get(key)?.node_id ?? null, attempt: r.nodes.get(key)?.attempt ?? 1, data: { state: 'done' } },
          { type: 'run.finished', data: { state: 'done' } },
        ]);
        return;
      }
      case 'run.cancel': {
        const r = runOf();
        if (!r) return this.fail(topic, n, 'not_found', 'no such run');
        this.reply(topic, n, {});
        this.push(r.run_id, [{ type: 'run.finished', data: { state: 'cancelled' } }]);
        return;
      }
      case 'run.retry': {
        const r = runOf();
        if (!r) return this.fail(topic, n, 'not_found', 'no such run');
        this.reply(topic, n, {});
        const failed = [...r.nodes.values()].filter((x) => x.state === 'failed' || x.state === 'interrupted');
        this.push(r.run_id, [
          ...failed.map((x) => ({ type: 'node.reset', key: x.key, node_id: x.node_id })),
          { type: 'run.retried', data: { reset: failed.length } },
        ]);
        return;
      }
      default:
        return this.fail(topic, n, 'unknown_topic', `unknown topic ${topic}`);
    }
  }

  private displayState(r: Run): string {
    if (r.state === 'running' && [...r.nodes.values()].some((x) => x.state === 'waiting'))
      return 'waiting';
    return r.state;
  }

  private summary(r: Run): Json {
    return {
      run_id: r.run_id,
      project_id: r.spec.project_id ?? 'p1',
      workflow: r.workflow,
      file: r.spec.file ?? `${r.workflow}.ts`,
      state: this.displayState(r),
      created_at: r.spec.created_at ?? new Date(Date.now() - 120_000).toISOString(),
      updated_at: new Date().toISOString(),
      source_hash: 'h',
      source_changed: !!r.spec.source_changed,
    };
  }

  private detail(r: Run): Json {
    return { ...this.summary(r), input: {}, base_sha: 'abc123' };
  }

  /** B-13: what node.get adds, read off the event log. */
  private facts(r: Run, key: string): Json {
    const out: Json = {};
    let waiting: Json | undefined;
    for (const e of r.log) {
      if (e.key !== key) continue;
      if (e.type === 'node.started')
        for (const f of ['session_id', 'branch', 'worktree'])
          if (e.data[f] !== undefined) out[f] = e.data[f];
      if (e.type === 'node.waiting') waiting = e.data;
      if (e.type === 'node.resumed' || e.type === 'node.finished' || e.type === 'node.reset')
        waiting = undefined;
      if (e.type === 'node.finished') {
        if (e.data.error !== undefined) out.error = e.data.error;
        if (e.data.conflict !== undefined) out.conflict = e.data.conflict;
      }
    }
    if (waiting && r.nodes.get(key)?.state === 'waiting') out.waiting = waiting;
    return out;
  }
}

const byKey = (a: { key: string }, b: { key: string }): number =>
  a.key.localeCompare(b.key, undefined, { numeric: true });
