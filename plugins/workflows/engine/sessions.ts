// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// What the daemon says about sessions, kept as the latest state per session
// (spec 529, Agent nodes). Frames arrive in any order relative to a request's
// ack: a fast turn can end before its PROMPT_ACP ack is read, and a session can
// be ready before CREATE_SESSION's. So nothing here is a one-shot event: the
// last message is kept, and a waiter checks it when it starts and again on each
// change. Every ACP_TRANSCRIPT message carries the whole state of the latest
// prompt, so the latest one is all there is to keep.

type Data = Record<string, unknown>;
const isObj = (v: unknown): v is Data => typeof v === 'object' && v !== null && !Array.isArray(v);

export interface PermissionState {
  request_id: string;
  title?: string;
  options: { option_id: string; name: string; kind: string }[];
}

/** The state fields of the latest ACP_TRANSCRIPT message for a session (items are not kept). */
export interface TranscriptState {
  epoch: number;
  prompt_id: number;
  permission?: PermissionState;
  result_status: string;
  result?: unknown;
  stop_reason: string;
}

/** The fields of the latest SESSION_EVENT for a session that matter here. */
export interface SessionState {
  alive: boolean;
  /** Empty means ready: the daemon omits `phase` once a session is. */
  phase: string;
  last_error?: string;
  /** `acp`, or `pty` while the user has taken the session over (F2). */
  kind: string;
  removed: boolean;
}

/** The slice of the SDK connection the feed listens on. */
export interface Source {
  on?(event: string, fn: (msg: never) => void): unknown;
}

const MAX_ERRORS = 256;

export class SessionFeed {
  readonly #tx = new Map<string, TranscriptState>();
  readonly #info = new Map<string, SessionState>();
  readonly #errors = new Map<string, string>();
  readonly #tracked = new Set<string>();
  readonly #subs = new Map<string, Set<() => void>>();

  attach(src: Source): void {
    src.on?.('ACP_TRANSCRIPT', ((m: unknown) => this.#transcript(m)) as (msg: never) => void);
    src.on?.('SESSION_EVENT', ((m: unknown) => this.#session(m)) as (msg: never) => void);
    // CREATE_SESSION settles on its ack; a worktree or adapter failure after that reaches only this listener, carrying the session id.
    src.on?.('ERROR', ((m: unknown) => this.#error(m)) as (msg: never) => void);
  }

  /** Start keeping transcript state for `id`. Call before sending its first prompt. */
  track(id: string): void {
    this.#tracked.add(id);
  }

  forget(id: string): void {
    this.#tracked.delete(id);
    this.#tx.delete(id);
    this.#errors.delete(id);
    if (this.#info.get(id)?.removed) this.#info.delete(id);
  }

  transcript(id: string): TranscriptState | undefined {
    return this.#tx.get(id);
  }

  session(id: string): SessionState | undefined {
    return this.#info.get(id);
  }

  error(id: string): string | undefined {
    return this.#errors.get(id);
  }

  /** Calls `fn` on every change to `id`; returns the unsubscribe. */
  subscribe(id: string, fn: () => void): () => void {
    let set = this.#subs.get(id);
    if (!set) this.#subs.set(id, (set = new Set()));
    set.add(fn);
    return () => {
      set.delete(fn);
      if (set.size === 0 && this.#subs.get(id) === set) this.#subs.delete(id);
    };
  }

  #notify(id: string): void {
    for (const fn of [...(this.#subs.get(id) ?? [])]) fn();
  }

  #transcript(m: unknown): void {
    if (!isObj(m) || typeof m.session_id !== 'string' || !this.#tracked.has(m.session_id)) return;
    const p = isObj(m.permission) ? m.permission : undefined;
    const options = p && Array.isArray(p.options) ? p.options.filter(isObj) : [];
    const state: TranscriptState = {
      epoch: Number(m.epoch ?? 0),
      prompt_id: Number(m.prompt_id ?? 0),
      result_status: typeof m.result_status === 'string' ? m.result_status : '',
      ...(m.result === undefined ? {} : { result: m.result }),
      stop_reason: typeof m.stop_reason === 'string' ? m.stop_reason : '',
      ...(p
        ? {
            permission: {
              request_id: String(p.request_id ?? ''),
              ...(typeof p.title === 'string' ? { title: p.title } : {}),
              options: options.map((o) => ({ option_id: String(o.option_id ?? ''), name: String(o.name ?? ''), kind: String(o.kind ?? '') })),
            },
          }
        : {}),
    };
    this.#tx.set(m.session_id, state);
    this.#notify(m.session_id);
  }

  #session(m: unknown): void {
    if (!isObj(m) || !isObj(m.session) || typeof m.session.id !== 'string') return;
    const s = m.session;
    const id = s.id as string;
    if (m.kind === 'removed') {
      // A session nobody waits on is forgotten; one being waited on must see that it is gone.
      if (this.#tracked.has(id) || this.#subs.has(id)) this.#info.set(id, { alive: false, phase: '', kind: 'acp', removed: true });
      else this.#info.delete(id);
    } else {
      this.#info.set(id, {
        alive: s.alive === true,
        phase: typeof s.phase === 'string' ? s.phase : '',
        ...(typeof s.last_error === 'string' && s.last_error !== '' ? { last_error: s.last_error } : {}),
        kind: typeof s.kind === 'string' && s.kind !== '' ? s.kind : 'pty',
        removed: false,
      });
    }
    this.#notify(id);
  }

  #error(m: unknown): void {
    if (!isObj(m) || typeof m.session_id !== 'string' || typeof m.message !== 'string') return;
    if (this.#errors.size >= MAX_ERRORS) this.#errors.delete(this.#errors.keys().next().value as string);
    this.#errors.set(m.session_id, m.message);
    this.#notify(m.session_id);
  }
}
