// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Agent nodes (spec 529, Agent nodes). One node is one ACP session, created
// with no opening prompt and driven turn by turn through PROMPT_ACP, so every
// turn gets an ack and a failed one is an error here instead of a log line in
// the daemon. A turn ends when the transcript carries a stop reason for the
// prompt id the ack named; the node is done when that turn submitted a result.

import { type AcpAgentInfo, refusal } from '../ir/catalog.ts';
import { checkValue } from '../ir/resultschema.ts';
import type { AgentNode } from '../sdk/dsl.ts';
import type { Engine, NodeContext } from './engine.ts';
import { renderAgentPrompt } from './prompt.ts';
import type { PermissionState, TranscriptState } from './sessions.ts';
import { isConflict, prepare, type Workspace } from './worktrees.ts';

/** How long a cancelled turn gets to report `cancelled` before its session is killed. */
export const CANCEL_GRACE_MS = 10_000;
/** How long a created session gets to become ready. */
export const READY_TIMEOUT_MS = 60_000;

/** F3: the one re-prompt after a turn that ended without a result. */
export const REPROMPT = 'You ended the turn without calling submit_result. Call it now with your result.';

export interface AgentDeps {
  cancelGraceMs?: number;
  readyTimeoutMs?: number;
}

interface Outcome {
  stop_reason: string;
  result_status: string;
  result?: unknown;
}

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);
const msgOf = (e: unknown): string => (e instanceof Error ? e.message : String(e));

export function agentExecutor(deps: AgentDeps = {}): (ctx: NodeContext) => Promise<void> {
  return async (ctx) => {
    await new AgentRun(ctx, deps).run();
  };
}

class AgentRun {
  readonly #ctx: NodeContext;
  readonly #engine: Engine;
  readonly #n: AgentNode;
  readonly #graceMs: number;
  readonly #readyMs: number;
  #ws: Workspace | undefined;
  #sid: string | undefined;
  #ack: { prompt_id: number; epoch: number } | undefined;
  #timer: ReturnType<typeof setTimeout> | undefined;
  #deadline = 0;
  #left: number | undefined;
  #stopped: Promise<void> | undefined;
  #abort: (() => void) | undefined;
  #unsubs = new Set<() => void>();

  constructor(ctx: NodeContext, deps: AgentDeps) {
    this.#ctx = ctx;
    this.#engine = ctx.engine;
    this.#n = ctx.node as AgentNode;
    this.#graceMs = deps.cancelGraceMs ?? CANCEL_GRACE_MS;
    this.#readyMs = deps.readyTimeoutMs ?? READY_TIMEOUT_MS;
  }

  get #state(): string | undefined {
    return this.#engine.run(this.#ctx.run_id).model.rows.get(this.#ctx.key)?.state;
  }

  /** Whether this launch still owns the node: the run is the live one and the node is in one of `states`. */
  #live(...states: string[]): boolean {
    return this.#engine.current(this.#ctx) && states.includes(this.#state ?? '');
  }

  #fail(why: string): void {
    if (this.#engine.current(this.#ctx)) this.#engine.failNode(this.#ctx.run_id, this.#ctx.key, why);
  }

  async run(): Promise<void> {
    const { run_id, key } = this.#ctx;
    if (!this.#live('pending', 'interrupted')) return;
    if (!this.#engine.reserveTurn(run_id, key)) return; // the 50-turn cap failed the node
    try {
      await this.#go();
    } catch (err) {
      this.#fail(`agent could not run: ${msgOf(err)}`);
      void this.stop();
    } finally {
      // A stop in progress still needs the feed: let it finish before the session's state is dropped.
      await this.#stopped?.catch(() => {});
      this.#cleanup();
    }
  }

  async #go(): Promise<void> {
    const engine = this.#engine;
    const hive = engine.hive;
    const { run_id, key } = this.#ctx;
    const n = this.#n;
    const { model, project_id } = engine.run(run_id);

    // The catalog is asked on every launch, so a ceiling the user lowered since the run started applies (R6). The mode is always explicit: an empty one means the ceiling.
    const listed = await hive.request('LIST_ACP_AGENTS', {}, { timeoutMs: 30_000 });
    if (!this.#live('pending', 'interrupted')) return;
    const agents = (listed?.agents ?? []) as AcpAgentInfo[];
    const why = refusal(n, agents);
    if (why) return this.#fail(why);
    const mode = n.mode ?? agents.find((a) => a.id === n.agent)?.modes[0]?.id;
    if (!mode) return this.#fail(`agent "${n.agent}" advertises no permission modes`);

    const prompt = renderAgentPrompt(model, key, engine.store.getRun(run_id)?.nonce ?? '');
    const ws = await prepare(engine, run_id, key);
    if (!this.#live('pending', 'interrupted')) return;
    if (isConflict(ws)) return void engine.failNode(run_id, key, ws.message, { conflict: ws.conflict });
    this.#ws = ws;

    // No initial_prompt: an opening prompt that fails is only logged by the daemon, and its turn gets no ack.
    const created = await hive.request(
      'CREATE_SESSION',
      { kind: 'acp', agent: n.agent, acp_mode: mode, project_id, worktree_path: ws.path, workflow_run_id: run_id, result_schema: n.output, name: `${model.ix.ir.name} · ${key}` },
      { timeoutMs: 60_000 },
    );
    const sid = created?.session_id as string | undefined;
    if (!sid) return this.#fail('the daemon did not name the new session');
    this.#sid = sid;
    engine.feed.track(sid);
    if (!engine.beginNode(run_id, key, { session_id: sid, branch: ws.branch, worktree: ws.path })) {
      await this.#kill(); // cancelled while the session was being made
      return;
    }
    engine.onStop(run_id, key, () => this.stop());

    const ready = await this.#ready();
    if (ready !== undefined) return this.#fail(ready);
    this.#arm(n.timeout_s * 1000);

    let text = prompt;
    for (let again = false; ; again = true) {
      const out = await this.#turn(text, again);
      if (!out) return;
      if (out.stop_reason !== 'end_turn') return this.#fail(`the agent's turn ended with stop reason "${out.stop_reason}"`);
      if (out.result_status === 'submitted') return await this.#finish(out.result);
      // F3: a turn that ends without submit_result is never taken as success. One more try, then the node fails.
      if (again) return this.#fail('the agent ended its turn twice without calling submit_result');
      text = REPROMPT;
    }
  }

  // ----------------------------------------------------------------- ready

  /** Undefined once the session is ready; otherwise why it will not be. */
  async #ready(): Promise<string | undefined> {
    const sid = this.#sid as string;
    const feed = this.#engine.feed;
    return await new Promise((resolve) => {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const done = (v: string | undefined): void => {
        clearTimeout(timer);
        unsub();
        this.#unsubs.delete(unsub);
        resolve(v);
      };
      const look = (): void => {
        if (!this.#live('running')) return done('the node was stopped before its session was ready');
        const info = feed.session(sid);
        const err = info?.last_error ?? feed.error(sid);
        if (err) done(`the agent session failed to start: ${err}`);
        else if (info?.removed) done('the agent session was closed before it was ready');
        else if (info?.alive && info.phase === '') done(undefined);
      };
      const unsub = feed.subscribe(sid, look);
      this.#unsubs.add(unsub);
      timer = setTimeout(() => done(`the agent session was not ready after ${Math.round(this.#readyMs / 1000)}s`), this.#readyMs);
      timer.unref?.();
      look();
    });
  }

  // ------------------------------------------------------------------ turns

  async #turn(text: string, again: boolean): Promise<Outcome | undefined> {
    const { run_id, key } = this.#ctx;
    // Every PROMPT_ACP counts against the run's cap, the re-prompt included.
    if (again && !this.#engine.reserveTurn(run_id, key)) return undefined;
    if (!this.#live('running', 'waiting')) return undefined;
    const ack = await this.#engine.hive.request('PROMPT_ACP', { session_id: this.#sid, text, workflow_run_id: run_id }, { timeoutMs: 60_000 });
    this.#ack = { prompt_id: ack?.prompt_id ?? 0, epoch: ack?.epoch ?? 0 };
    if (this.#ack.prompt_id === 0) throw new Error('the daemon did not report the turn it started');
    this.#engine.ackTurn(run_id, key, this.#ack);
    return await this.#outcome(this.#ack);
  }

  /** Waits for the prompt `ack` names to end. Undefined when the node stopped being ours or failed meanwhile. */
  #outcome(ack: { prompt_id: number; epoch: number }): Promise<Outcome | undefined> {
    const sid = this.#sid as string;
    const feed = this.#engine.feed;
    return new Promise((resolve) => {
      let over = false;
      const finish = (o: Outcome | undefined): void => {
        if (over) return;
        over = true;
        unsub();
        this.#unsubs.delete(unsub);
        this.#abort = undefined;
        resolve(o);
      };
      const look = (): void => {
        if (over) return;
        if (!this.#live('running', 'waiting')) return finish(undefined);
        this.#follow(feed.session(sid)?.kind);
        const info = feed.session(sid);
        if (info?.removed || info?.last_error) {
          this.#fail(info.removed ? 'the agent session was closed during its turn' : `the agent session failed: ${info.last_error}`);
          return finish(undefined);
        }
        const tx = feed.transcript(sid);
        // A message for another turn or epoch is not this turn's.
        if (!tx || tx.epoch !== ack.epoch || tx.prompt_id !== ack.prompt_id) return;
        this.#permission(tx.permission);
        if (!this.#live('running', 'waiting')) return finish(undefined);
        if (tx.stop_reason) finish(outcomeOf(tx));
      };
      const unsub = feed.subscribe(sid, look);
      this.#unsubs.add(unsub);
      this.#abort = () => finish(undefined);
      look();
    });
  }

  /** Mirrors the transcript's pending permission onto the node: waiting while it is open, resumed when it clears (B-1). */
  #permission(p: PermissionState | undefined): void {
    const engine = this.#engine;
    const { run_id, key } = this.#ctx;
    const row = engine.run(run_id).model.rows.get(key);
    if (p) {
      if (row?.state === 'waiting' && row.waiting?.request_id === p.request_id) return; // every message carries the pending request
      if (row?.state === 'waiting') engine.resumeNode(run_id, key); // a different request replaced it
      engine.waitNode(run_id, key, { reason: 'permission', session_id: this.#sid, request_id: p.request_id, title: p.title ?? '', options: p.options });
      // An id the GUI could not send back unchanged fails the node (B-18); the agent is blocked on it, so its turn is cancelled.
      if (this.#state === 'failed') void this.stop();
    } else if (row?.state === 'waiting') engine.resumeNode(run_id, key);
  }

  // -------------------------------------------------------------- the result

  async #finish(result: unknown): Promise<void> {
    const engine = this.#engine;
    const { run_id, key } = this.#ctx;
    const env = isObj(result) ? result : undefined;
    if (!env) return this.#fail('the agent submitted a result that is not an object');
    if (env.status !== 'ok') {
      const summary = typeof env.summary === 'string' && env.summary !== '' ? `: ${env.summary}` : '';
      return this.#fail(`the agent reported that it could not do the task${summary}`);
    }
    const bad = checkValue(this.#n.output, env.data, 'result');
    if (bad) return this.#fail(`the agent's result does not match the node's output: ${bad}`);

    // The work is committed before the node is done, so the next node starts from it.
    const attempt = engine.run(run_id).model.rows.get(key)?.attempt ?? 0;
    try {
      await engine.hive.request(
        'COMMIT_WORKTREE',
        { project_id: engine.run(run_id).project_id, workflow_run_id: run_id, branch: (this.#ws as Workspace).branch, message: `wf: ${key} (attempt ${attempt})` },
        { timeoutMs: 120_000 },
      );
    } catch (err) {
      return this.#fail(`could not commit the agent's work: ${msgOf(err)}`);
    }
    if (!this.#live('running', 'waiting')) return;
    engine.completeNode(run_id, key, env.data);
  }

  // -------------------------------------------------- timeout, cancel, takeover

  #arm(ms: number): void {
    this.#left = undefined;
    this.#deadline = Date.now() + ms;
    this.#timer = setTimeout(() => {
      this.#timer = undefined;
      this.#fail(`timed out after ${this.#n.timeout_s}s`);
      void this.stop();
    }, ms);
    this.#timer.unref?.(); // a node's clock never keeps the plugin alive
  }

  /** F2: while the user has taken the session over, the node waits and its clock stops. */
  #follow(kind: string | undefined): void {
    if (kind === 'pty' && this.#timer) {
      clearTimeout(this.#timer);
      this.#timer = undefined;
      this.#left = Math.max(1, this.#deadline - Date.now());
    } else if (kind === 'acp' && this.#left !== undefined && !this.#timer) this.#arm(this.#left);
  }

  /** Whether the turn that was started has ended (or none was). */
  #ended(): boolean {
    if (!this.#ack || !this.#sid) return true;
    const tx: TranscriptState | undefined = this.#engine.feed.transcript(this.#sid);
    return tx?.epoch === this.#ack.epoch && tx.prompt_id === this.#ack.prompt_id && tx.stop_reason !== '';
  }

  /**
   * Cancels the turn, pinned to the prompt that is ours so it cannot stop the
   * next one. If the adapter has not reported the turn over after the grace, the
   * session is killed; the worktree is kept either way. Safe to call twice.
   */
  stop(): Promise<void> {
    this.#stopped ??= (async () => {
      const sid = this.#sid;
      if (!sid || this.#ended()) return;
      try {
        await this.#engine.hive.request('CANCEL_ACP', { session_id: sid, ...(this.#ack ?? {}) });
      } catch {
        // The turn may already be over; the wait below decides.
      }
      await this.#until(() => this.#ended(), this.#graceMs);
      if (!this.#ended()) await this.#kill();
    })();
    this.#abort?.();
    return this.#stopped;
  }

  async #kill(): Promise<void> {
    if (!this.#sid) return;
    try {
      await this.#engine.hive.request('KILL_SESSION', { session_id: this.#sid });
    } catch {
      // Already gone, or the daemon is: nothing more to do for it.
    }
  }

  #until(pred: () => boolean, ms: number): Promise<void> {
    const sid = this.#sid as string;
    return new Promise((resolve) => {
      if (pred()) return resolve();
      const done = (): void => {
        clearTimeout(timer);
        unsub();
        resolve();
      };
      const unsub = this.#engine.feed.subscribe(sid, () => pred() && done());
      const timer = setTimeout(done, ms);
      timer.unref?.();
    });
  }

  #cleanup(): void {
    clearTimeout(this.#timer);
    for (const u of this.#unsubs) u();
    this.#unsubs.clear();
    if (this.#engine.current(this.#ctx)) this.#engine.onStop(this.#ctx.run_id, this.#ctx.key, undefined);
    if (this.#sid) this.#engine.feed.forget(this.#sid);
  }
}

function outcomeOf(tx: TranscriptState): Outcome {
  return { stop_reason: tx.stop_reason, result_status: tx.result_status, ...(tx.result === undefined ? {} : { result: tx.result }) };
}
