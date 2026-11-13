// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The engine: the effects around the pure reducer (interp.ts). It keeps each
// active run's model in memory, commits one transition at a time through the
// store, and starts the effects the reducer asks for. It never holds a run's
// position anywhere a crash would lose it: whatever is committed is the whole
// state, and `recover()` runs the same reducer over it.

import { randomBytes } from 'node:crypto';
import { validate } from '../ir/validate.ts';
import { shape } from '../ir/shape.ts';
import { checkValue } from '../ir/resultschema.ts';
import type { IR, Node } from '../sdk/dsl.ts';
import type { EventDraft, RunEvent } from './events.ts';
import { initialRows, apply, type Action, type Applied, type Model, modelFor, step, type Transition } from './interp.ts';
import { killTree, reap } from './killtree.ts';
import type { Row, RunRow } from './model.ts';
import { encodedSize } from './size.ts';
import type { Store } from './store.ts';

/** `input` of a run, as sent. */
export const MAX_INPUT_BYTES = 8 << 10;

/** The slice of the SDK connection the engine uses: the fake in the tests implements it too. */
export interface Hive {
  // biome-ignore lint/suspicious/noExplicitAny: wire replies are decoded JSON
  request(name: string, payload?: object, opts?: { timeoutMs?: number }): Promise<any>;
  postMessage(topic: string, data?: unknown): void;
}

export interface NodeContext {
  engine: Engine;
  /** Which in-memory run this launch belongs to: a retry makes a new one, so a stale executor can tell (`engine.current`). */
  epoch: number;
  run_id: string;
  key: string;
  node: Node;
}

/** What starts a node. A check is real in B2; agents arrive in B3, and tests inject their own. */
export interface Executors {
  check(ctx: NodeContext): Promise<void>;
  agent(ctx: NodeContext): Promise<void>;
}

export interface EngineOptions {
  store: Store;
  hive: Hive;
  executors: Executors;
  now?: () => Date;
  /** Called with every batch of events as stored, for the live push. */
  onEvents?: (runId: string, events: RunEvent[]) => void;
}

interface Runtime {
  id: string;
  epoch: number;
  project_id: string;
  model: Model;
  inflight: Set<string>;
  stops: Map<string, () => Promise<void>>;
  driving: boolean;
  again: boolean;
}

export class EngineError extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

const B32 = 'abcdefghijklmnopqrstuvwxyz234567';
/** `^[a-z0-9][a-z0-9-]{0,62}$`, as A requires of a workflow run id. */
function newRunId(): string {
  const b = randomBytes(12);
  return `r${Array.from(b, (x) => B32[x & 31]).join('')}`;
}

export class Engine {
  readonly store: Store;
  readonly hive: Hive;
  readonly #executors: Executors;
  readonly #now: () => Date;
  readonly #onEvents: ((runId: string, events: RunEvent[]) => void) | undefined;
  readonly #runs = new Map<string, Runtime>();
  #flusher: ReturnType<typeof setInterval> | undefined;
  #stopping = false;
  #epochs = 0;

  constructor(o: EngineOptions) {
    this.store = o.store;
    this.hive = o.hive;
    this.#executors = o.executors;
    this.#now = o.now ?? (() => new Date());
    this.#onEvents = o.onEvents;
  }

  // ----------------------------------------------------------------- runs

  /** The test seam B2 starts runs through; `run.start` arrives in B4 with the build step. Validates shape and semantics, and the input. */
  startRunFromIR(a: { project_id: string; file: string; ir: unknown; input: Record<string, unknown>; source_hash?: string }): string {
    const bad = shape(a.ir);
    if (bad.length > 0) throw new EngineError('invalid_ir', bad.join('; '));
    const ir = a.ir as IR;
    const semantic = validate(ir);
    if (semantic.length > 0) throw new EngineError('invalid_ir', semantic.join('; '));
    const why = checkValue(ir.input, a.input, 'input');
    if (why) throw new EngineError('invalid_input', why);
    if (encodedSize(a.input) > MAX_INPUT_BYTES) throw new EngineError('invalid_input', `input is over ${MAX_INPUT_BYTES} bytes as sent`);

    const run_id = newRunId();
    const rows = initialRows(ir);
    const first: EventDraft = {
      key: null,
      node_id: null,
      attempt: 0,
      type: 'run.started',
      data: { workflow: ir.name, file: a.file, project_id: a.project_id, source_hash: a.source_hash ?? '', base_sha: '', input: a.input },
    };
    this.store.createRun(
      { run_id, project_id: a.project_id, workflow: ir.name, file: a.file, source_hash: a.source_hash ?? '', ir: JSON.stringify(ir), input: JSON.stringify(a.input) },
      rows,
      [first],
    );
    this.#publish(run_id, this.store.events(run_id, 0, 10));
    const rt = this.#load(this.store.getRun(run_id) as RunRow);
    this.drive(rt);
    return run_id;
  }

  /** Why a stored run cannot go on, or undefined. The IR is checked again before every resume and retry (spec 529, criterion 2): the engine that wrote it may not be the one reading it. */
  #stale(run: RunRow): string | undefined {
    const bad = shape(JSON.parse(run.ir));
    if (bad.length === 0) bad.push(...validate(JSON.parse(run.ir) as IR));
    return bad.length === 0 ? undefined : `the stored workflow no longer validates: ${bad.slice(0, 3).join('; ')}`;
  }

  /** Fails a run that cannot be resumed, in one commit, so it is not retried on every start. */
  #refuse(run: RunRow, why: string): void {
    const error = why.slice(0, 1024);
    this.#publish(run.run_id, this.store.commit(run.run_id, { rows: [], events: [{ key: null, node_id: null, attempt: 0, type: 'run.finished', data: { state: 'failed', error } }], run: { state: 'failed', error } }));
  }

  #load(run: RunRow): Runtime {
    const model = modelFor(JSON.parse(run.ir) as IR, JSON.parse(run.input), this.store.loadRows(run.run_id), run.state);
    const rt: Runtime = { id: run.run_id, epoch: ++this.#epochs, project_id: run.project_id, model, inflight: new Set(), stops: new Map(), driving: false, again: false };
    // A finished run is not kept: it is only read, or retried, and a retry reloads it.
    if (run.state === 'running' || run.state === 'waiting') this.#runs.set(run.run_id, rt);
    return rt;
  }

  #rt(runId: string): Runtime {
    const rt = this.#runs.get(runId);
    if (rt) return rt;
    const run = this.store.getRun(runId);
    if (!run) throw new EngineError('no_such_run', `no run ${runId}`);
    return this.#load(run);
  }

  /** Reads a run's current state through the in-memory model, loading it when the run is not active. */
  run(runId: string): { model: Model; project_id: string } {
    const rt = this.#rt(runId);
    return { model: rt.model, project_id: rt.project_id };
  }

  // ---------------------------------------------------------------- commits

  /** Commits one transition: store first, then the model, then the push and the stops. Throws, leaving the model alone, when the store does. */
  commit(runId: string, t: Transition): Applied {
    const rt = this.#rt(runId);
    const a = apply(rt.model, t, { now: () => this.#now().toISOString(), handles: this.store.handlesFor(runId) });
    const events = this.store.commit(runId, a.change);
    for (const r of a.change.rows) rt.model.rows.set(r.key, r);
    if (a.change.run?.state) rt.model.state = a.change.run.state;
    this.#publish(runId, events);
    for (const r of a.cancelled) {
      const stop = rt.stops.get(r.key);
      if (stop) void stop();
    }
    if (rt.model.state === 'done' || rt.model.state === 'failed' || rt.model.state === 'cancelled') this.#runs.delete(runId);
    return a;
  }

  #publish(runId: string, events: RunEvent[]): void {
    if (events.length > 0) this.#onEvents?.(runId, events);
  }

  /** Writes the buffered check.output and agent.* events. Runs every 250 ms once `startFlush` is called. */
  flush(): void {
    for (const ev of this.store.flush()) this.#publish(ev.run_id, [ev]);
  }

  startFlush(ms = 250): void {
    this.#flusher ??= setInterval(() => this.flush(), ms);
    this.#flusher.unref?.();
  }

  stopFlush(): void {
    if (this.#flusher) clearInterval(this.#flusher);
    this.#flusher = undefined;
  }

  /** Whether `ctx` still belongs to the run's live runtime (not ended, not replaced by a retry). */
  current(ctx: { run_id: string; epoch: number }): boolean {
    return this.#runs.get(ctx.run_id)?.epoch === ctx.epoch;
  }

  /** Buffers a check.output or agent.* event. Output for a node that is no longer running is dropped: nothing follows `node.finished`. */
  buffer(runId: string, draft: EventDraft): void {
    const s = draft.key === null ? undefined : this.#row(runId, draft.key)?.state;
    if (s === 'running' || s === 'waiting') this.store.buffer(runId, draft);
  }

  /** Pins the run's base commit (spec 529, R4), at once, so a restart cannot split a run across two bases. */
  pinBase(runId: string, sha: string): void {
    if (sha && !this.store.getRun(runId)?.base_sha) this.store.commit(runId, { rows: [], events: [], run: { base_sha: sha } });
  }

  // -------------------------------------------------------------- the loop

  /** Runs the reducer to a fixed point: commit every due transition, start every due effect once. */
  drive(rt: Runtime): void {
    if (rt.driving) {
      rt.again = true;
      return;
    }
    rt.driving = true;
    try {
      do {
        rt.again = false;
        for (;;) {
          const due = step(rt.model, rt.inflight);
          if (due.transitions.length > 0) {
            for (const t of due.transitions) {
              if (rt.model.state === 'done' || rt.model.state === 'failed' || rt.model.state === 'cancelled') break;
              this.commit(rt.id, t);
            }
            continue; // effects start only from a pass with nothing left to commit: a run that is failing launches nothing
          }
          for (const a of due.actions) this.#launch(rt, a);
          break;
        }
      } while (rt.again);
    } finally {
      rt.driving = false;
    }
  }

  #launch(rt: Runtime, a: Action): void {
    const node = rt.model.ix.byId.get(a.node_id)?.node as Node;
    rt.inflight.add(a.key);
    const ctx: NodeContext = { engine: this, epoch: rt.epoch, run_id: rt.id, key: a.key, node };
    const fail = (err: unknown): void => {
      if (this.#runs.get(rt.id) !== rt) return; // the run ended or was retried since: this failure is not the new attempt's
      this.failNode(rt.id, a.key, `${a.a} could not run: ${err instanceof Error ? err.message : String(err)}`);
    };
    let p: Promise<void>;
    try {
      p = this.#executors[a.a](ctx);
    } catch (err) {
      p = Promise.reject(err);
    }
    p.catch(fail).finally(() => {
      rt.inflight.delete(a.key);
      // An executor that returns without starting or failing its node would be launched again at once, forever.
      const s = rt.model.rows.get(a.key)?.state;
      const live = this.#runs.get(rt.id) === rt;
      if (live && (s === 'pending' || s === 'interrupted')) this.failNode(rt.id, a.key, `${a.a} executor returned without starting the node`);
      if (live) this.drive(rt);
    });
  }

  // ----------------------------------------- what executors call (B2 + B3)

  #row(runId: string, key: string): Row | undefined {
    return this.#runs.get(runId)?.model.rows.get(key);
  }

  /** The node is starting: its process or session exists (`patch` records it). False when the node is no longer startable, say after a cancel. */
  beginNode(runId: string, key: string, patch?: Partial<Row>): boolean {
    const r = this.#row(runId, key);
    if (this.#stopping || !r || (r.state !== 'pending' && r.state !== 'interrupted')) return false;
    this.commit(runId, { t: 'start', key, ...(patch ? { patch } : {}) });
    return true;
  }

  completeNode(runId: string, key: string, output: unknown): boolean {
    return this.#end(runId, key, 'running', { t: 'finish', key, output });
  }

  failNode(runId: string, key: string, error: string, extra: { cap?: string; conflict?: Row['conflict'] } = {}): boolean {
    const r = this.#row(runId, key);
    if (this.#stopping || !r || r.state === 'done' || r.state === 'failed' || r.state === 'cancelled' || r.state === 'skipped') return false;
    this.commit(runId, { t: 'fail', key, error, ...extra });
    this.#after(runId);
    return true;
  }

  /** Reserves an agent turn against the run's cap; false when the cap failed the node. */
  reserveTurn(runId: string, key: string): boolean {
    this.commit(runId, { t: 'turn', key });
    this.#after(runId);
    return this.#row(runId, key)?.state !== 'failed';
  }

  #end(runId: string, key: string, from: Row['state'], t: Transition): boolean {
    if (this.#stopping || this.#row(runId, key)?.state !== from) return false;
    this.commit(runId, t);
    this.#after(runId);
    return true;
  }

  #after(runId: string): void {
    const rt = this.#runs.get(runId);
    if (rt) this.drive(rt);
  }

  /** Registers how to stop a node's process, for a cancel or a run failure. */
  onStop(runId: string, key: string, stop: (() => Promise<void>) | undefined): void {
    const rt = this.#runs.get(runId);
    if (!rt) return;
    if (stop) rt.stops.set(key, stop);
    else rt.stops.delete(key);
  }

  // ------------------------------------------------------- what topics call

  answerHuman(runId: string, key: string, answer: unknown): void {
    const rt = this.#rt(runId);
    const r = rt.model.rows.get(key);
    const n = r && rt.model.ix.byId.get(r.node_id)?.node;
    if (!r || n?.kind !== 'human' || r.state !== 'waiting') throw new EngineError('not_waiting', `${key} is not waiting for an answer`);
    const why = checkValue(n.output, answer, 'answer');
    if (why) throw new EngineError('invalid_answer', why);
    this.commit(runId, { t: 'finish', key, output: answer });
    this.#after(runId);
  }

  cancelRun(runId: string): void {
    const rt = this.#rt(runId);
    if (rt.model.state !== 'running' && rt.model.state !== 'waiting') throw new EngineError('not_running', `run ${runId} is ${rt.model.state}`);
    this.commit(runId, { t: 'run.cancel' });
  }

  retryRun(runId: string): void {
    const rt = this.#rt(runId);
    if (rt.model.state !== 'failed' && rt.model.state !== 'cancelled') throw new EngineError('not_retryable', `run ${runId} is ${rt.model.state}; only a failed or cancelled run can be retried`);
    const why = this.#stale(this.store.getRun(runId) as RunRow);
    if (why) throw new EngineError('invalid_ir', why);
    this.commit(runId, { t: 'run.retry' });
    this.drive(this.#rt(runId));
  }

  /**
   * A clean stop: TERM every running check's group, briefly, so a normal
   * `hived` stop leaves no `go test` behind. The rows stay `running`; the next
   * start reaps and retries them like any other interrupted check.
   */
  async shutdown(graceMs = 1_000): Promise<void> {
    // A check ended by this stop is interrupted, not finished: nothing it reports from here on is recorded.
    this.#stopping = true;
    const stops: Promise<void>[] = [];
    for (const rt of this.#runs.values()) {
      for (const r of rt.model.rows.values()) {
        if (r.state === 'running' && r.pid !== undefined) stops.push(killTree({ pid: r.pid, pgid: r.pgid }, { graceMs }));
      }
    }
    await Promise.all(stops);
    this.stopFlush();
    this.flush();
  }

  // --------------------------------------------------------------- startup

  /**
   * Picks up every run that was active when the engine last stopped. A check
   * that was running is gone with the plugin (or about to be): its recorded
   * group is reaped when the pid still is that process, and the node becomes
   * `interrupted`, which the reducer retries. A waiting human is untouched.
   */
  async recover(): Promise<void> {
    const loaded: Runtime[] = [];
    for (const run of this.store.activeRuns()) {
      const why = this.#stale(run);
      if (why) this.#refuse(run, why);
      else loaded.push(this.#load(run));
    }
    for (const rt of loaded) {
      for (const r of [...rt.model.rows.values()]) {
        if (r.state !== 'running') continue;
        const kind = rt.model.ix.byId.get(r.node_id)?.node.kind;
        if (kind !== 'check' && kind !== 'agent') continue;
        if (r.pid !== undefined) await reap({ pid: r.pid, pgid: r.pgid, proc_start: r.proc_start });
        this.commit(rt.id, { t: 'interrupt', key: r.key });
      }
    }
    for (const rt of loaded) this.drive(rt);
  }
}
