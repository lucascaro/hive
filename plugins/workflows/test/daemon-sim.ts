// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// A scripted daemon for agent nodes (spec 529): it answers the requests an
// agent node makes and plays the frames the daemon would send back, in the
// order a test chooses, including the awkward ones (a turn that ends before its
// ack is read). Nothing here is the engine; it is only what the engine talks to.
//
// The daemon outlives the engine in B3c's tests: `attach` points it at a new
// connection, the way a restarted plugin dials the same daemon again.

import type { FakeHive } from './harness.ts';

export const CLAUDE = { id: 'claude', name: 'Claude', modes: [{ id: 'plan', label: 'Plan' }, { id: 'default', label: 'Default' }, { id: 'acceptEdits', label: 'Accept edits' }], default_mode: 'plan', ceiling: 'default', available: true };

type Data = Record<string, unknown>;

export interface Permission {
  request_id: string;
  title?: string;
  options: { option_id: string; name: string; kind: string }[];
}

export interface UserItem {
  id: number;
  origin: string;
  text: string;
}

/** One session: the state every ACP_TRANSCRIPT message of it carries. */
export class SimSession {
  readonly id: string;
  hive: FakeHive;
  name = '';
  spawned_by = '';
  worktree_path = '';
  removed = false;
  /** What the latest SESSION_EVENT said: each one is complete, as in the old sim. */
  cur: Data = { alive: true, kind: 'acp' };
  epoch = 1;
  prompt_id = 0;
  permission: Permission | undefined;
  result_status = '';
  result: unknown;
  stop_reason = '';
  users: UserItem[] = [];
  /** The daemon trims the oldest items of a long transcript: a snapshot leaves out this many of the oldest user items. */
  trim = 0;

  constructor(hive: FakeHive, id: string) {
    this.hive = hive;
    this.id = id;
  }

  /** The SessionInfo the daemon would list. */
  info(): Data {
    return { id: this.id, name: this.name, spawned_by: this.spawned_by, worktree_path: this.worktree_path, ...this.cur };
  }

  /** A session event: ready is `alive` with no phase. */
  event(kind: 'added' | 'updated' | 'removed', fields: Data = {}): void {
    this.cur = { alive: true, kind: 'acp', ...fields };
    this.hive.emit('SESSION_EVENT', { kind, session: this.info() });
  }

  /** Starts a new prompt: everything of the last one is cleared. */
  begin(origin = '', text = ''): number {
    this.prompt_id += 10;
    this.permission = undefined;
    this.result_status = '';
    this.result = undefined;
    this.stop_reason = '';
    this.users.push({ id: this.prompt_id, origin, text });
    this.send([{ id: this.prompt_id, kind: 'user', origin, text }]);
    return this.prompt_id;
  }

  /** The state fields every message carries; `items` and `reset` are the message's own. */
  state(): Data {
    return {
      session_id: this.id,
      epoch: this.epoch,
      prompt_id: this.prompt_id,
      ...(this.permission ? { permission: this.permission } : {}),
      ...(this.result_status ? { result_status: this.result_status } : {}),
      ...(this.result === undefined ? {} : { result: this.result }),
      ...(this.stop_reason ? { stop_reason: this.stop_reason } : {}),
    };
  }

  send(items: Data[] = []): void {
    this.hive.emit('ACP_TRANSCRIPT', { ...this.state(), ...(items.length > 0 ? { items } : {}) });
  }

  /** The snapshot GET_ACP_TRANSCRIPT answers with: the whole transcript, as a reset. */
  snapshot(to: FakeHive = this.hive): void {
    to.emit('ACP_TRANSCRIPT', { ...this.state(), reset: true, items: this.users.slice(this.trim).map((u) => ({ id: u.id, kind: 'user', origin: u.origin, text: u.text })) });
  }

  ask(p: Permission): void {
    this.permission = p;
    this.send();
  }

  answer(): void {
    this.permission = undefined;
    this.send();
  }

  submit(envelope: unknown): void {
    this.result_status = 'submitted';
    this.result = envelope;
    this.send();
  }

  /** Ends the turn. `result` false: the daemon reports `none`. */
  end(stop_reason = 'end_turn'): void {
    this.permission = undefined;
    if (!this.result_status) this.result_status = 'none';
    this.stop_reason = stop_reason;
    this.send();
  }

  /** What a restarted adapter looks like: a new epoch, nothing said yet. */
  reset(): void {
    this.trim = 0; // the trim counted the old epoch's items
    this.epoch++;
    this.prompt_id = 0;
    this.permission = undefined;
    this.result_status = '';
    this.result = undefined;
    this.stop_reason = '';
    this.users = [];
  }
}

export interface Sim {
  sessions: SimSession[];
  prompts: string[];
  /** Requests of one verb, in order, across every connection the sim has served. */
  of(name: string): Data[];
  /** Serves a new connection (the plugin dialled again, or was restarted). */
  attach(hive: FakeHive): void;
}

export interface SimOptions {
  agents?: unknown[];
  /** Called when a turn starts (after the daemon acked it, or before, per `ackFirst`). */
  onPrompt(turn: number, text: string, s: SimSession): void;
  /** Whether the ack is delivered before the turn's frames (default), or after them. */
  ackFirst?: boolean;
  /** Fail CREATE_SESSION with this error. */
  createError?: string;
  /** Do not announce the session as ready. */
  neverReady?: boolean;
  /** Called when RESTART_SESSION arrives: throw to refuse it (`restart_failed`). */
  onRestart?: (s: SimSession) => void;
  /** Refuse every KILL_SESSION. */
  killFails?: boolean;
  /** Delay CREATE_SESSION's reply, to hold a launch in flight. */
  holdCreate?: Promise<void>;
  /** Drop the PROMPT_ACP ack (the daemon starts the turn; the reply never comes). */
  dropPromptAck?: (turn: number) => boolean;
}

const hiveError = (code: string, message: string): Error => Object.assign(new Error(message), { hive: { code, message } });

export function simulate(hive: FakeHive, o: SimOptions): Sim {
  const log: { name: string; payload: Data }[] = [];
  const sim: Sim = {
    sessions: [],
    prompts: [],
    of: (name) => log.filter((r) => r.name === name).map((r) => r.payload),
    attach: (h) => install(h),
  };
  let held = o.holdCreate;
  let nSessions = 0;
  let worktree = 0;
  const paths = new Map<string, string>();
  const live = (): SimSession[] => sim.sessions.filter((s) => !s.removed);

  function install(h: FakeHive): void {
    for (const s of sim.sessions) s.hive = h;
    const base = h.request.bind(h);
    h.request = (name, payload = {}) => {
      log.push({ name, payload: payload as Data });
      return base(name, payload);
    };
    h.replies.LIST_ACP_AGENTS = () => ({ agents: o.agents ?? [CLAUDE] });
    h.replies.PREPARE_WORKTREE = (req) => {
      const branch = req.branch as string;
      const known = paths.get(branch);
      const path = known ?? `/wt/${branch.split('/').slice(3).join('_') || 'x'}-${worktree++}`;
      paths.set(branch, path);
      return { path, branch, base_sha: req.base === 'HEAD' ? 'sha-1' : undefined, head_sha: 'sha-1', reattached: known !== undefined, merged: [] };
    };
    h.replies.COMMIT_WORKTREE = () => ({ committed: true, head: 'sha-2' });
    h.replies.CANCEL_ACP = () => ({ was_busy: true });
    h.replies.KILL_SESSION = (req) => {
      if (o.killFails) throw hiveError('kill_failed', 'the daemon refused');
      const s = sim.sessions.find((x) => x.id === req.session_id);
      if (s && !s.removed) {
        s.removed = true;
        s.hive.emit('SESSION_EVENT', { kind: 'removed', session: s.info() });
      }
      return {};
    };
    h.replies.RESTART_SESSION = (req) => {
      const s = sim.sessions.find((x) => x.id === req.session_id);
      if (!s || s.removed) throw hiveError('restart_failed', 'no such session');
      o.onRestart?.(s); // throws to refuse
      s.reset();
      s.event('updated');
      return { session_id: s.id };
    };
    h.replies.CREATE_SESSION = async (req) => {
      if (o.createError) throw hiveError('acp_mode_above_ceiling', o.createError);
      const s = new SimSession(h, `s${++nSessions}`);
      s.name = (req.name as string) ?? '';
      s.spawned_by = req.workflow_run_id ? `plugin:workflows/workflow:${req.workflow_run_id as string}` : '';
      s.worktree_path = (req.worktree_path as string) ?? '';
      sim.sessions.push(s);
      s.event('added', { phase: 'spawning' });
      if (!o.neverReady) setImmediate(() => s.event('updated'));
      if (held) {
        const p = held;
        held = undefined; // only the first create is held
        await p;
      }
      return { session_id: s.id };
    };
    h.replies.PROMPT_ACP = (req) => {
      const s = sim.sessions.find((x) => x.id === req.session_id) as SimSession;
      // The daemon refuses a prompt while a turn is running (ErrACPBusy).
      if (s.prompt_id > 0 && !s.stop_reason) throw hiveError('acp_busy', 'a turn is running');
      sim.prompts.push(req.text as string);
      const turn = sim.prompts.length;
      const id = s.begin(req.workflow_run_id ? `plugin:workflows/workflow:${req.workflow_run_id as string}` : 'user', req.text as string);
      const run = (): void => o.onPrompt(turn, req.text as string, s);
      if (o.ackFirst === false) run();
      else setImmediate(run);
      if (o.dropPromptAck?.(turn)) throw hiveError('connection_lost', 'hive: connection closed');
      return { prompt_id: id, epoch: s.epoch };
    };
    h.onSend.LIST_SESSIONS = () => h.emit('SESSIONS', { sessions: live().map((s) => s.info()) });
    h.onSend.GET_ACP_TRANSCRIPT = (req) => {
      const s = live().find((x) => x.id === req.session_id);
      if (!s) h.emit('ERROR', { code: 'no_such_session', message: 'that session is not open', session_id: req.session_id });
      else s.snapshot(h); // the reply goes to the connection that asked
    };
  }
  install(hive);
  return sim;
}
