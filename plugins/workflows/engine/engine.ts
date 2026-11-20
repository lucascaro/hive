// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The engine: the effects around the pure reducer (interp.ts). It keeps each
// active run's model in memory, commits one transition at a time through the
// store, and starts the effects the reducer asks for. It never holds a run's
// position anywhere a crash would lose it: whatever is committed is the whole
// state, and `recover()` runs the same reducer over it.

import { randomBytes } from 'node:crypto';
import { type AcpAgentInfo, agentNodes, catalog } from '../ir/catalog.ts';
import { validate } from '../ir/validate.ts';
import { shape } from '../ir/shape.ts';
import { checkValue } from '../ir/resultschema.ts';
import type { IR, Node } from '../sdk/dsl.ts';
import type { EventDraft, RunEvent } from './events.ts';
import { initialRows, apply, type Action, type Applied, type Model, modelFor, step, type Transition } from './interp.ts';
import { type KillDeps, killTree, reap, unsafeTarget } from './killtree.ts';
import { redactCut } from './redact.ts';
import type { Row, RunRow } from './model.ts';
import { recoverAgents } from './durability.ts';
import { SessionFeed } from './sessions.ts';
import { encodedSize } from './size.ts';
import type { Store } from './store.ts';

/** `input` of a run, as sent. */
export const MAX_INPUT_BYTES = 8 << 10;

/** The slice of the SDK connection the engine uses: the fake in the tests implements it too. */
export interface Hive {
  // biome-ignore lint/suspicious/noExplicitAny: wire replies are decoded JSON
  request(name: string, payload?: object, opts?: { timeoutMs?: number }): Promise<any>;
  postMessage(topic: string, data?: unknown): void;
  /** Frames the daemon sends unasked (SESSION_EVENT, ACP_TRANSCRIPT, ERROR): the SDK connection is an EventEmitter. */
  on?(event: string, fn: (msg: never) => void): unknown;
  /** Sends a frame that has no req_id reply (LIST_SESSIONS, GET_ACP_TRANSCRIPT): its answer arrives as an event. */
  send?(name: string, payload?: object): void;
  /** The daemon's WELCOME: `boot_id` changes when `hived` restarts (R2). */
  readonly welcome?: { boot_id?: string };
}

/** What an agent launch is picking up instead of starting from nothing (B3c): a node whose turn survived a plugin crash or a reconnect. */
export type AgentPlan =
  /** The row is running and its turn was started: watch it, from the ack the daemon gave (or the transcript showed). */
  | { kind: 'watch'; ack: { prompt_id: number; epoch: number }; again: boolean }
  /** The row is running with a ready session and no turn of ours in its transcript: send the turn now. */
  | { kind: 'send' };

export interface NodeContext {
  engine: Engine;
  /** Which in-memory run this launch belongs to: a retry makes a new one, so a stale executor can tell (`engine.current`). */
  epoch: number;
  run_id: string;
  key: string;
  node: Node;
  /** Set only when an agent node is picked up after a crash or reconnect. */
  plan?: AgentPlan;
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
  /** How processes are signalled at startup and shutdown: a seam so a test can make the OS refuse (EPERM) without signalling anything. */
  kill?: KillDeps;
  /** This plugin's id, as the daemon labels the sessions it creates (`plugin:<id>/workflow:<run>`). Defaults to HIVE_PLUGIN_ID. */
  pluginId?: string;
  /** How long recovery waits for the daemon to list its sessions or send a transcript before it assumes nothing survived. A test shortens it. */
  daemonWaitMs?: number;
  /** Called with every batch of events as stored, for the live push. */
  onEvents?: (runId: string, events: RunEvent[]) => void;
}

interface Runtime {
  id: string;
  epoch: number;
  project_id: string;
  model: Model;
  inflight: Set<string>;
  stops: Map<string, () => Promise<'kept' | void>>;
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

/** How long a retry waits for the launch of the same node before it. Longer than any request a launch makes. */
const LAUNCH_WAIT_MS = 130_000;

/** Resolves when `p` settles (either way) or after `ms`. */
function waitFor(p: Promise<unknown>, ms: number): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    timer.unref?.();
    const done = (): void => {
      clearTimeout(timer);
      resolve();
    };
    p.then(done, done);
  });
}

const B32 = 'abcdefghijklmnopqrstuvwxyz234567';
/** `^[a-z0-9][a-z0-9-]{0,62}$`, as A requires of a workflow run id. */
function newRunId(): string {
  const b = randomBytes(12);
  return `r${Array.from(b, (x) => B32[x & 31]).join('')}`;
}

export class Engine {
  readonly store: Store;
  #hive: Hive;
  /** The latest the daemon said about each session; agent executors wait on it. */
  readonly feed = new SessionFeed();
  readonly #executors: Executors;
  readonly #now: () => Date;
  readonly #onEvents: ((runId: string, events: RunEvent[]) => void) | undefined;
  readonly #runs = new Map<string, Runtime>();
  #flusher: ReturnType<typeof setInterval> | undefined;
  #stopping = false;
  readonly #killDeps: KillDeps;
  readonly #pluginId: string;
  /** See `EngineOptions.daemonWaitMs`. */
  readonly daemonWaitMs: number;
  #epochs = 0;
  /** Counts lost connections: an agent launch made under an earlier one is stale and must not fail or kill anything (B3c). */
  #link = 0;
  /** No effects are started while the engine recovers or has no connection: a launch before the orphan scan could create a second session. */
  #holds = 0;
  /** Whether the current connection is open. A lost one stays lost until `reconnect` is handed a new one that survives its own read. */
  #alive = true;
  /** Recovery and reconnect never overlap: a reconnect queued behind the startup recovery waits for it. */
  #chain: Promise<unknown> = Promise.resolve();
  /** Sessions found by recovery that a pending node should take over instead of creating one, by `run/key`. */
  readonly #orphans = new Map<string, string>();
  /** The latest agent launch of each node: a retry waits for the one before it, so a stale launch's kill cannot land after the retry re-attached its worktree. */
  readonly #tails = new Map<string, Promise<unknown>>();

  constructor(o: EngineOptions) {
    this.store = o.store;
    this.#hive = o.hive;
    this.#executors = o.executors;
    this.#now = o.now ?? (() => new Date());
    this.#onEvents = o.onEvents;
    this.#killDeps = o.kill ?? {};
    this.#pluginId = o.pluginId ?? process.env.HIVE_PLUGIN_ID ?? 'workflows';
    this.daemonWaitMs = o.daemonWaitMs ?? 15_000;
    this.feed.attach(o.hive);
  }

  get hive(): Hive {
    return this.#hive;
  }

  /** Bumped on every lost connection. A launch compares it with the value it started under. */
  get link(): number {
    return this.#link;
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
    const error = redactCut(why, 1024);
    // A refused run stops here: its live nodes are cancelled in the same commit, with their recorded process ids cleared, so
    // run.retry has nothing stuck `running` and nothing can ever signal a stale row.
    const now = this.#now().toISOString();
    const live = this.store.loadRows(run.run_id).filter((r) => r.state === 'running' || r.state === 'waiting');
    const rows = live.map((r) => ({ ...r, state: 'cancelled' as const, pid: undefined, pgid: undefined, proc_start: undefined, waiting: undefined, ended_at: now }));
    const events = [
      ...live.map((r) => ({ key: r.key, node_id: r.node_id, attempt: r.attempt, type: 'node.finished' as const, data: { state: 'cancelled' } })),
      { key: null, node_id: null, attempt: 0, type: 'run.finished' as const, data: { state: 'failed', error } },
    ];
    this.#publish(run.run_id, this.store.commit(run.run_id, { rows, events, run: { state: 'failed', error } }));
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
    const a = apply(rt.model, t, { now: () => this.#now().toISOString() });
    const events = this.store.commit(runId, a.change);
    for (const r of a.change.rows) rt.model.rows.set(r.key, r);
    if (a.change.run?.state) rt.model.state = a.change.run.state;
    this.#publish(runId, events);
    for (const r of a.cancelled) {
      const stop = rt.stops.get(r.key);
      // The node is already cancelled; a stop that fails (EPERM) is logged, never thrown into the event loop.
      if (stop) stop().then((res) => res === 'kept' && console.error(`workflows: could not stop ${r.key}: the operating system refused the signal`), (err: Error) => console.error(`workflows: could not stop ${r.key}: ${err.message}`));
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
          if (this.#holds === 0 && this.#alive) for (const a of due.actions) this.#launch(rt, a);
          break;
        }
      } while (rt.again);
    } finally {
      rt.driving = false;
    }
  }

  #launch(rt: Runtime, a: Action, plan?: AgentPlan): void {
    const node = rt.model.ix.byId.get(a.node_id)?.node as Node;
    rt.inflight.add(a.key);
    const link = this.#link;
    const ctx: NodeContext = { engine: this, epoch: rt.epoch, run_id: rt.id, key: a.key, node, ...(plan ? { plan } : {}) };
    const fail = (err: unknown): void => {
      if (this.#runs.get(rt.id) !== rt) return; // the run ended or was retried since: this failure is not the new attempt's
      if (link !== this.#link) return; // the connection was lost under this launch: recovery re-derives it from the rows, so its failure is not the node's
      this.failNode(rt.id, a.key, `${a.a} could not run: ${err instanceof Error ? err.message : String(err)}`, { link });
    };
    const start = (): Promise<void> => {
      try {
        return this.#executors[a.a](ctx);
      } catch (err) {
        return Promise.reject(err);
      }
    };
    // A retry of a node waits for the launch before it (a cancelled attempt's stale CREATE_SESSION and its kill): that kill
    // can dispose a worktree the retry has just re-attached, so the two must not overlap. Bounded, so a hung launch
    // cannot hold the node forever.
    const tkey = `${rt.id}/${a.key}`;
    const before = a.a === 'agent' ? this.#tails.get(tkey) : undefined;
    const p = before ? waitFor(before, LAUNCH_WAIT_MS).then(start) : start();
    if (a.a === 'agent') {
      this.#tails.set(tkey, p);
      const clear = (): void => {
        if (this.#tails.get(tkey) === p) this.#tails.delete(tkey);
      };
      p.then(clear, clear);
    }
    p.catch(fail).finally(() => {
      rt.inflight.delete(a.key);
      // An executor that returns without starting or failing its node would be launched again at once, forever.
      const s = rt.model.rows.get(a.key)?.state;
      const live = this.#runs.get(rt.id) === rt && link === this.#link;
      if (live && (s === 'pending' || s === 'interrupted')) this.failNode(rt.id, a.key, `${a.a} executor returned without starting the node`, { link });
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

  /**
   * `link` is the connection generation the caller started under: a failure from a launch the connection outlived is not the
   * node's (recovery reads it again from its row, and a newer launch may own it), so it is refused.
   */
  failNode(runId: string, key: string, error: string, extra: { cap?: string; conflict?: Row['conflict']; link?: number } = {}): boolean {
    const { link, ...rest } = extra;
    if (link !== undefined && link !== this.#link) return false;
    const r = this.#row(runId, key);
    if (this.#stopping || !r || r.state === 'done' || r.state === 'failed' || r.state === 'cancelled' || r.state === 'skipped') return false;
    this.commit(runId, { t: 'fail', key, error, ...rest });
    this.#after(runId);
    return true;
  }

  /** Reserves an agent turn against the run's cap; false when the cap failed the node. */
  reserveTurn(runId: string, key: string): boolean {
    this.commit(runId, { t: 'turn', key });
    this.#after(runId);
    return this.#row(runId, key)?.state !== 'failed';
  }

  /** The agent's turn is waiting on the user: a permission request (B-1, B-18 in `apply`). False when the node is not running. */
  waitNode(runId: string, key: string, waiting: Record<string, unknown>): boolean {
    if (this.#stopping || this.#row(runId, key)?.state !== 'running') return false;
    this.commit(runId, { t: 'wait', key, waiting });
    this.#after(runId);
    return true;
  }

  /** The permission was answered and the turn goes on: `node.resumed`. False when the node is not waiting. */
  resumeNode(runId: string, key: string): boolean {
    if (this.#stopping || this.#row(runId, key)?.state !== 'waiting') return false;
    this.commit(runId, { t: 'resume', key, reason: 'permission' });
    return true;
  }

  /** Records which transcript item and epoch an agent turn is, with the daemon boot it ran under (R2). */
  ackTurn(runId: string, key: string, a: { prompt_id: number; epoch: number }, again = false): void {
    const r = this.#row(runId, key);
    if (this.#stopping || !r || (r.state !== 'running' && r.state !== 'waiting')) return;
    this.commit(runId, { t: 'ack', key, ...a, again, ...(this.hive.welcome?.boot_id ? { boot_id: this.hive.welcome.boot_id } : {}) });
  }

  /** Why the IR's agent nodes cannot run under the daemon's catalog and ceilings right now (R6), or undefined. Asked before every run start and retry. */
  async catalogRefusal(ir: IR): Promise<string | undefined> {
    if (agentNodes(ir.nodes).next().done) return undefined;
    const reply = await this.hive.request('LIST_ACP_AGENTS', {}, { timeoutMs: 30_000 });
    const errors = catalog(ir, (reply?.agents ?? []) as AcpAgentInfo[]);
    return errors.length > 0 ? errors.join('; ') : undefined;
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
  onStop(runId: string, key: string, stop: (() => Promise<'kept' | void>) | undefined): void {
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
    const stops: Promise<unknown>[] = [];
    for (const rt of this.#runs.values()) {
      for (const r of rt.model.rows.values()) {
        if (r.state === 'running' && r.pid !== undefined) stops.push(killTree({ pid: r.pid, pgid: r.pgid }, { ...this.#killDeps, graceMs }));
      }
    }
    // One stop that fails must not skip the flush: buffered events are the only record of the output so far.
    await Promise.allSettled(stops);
    this.stopFlush();
    this.flush();
  }

  // --------------------------------------------------------------- startup

  /**
   * Picks up every run that was active when the engine last stopped. A check
   * that was running is gone with the plugin (or about to be): its recorded
   * group is reaped when the pid still is that process, and the node becomes
   * `interrupted`, which the reducer retries. A waiting human is untouched.
   * An agent node is read against the daemon (durability.ts): its turn is
   * recorded, watched or adopted when it survived, and the node is
   * `interrupted` only when it did not.
   */
  recover(): Promise<void> {
    return this.#serial(() => this.#recover());
  }

  #serial<T>(fn: () => Promise<T>): Promise<T> {
    const p = this.#chain.then(fn, fn);
    this.#chain = p.catch(() => {});
    return p;
  }

  async #recover(): Promise<void> {
    this.#holds++;
    try {
      const loaded: Runtime[] = [];
      for (const run of this.store.activeRuns()) {
        const why = this.#stale(run);
        if (why) this.#refuse(run, why);
        else loaded.push(this.#load(run));
      }
      for (const rt of loaded) {
        for (const r of [...rt.model.rows.values()]) {
          if (r.state !== 'running') continue;
          if (rt.model.ix.byId.get(r.node_id)?.node.kind !== 'check') continue;
          await this.#recoverCheck(rt, r);
        }
      }
      await recoverAgents(this, loaded.map((rt) => rt.id));
    } finally {
      this.#holds--;
    }
    // A connection lost during the read leaves the nodes as they are: the reconnect reads them again.
    for (const rt of this.#runs.values()) this.drive(rt);
  }

  async #recoverCheck(rt: Runtime, r: Row): Promise<void> {
    // A row is not trusted to name a process: a corrupt or forged pid fails the node, naming the row, and nothing is signalled.
    const bad = r.pid === undefined ? undefined : unsafeTarget({ pid: r.pid, pgid: r.pgid });
    if (bad) {
      this.commit(rt.id, { t: 'fail', key: r.key, error: `the recorded process for ${r.key} cannot be used (${bad}); nothing was signalled` });
      return;
    }
    let reaped: string = 'gone';
    if (r.pid !== undefined) reaped = await reap({ pid: r.pid, pgid: r.pgid, proc_start: r.proc_start }, this.#killDeps).catch(() => 'kept');
    // A survivor the OS will not let us stop must not be run twice at once: the node fails, naming the pid, instead of being retried.
    if (reaped === 'kept') this.commit(rt.id, { t: 'fail', key: r.key, error: `the previous attempt's process ${r.pid} is still running and could not be stopped (the operating system refused the signal); stop it by hand, then retry the run` });
    else if (reaped === 'refused') this.commit(rt.id, { t: 'fail', key: r.key, error: `the recorded process ${r.pid} for ${r.key} cannot be used; nothing was signalled` });
    else this.commit(rt.id, { t: 'interrupt', key: r.key });
  }

  // ------------------------------------------- durability (B3c, durability.ts)

  /** The agent node rows of an active run (empty for a run that is not active). */
  agentRows(runId: string): Row[] {
    const rt = this.#runs.get(runId);
    if (!rt) return [];
    return [...rt.model.rows.values()].filter((r) => rt.model.ix.byId.get(r.node_id)?.node.kind === 'agent');
  }

  /** The `timeout_s` of an agent node. */
  timeoutOf(runId: string, key: string): number {
    const rt = this.#runs.get(runId);
    const n = rt?.model.ix.byId.get(rt.model.rows.get(key)?.node_id ?? '')?.node;
    return n?.kind === 'agent' ? n.timeout_s : 0;
  }

  /** The workflow's name: what its sessions are named after. */
  workflowName(runId: string): string {
    return this.#runs.get(runId)?.model.ix.ir.name ?? '';
  }

  /** The principal the daemon stamps on this run's sessions and turns: `plugin:<id>/workflow:<run>`. */
  principalOf(runId: string): string {
    return `plugin:${this.#pluginId}/workflow:${runId}`;
  }

  /** The agent node `key` is picked up with `plan`: its row is already running, so nothing is created and no turn is reserved. */
  resumeAgent(runId: string, key: string, plan: AgentPlan): void {
    const rt = this.#runs.get(runId);
    const node = rt && rt.model.ix.byId.get(rt.model.rows.get(key)?.node_id ?? '');
    if (!rt || !node || !this.#alive) return; // a lost connection cannot run a launch; the reconnect reads the row again
    this.#launch(rt, { a: 'agent', key, node_id: node.node.id }, plan);
  }

  /** A live session recovery found for a pending node that was being created when the engine stopped: the next launch of that node takes it over. */
  adoptOrphan(runId: string, key: string, sessionId: string): void {
    this.#orphans.set(`${runId}/${key}`, sessionId);
  }

  /** The session recovery set aside for `key`, once. */
  takeOrphan(runId: string, key: string): string | undefined {
    const id = this.#orphans.get(`${runId}/${key}`);
    this.#orphans.delete(`${runId}/${key}`);
    return id;
  }

  /** Marks a running or waiting agent node `interrupted`: its turn did not survive, and the reducer retries it. `drive` also starts the retry (for a call made after recovery has finished). */
  interruptNode(runId: string, key: string, drive = false): void {
    const s = this.#row(runId, key)?.state;
    if (s !== 'running' && s !== 'waiting') return;
    this.commit(runId, { t: 'interrupt', key });
    if (drive) this.#after(runId);
  }

  /** The row of an agent node of an active run. */
  rowOf(runId: string, key: string): Row | undefined {
    return this.#row(runId, key);
  }

  // ------------------------------------------------------------- reconnect

  /**
   * The connection to the daemon is gone (`h`: the one that closed; omitted
   * means the current one). Every agent launch made so far is stale from here
   * on (it fails nothing and kills nothing: recovery re-derives it from the
   * rows), and nothing new starts until a `reconnect` survives its own read.
   * A connection that is not the current one, or is already lost, is ignored.
   * Returns whether the current connection was just lost.
   */
  disconnect(h?: Hive): boolean {
    if ((h !== undefined && h !== this.#hive) || !this.#alive) return false;
    this.#alive = false;
    this.#link++;
    this.feed.pokeAll();
    return true;
  }

  /** Whether the engine's current connection is lost. */
  get disconnected(): boolean {
    return !this.#alive;
  }

  /**
   * A new connection to the daemon (the old one overflowed or dropped): the
   * running agent nodes are read against it exactly as at startup, then the
   * runs go on. Same `boot_id` means the turns may still be running; a new
   * one means `hived` restarted and they are not. If the new connection is
   * itself lost while it is read, the engine stays disconnected and the caller
   * dials again.
   */
  reconnect(hive: Hive): Promise<void> {
    // The connection is the engine's from this call on, so a close that lands while the read is still queued behind the startup recovery is seen.
    this.#hive = hive;
    this.#alive = true;
    this.#holds++;
    return this.#serial(async () => {
      try {
        await this.#reconnect(hive);
      } finally {
        this.#holds--;
      }
      if (this.#hive !== hive || !this.#alive) return; // lost while it was read: nothing starts on a dead socket
      for (const rt of [...this.#runs.values()]) this.drive(rt);
    });
  }

  async #reconnect(hive: Hive): Promise<void> {
    this.feed.attach(hive);
    // The agent launches of the lost connection are dead: forget that they are in flight, so a node that was being created is launched again (it may adopt what that launch made).
    for (const rt of this.#runs.values()) {
      for (const key of [...rt.inflight]) if (rt.model.ix.byId.get(rt.model.rows.get(key)?.node_id ?? '')?.node.kind === 'agent') rt.inflight.delete(key);
    }
    await recoverAgents(this, [...this.#runs.keys()]);
  }
}
