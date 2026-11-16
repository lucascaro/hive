// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// A scripted daemon for agent nodes (spec 529): it answers the requests an
// agent node makes and plays the frames the daemon would send back, in the
// order a test chooses, including the awkward ones (a turn that ends before its
// ack is read). Nothing here is the engine; it is only what the engine talks to.

import type { FakeHive } from './harness.ts';

export const CLAUDE = { id: 'claude', name: 'Claude', modes: [{ id: 'plan', label: 'Plan' }, { id: 'default', label: 'Default' }, { id: 'acceptEdits', label: 'Accept edits' }], default_mode: 'plan', ceiling: 'default', available: true };

type Data = Record<string, unknown>;

export interface Permission {
  request_id: string;
  title?: string;
  options: { option_id: string; name: string; kind: string }[];
}

/** One session: the state every ACP_TRANSCRIPT message of it carries. */
export class SimSession {
  readonly id: string;
  readonly #hive: FakeHive;
  epoch = 1;
  prompt_id = 0;
  permission: Permission | undefined;
  result_status = '';
  result: unknown;
  stop_reason = '';

  constructor(hive: FakeHive, id: string) {
    this.#hive = hive;
    this.id = id;
  }

  /** A session event: ready is `alive` with no phase. */
  event(kind: 'added' | 'updated' | 'removed', fields: Data = {}): void {
    this.#hive.emit('SESSION_EVENT', { kind, session: { id: this.id, alive: true, kind: 'acp', ...fields } });
  }

  /** Starts a new prompt: everything of the last one is cleared. */
  begin(): number {
    this.prompt_id += 10;
    this.permission = undefined;
    this.result_status = '';
    this.result = undefined;
    this.stop_reason = '';
    this.send();
    return this.prompt_id;
  }

  send(): void {
    this.#hive.emit('ACP_TRANSCRIPT', {
      session_id: this.id,
      epoch: this.epoch,
      prompt_id: this.prompt_id,
      ...(this.permission ? { permission: this.permission } : {}),
      ...(this.result_status ? { result_status: this.result_status } : {}),
      ...(this.result === undefined ? {} : { result: this.result }),
      ...(this.stop_reason ? { stop_reason: this.stop_reason } : {}),
    });
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
}

export interface Sim {
  sessions: SimSession[];
  prompts: string[];
  /** Requests of one verb, in order. */
  of(name: string): Data[];
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
}

export function simulate(hive: FakeHive, o: SimOptions): Sim {
  const sim: Sim = { sessions: [], prompts: [], of: (name) => hive.requests.filter((r) => r.name === name).map((r) => r.payload) };
  let nSessions = 0;
  let worktree = 0;
  hive.replies.LIST_ACP_AGENTS = () => ({ agents: o.agents ?? [CLAUDE] });
  hive.replies.PREPARE_WORKTREE = (req) => {
    const branch = req.branch as string;
    return { path: `/wt/${branch.split('/').slice(3).join('_') || 'x'}-${worktree++}`, branch, base_sha: req.base === 'HEAD' ? 'sha-1' : undefined, head_sha: 'sha-1', reattached: false, merged: [] };
  };
  hive.replies.COMMIT_WORKTREE = () => ({ committed: true, head: 'sha-2' });
  hive.replies.CANCEL_ACP = () => ({ was_busy: true });
  hive.replies.CREATE_SESSION = () => {
    if (o.createError) throw Object.assign(new Error(o.createError), { hive: { code: 'acp_mode_above_ceiling', message: o.createError } });
    const s = new SimSession(hive, `s${++nSessions}`);
    sim.sessions.push(s);
    s.event('added', { phase: 'spawning' });
    if (!o.neverReady) setImmediate(() => s.event('updated'));
    return { session_id: s.id };
  };
  hive.replies.PROMPT_ACP = (req) => {
    const s = sim.sessions.find((x) => x.id === req.session_id) as SimSession;
    sim.prompts.push(req.text as string);
    const id = s.begin();
    const turn = sim.prompts.length;
    const run = (): void => o.onPrompt(turn, req.text as string, s);
    if (o.ackFirst === false) run();
    else setImmediate(run);
    return { prompt_id: id, epoch: s.epoch };
  };
  return sim;
}
