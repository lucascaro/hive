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
import type { AgentPlan, Engine, NodeContext } from './engine.ts';
import type { Row } from './model.ts';
import { renderAgentPrompt } from './prompt.ts';
import type { PermissionState, SessionState, TranscriptState } from './sessions.ts';
import { isConflict, prepare, type Workspace } from './worktrees.ts';

/** How long a cancelled turn gets to report `cancelled` before its session is killed. */
export const CANCEL_GRACE_MS = 10_000;
/** How long a created session gets to become ready. */
export const READY_TIMEOUT_MS = 60_000;

/** F3: the one re-prompt after a turn that ended without a result. */
export const REPROMPT = 'You ended the turn without calling submit_result. Call it now with your result.';

/** What a retried attempt is told: the worktree is the previous attempt's, and it may hold half of its work. */
export const INTERRUPTED_NOTE =
  'Your previous attempt was interrupted; the worktree may hold partial work. Check what is there, finish the task, and call submit_result when done. The original task follows.\n\n';

/** Where a test can hold an agent node still, so a crash lands exactly there (internal/workflowe2e). Never set in a release. */
export type PausePoint = 'after-create' | 'before-prompt' | 'after-prompt';

export interface AgentDeps {
  cancelGraceMs?: number;
  readyTimeoutMs?: number;
  /** How often a session being revived is looked at again. */
  settlePollMs?: number;
  pause?: (point: PausePoint) => Promise<void>;
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
  readonly #deps: AgentDeps;
  readonly #graceMs: number;
  readonly #readyMs: number;
  /** The connection this launch was made under: once it is lost the launch is stale and touches nothing (B3c). */
  readonly #link: number;
  /** This attempt follows an interrupted one: its prompt carries the note, and its old session is reused if it is still there. */
  #retry = false;
  #ws: Workspace | undefined;
  #sid: string | undefined;
  #ack: { prompt_id: number; epoch: number } | undefined;
  /** The PROMPT_ACP in flight, until its ack has been recorded: a stop that arrives meanwhile must cancel the turn that ack names. */
  #sending: Promise<unknown> | undefined;
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
    this.#deps = deps;
    this.#graceMs = deps.cancelGraceMs ?? CANCEL_GRACE_MS;
    this.#readyMs = deps.readyTimeoutMs ?? READY_TIMEOUT_MS;
    this.#link = ctx.link ?? ctx.engine.link;
  }

  get #row(): Row | undefined {
    return this.#engine.run(this.#ctx.run_id).model.rows.get(this.#ctx.key);
  }

  get #state(): string | undefined {
    return this.#row?.state;
  }

  /** Whether the connection this launch started under is still the engine's. */
  #fresh(): boolean {
    return this.#engine.link === this.#link;
  }

  /** Whether this launch still owns the node: the connection and run are the live ones and the node is in one of `states`. */
  #live(...states: string[]): boolean {
    return this.#fresh() && this.#engine.current(this.#ctx) && states.includes(this.#state ?? '');
  }

  #fail(why: string): void {
    if (this.#fresh() && this.#engine.current(this.#ctx)) this.#engine.failNode(this.#ctx.run_id, this.#ctx.key, why, { link: this.#link });
  }

  async run(): Promise<void> {
    const { plan } = this.#ctx;
    if (plan) {
      if (!this.#live('running', 'waiting')) return; // picked up after a crash: the row is already running
    } else {
      if (!this.#live('pending', 'interrupted')) return;
      this.#retry = this.#state === 'interrupted';
    }
    try {
      await (plan ? this.#resumed(plan) : this.#go());
    } catch (err) {
      // A connection lost under this launch is not the node's failure: recovery reads it again from its row.
      if (this.#fresh()) {
        this.#fail(`agent could not run: ${msgOf(err)}`);
        void this.stop();
      }
    } finally {
      // A stop in progress still needs the feed: let it finish before the session's state is dropped.
      await this.#stopped?.catch(() => {});
      this.#cleanup();
    }
  }

  /** The catalog is asked on every launch, so a ceiling the user lowered since the run started applies (R6). The mode is always explicit: an empty one means the ceiling. */
  async #catalog(): Promise<{ mode: string } | { why: string } | undefined> {
    const n = this.#n;
    const listed = await this.#engine.hive.request('LIST_ACP_AGENTS', {}, { timeoutMs: 30_000 });
    if (!this.#live('pending', 'interrupted', 'running', 'waiting')) return undefined;
    const agents = (listed?.agents ?? []) as AcpAgentInfo[];
    const why = refusal(n, agents);
    if (why) return { why };
    const mode = n.mode ?? agents.find((a) => a.id === n.agent)?.modes[0]?.id;
    return mode ? { mode } : { why: `agent "${n.agent}" advertises no permission modes` };
  }

  async #go(): Promise<void> {
    const engine = this.#engine;
    const { run_id, key } = this.#ctx;
    const n = this.#n;
    const { model, project_id } = engine.run(run_id);

    const cat = await this.#catalog();
    if (!cat) return;
    if ('why' in cat) return this.#fail(cat.why);

    // A retry carries a note: the worktree is the interrupted attempt's, and may hold half of its work.
    const task = renderAgentPrompt(model, key, engine.store.getRun(run_id)?.nonce ?? '');
    const prompt = this.#retry ? INTERRUPTED_NOTE + task : task;
    const ws = await prepare(engine, run_id, key);
    if (!this.#live('pending', 'interrupted')) return;
    if (isConflict(ws)) return void engine.failNode(run_id, key, ws.message, { conflict: ws.conflict, link: this.#link });
    this.#ws = ws;

    const got = await this.#session(cat.mode, project_id, `${model.ix.ir.name} · ${key}`);
    if (!got) return;
    const { sid, ours, created } = got;
    this.#sid = sid;
    engine.feed.track(sid);
    if (created) await this.#deps.pause?.('after-create');
    const at = this.#ws as Workspace;
    const boot = engine.hive.welcome?.boot_id;
    // CREATE_SESSION can take a while: a cancel and retry meanwhile must not let this stale launch claim the new attempt's node.
    if (!this.#fresh() || !engine.current(this.#ctx) || !engine.beginNode(run_id, key, { session_id: sid, branch: at.branch, worktree: at.path, ...(boot ? { boot_id: boot } : {}) })) {
      if (ours && this.#fresh()) await this.#kill(); // cancelled while the session was being made
      return;
    }
    engine.onStop(run_id, key, () => this.stop());

    const ready = await this.#ready();
    if (ready !== undefined) return this.#fail(ready);
    this.#arm(n.timeout_s * 1000);
    await this.#deps.pause?.('before-prompt');
    return await this.#loop(prompt, undefined, false);
  }

  /** The turns of the node: send the prompt (or watch the one already sent), and F3 one re-prompt after a turn that ended without a result. */
  async #loop(prompt: string, watching: { prompt_id: number; epoch: number } | undefined, reprompted: boolean): Promise<void> {
    let text = prompt;
    let pending = watching;
    for (let again = reprompted; ; again = true) {
      const out = pending ? await this.#watch(pending) : await this.#turn(text, again);
      pending = undefined;
      if (!out) return;
      if (out.stop_reason !== 'end_turn') return this.#fail(`the agent's turn ended with stop reason "${out.stop_reason}"`);
      if (out.result_status === 'submitted') return await this.#finish(out.result);
      // F3: a turn that ends without submit_result is never taken as success. One more try, then the node fails.
      if (again) return this.#fail('the agent ended its turn twice without calling submit_result');
      text = REPROMPT;
    }
  }

  /**
   * A node whose row is already running, picked up after a plugin crash or a
   * reconnect (B3c). Its session and worktree are on the row; a turn that was
   * started is watched from the ack, and one that never was is sent now.
   */
  async #resumed(plan: AgentPlan): Promise<void> {
    const engine = this.#engine;
    const { run_id, key } = this.#ctx;
    const row = this.#row as Row;
    if (!row.session_id || !row.worktree || !row.branch) return this.#fail('the node has no session or worktree recorded to pick up');
    this.#sid = row.session_id;
    this.#ws = { path: row.worktree, branch: row.branch };
    engine.feed.track(row.session_id);
    engine.onStop(run_id, key, () => this.stop());
    // The clock kept running while the engine was away.
    const since = Date.parse(row.started_at ?? '');
    this.#arm(Math.max(1000, this.#n.timeout_s * 1000 - (Number.isFinite(since) ? Date.now() - since : 0)));
    if (plan.kind === 'watch') {
      this.#ack = plan.ack;
      return await this.#loop('', plan.ack, plan.again);
    }
    const cat = await this.#catalog();
    if (!cat) return;
    if ('why' in cat) return this.#fail(cat.why);
    const ready = await this.#ready();
    if (ready !== undefined) return this.#fail(ready);
    const { model } = engine.run(run_id);
    const task = renderAgentPrompt(model, key, engine.store.getRun(run_id)?.nonce ?? '');
    // An attempt after the first is a retry, whichever launch sends its first turn.
    return await this.#loop(row.attempt > 1 ? INTERRUPTED_NOTE + task : task, undefined, false);
  }

  // ---------------------------------------------------------------- session

  /**
   * The session this attempt runs in. A session an earlier incarnation of the
   * engine made for this node (an orphan) or this node's own interrupted one
   * is taken over when it is still usable; otherwise a fresh one is created
   * in the same worktree. Undefined when the launch is no longer the node's.
   */
  async #session(mode: string, project_id: string, name: string): Promise<{ sid: string; ours: boolean; created: boolean } | undefined> {
    const { run_id, key } = this.#ctx;
    const orphan = this.#engine.takeOrphan(run_id, key);
    const cand = orphan ?? (this.#retry ? this.#row?.session_id : undefined);
    if (cand) {
      const reused = await this.#reuse(cand, orphan !== undefined);
      if (reused === undefined) return undefined;
      if (reused) return { sid: cand, ours: orphan !== undefined, created: false };
    }
    const ws = this.#ws as Workspace;
    const created = await this.#engine.hive.request(
      'CREATE_SESSION',
      { kind: 'acp', agent: this.#n.agent, acp_mode: mode, project_id, worktree_path: ws.path, workflow_run_id: run_id, result_schema: this.#n.output, name },
      { timeoutMs: 60_000 },
    );
    const sid = created?.session_id as string | undefined;
    if (!sid) {
      this.#fail('the daemon did not name the new session');
      return undefined;
    }
    return { sid, ours: true, created: true };
  }

  /**
   * Whether `sid` can run this attempt: true, false (make a new one) or
   * undefined (the launch is stale or the node failed). A session still being
   * revived is waited for; a dead one is restarted (R3), and when it cannot be
   * (no `loadSession`, or the load fails) it is killed with its worktree kept
   * and a fresh session takes its place. An orphan was never prompted, so a
   * dead one is just removed.
   */
  async #reuse(sid: string, orphan: boolean): Promise<boolean | undefined> {
    const info = await this.#settled(sid);
    if (info === 'stale') return undefined;
    const ws = this.#ws as Workspace;
    if (!info || info.removed) return false;
    if (info.worktree_path !== undefined && info.worktree_path !== ws.path) {
      await this.#kill(sid); // not in the worktree this node works in: it is nobody's
      return false;
    }
    if (info.kind === 'pty') {
      this.#fail('the previous attempt\'s session was taken over, so it cannot be prompted');
      return undefined;
    }
    if (info.alive && !info.last_error) return true;
    if (!orphan) {
      try {
        await this.#engine.hive.request('RESTART_SESSION', { session_id: sid }, { timeoutMs: 60_000 });
        const again = await this.#settled(sid);
        if (again === 'stale') return undefined;
        if (again?.alive && !again.last_error) return true;
      } catch (err) {
        if (!this.#fresh()) throw err;
        // restart_failed: the agent cannot load the old conversation (or the session is gone). A fresh session, same worktree.
      }
    }
    await this.#kill(sid);
    if (!this.#live('pending', 'interrupted')) return undefined;
    // Closing the last session of a hive-managed worktree makes the daemon remove it only when it is Pristine (nothing uncommitted, no unpushed commits, and
    // it could be inspected): a worktree holding work is kept. Preparing it again re-attaches one that was kept and recreates one that was removed.
    const again = await prepare(this.#engine, this.#ctx.run_id, this.#ctx.key);
    if (!this.#live('pending', 'interrupted')) return undefined;
    if (isConflict(again)) {
      this.#engine.failNode(this.#ctx.run_id, this.#ctx.key, again.message, { conflict: again.conflict, link: this.#link });
      return undefined;
    }
    this.#ws = again;
    return false;
  }

  /** The session once it is no longer starting or being revived (`phase` empty), read from the daemon each time. 'stale' when the launch is no longer the node's. */
  async #settled(sid: string): Promise<SessionState | undefined | 'stale'> {
    const engine = this.#engine;
    const poll = this.#deps.settlePollMs ?? 200;
    const until = Date.now() + this.#readyMs;
    for (;;) {
      const send = engine.hive.send?.bind(engine.hive);
      const listed = send ? await engine.feed.listSessions(send) : undefined;
      if (!this.#live('pending', 'interrupted')) return 'stale';
      const info = listed ? listed.find((s) => s.id === sid) : engine.feed.session(sid);
      if (!info || info.removed || info.phase === '' || Date.now() >= until) return info;
      await new Promise<void>((r) => {
        const t = setTimeout(r, poll);
        t.unref?.();
      });
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
    // Every PROMPT_ACP counts against the run's cap, the first and the re-prompt alike. It is charged here, at the send, so a launch that the
    // connection cut short before any prompt (and is relaunched) is not charged twice.
    if (!this.#engine.reserveTurn(run_id, key)) {
      void this.stop(); // the cap failed the node: the session it made has no turn and is closed
      return undefined;
    }
    if (!this.#live('running', 'waiting')) return undefined;
    const sending = this.#engine.hive.request('PROMPT_ACP', { session_id: this.#sid, text, workflow_run_id: run_id }, { timeoutMs: 60_000 }).then((ack) => {
      this.#ack = { prompt_id: ack?.prompt_id ?? 0, epoch: ack?.epoch ?? 0 };
    });
    this.#sending = sending;
    await sending;
    const ack = this.#ack as { prompt_id: number; epoch: number };
    if (ack.prompt_id === 0) throw new Error('the daemon did not report the turn it started');
    await this.#deps.pause?.('after-prompt');
    // A stop that came while the prompt was in flight cancels this turn itself (stop waits for the ack); there is nothing to wait for here.
    if (!this.#live('running', 'waiting')) return undefined;
    this.#engine.ackTurn(run_id, key, ack, again);
    return await this.#outcome(ack);
  }

  /** A turn that was started before this launch existed (a crash or reconnect): it is the node's turn from here on. */
  #watch(ack: { prompt_id: number; epoch: number }): Promise<Outcome | undefined> {
    this.#ack = ack;
    return this.#outcome(ack);
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
      if (!this.#fresh()) return;
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
    if (!this.#ack || !this.#sid) return false;
    const tx: TranscriptState | undefined = this.#engine.feed.transcript(this.#sid);
    return tx?.epoch === this.#ack.epoch && tx.prompt_id === this.#ack.prompt_id && tx.stop_reason !== '';
  }

  /**
   * Cancels the turn, pinned to the prompt that is ours so it cannot stop the
   * next one. If the adapter has not reported the turn over after the grace, the
   * session is killed; the worktree is kept either way. Safe to call twice.
   */
  stop(): Promise<void> {
    // A launch the connection outlived is stale: its session belongs to the launch that replaced it, and this one speaks for no one.
    if (!this.#fresh()) return (this.#stopped ??= Promise.resolve());
    this.#stopped ??= (async () => {
      const sid = this.#sid;
      if (!sid) return;
      // A prompt that is still being sent has started a turn the daemon will not tell us about until its ack: wait for it, so the cancel is pinned to that turn.
      await this.#sending?.catch(() => {});
      // No turn was ever started (cancelled while the session was becoming ready, or the first prompt was refused): nothing to cancel, so the idle session is closed.
      if (!this.#ack) return await this.#kill();
      if (this.#ended()) return;
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

  async #kill(sid = this.#sid): Promise<void> {
    if (!sid) return;
    try {
      await this.#engine.hive.request('KILL_SESSION', { session_id: sid, force: true });
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
    // A launch the connection outlived is stale: the launch that replaced it owns the stop and the session's state now.
    if (!this.#fresh()) return;
    if (this.#engine.current(this.#ctx)) this.#engine.onStop(this.#ctx.run_id, this.#ctx.key, undefined);
    if (this.#sid) this.#engine.feed.forget(this.#sid);
  }
}

function outcomeOf(tx: TranscriptState): Outcome {
  return { stop_reason: tx.stop_reason, result_status: tx.result_status, ...(tx.result === undefined ? {} : { result: tx.result }) };
}
