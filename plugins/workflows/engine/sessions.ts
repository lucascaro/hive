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
  /** Whether this message was a whole-transcript snapshot (the answer to GET_ACP_TRANSCRIPT). */
  reset: boolean;
  /** The user turns seen in this epoch, by item id: who sent them and the start of their text (B3c adopts a turn whose ack was lost). */
  users: ReadonlyMap<number, { origin: string; text: string }>;
  /** Ids of the items in this epoch that are not Hive's own `notice` lines: the conversation, as far as the daemon still holds it. */
  content: ReadonlySet<number>;
}

/** How much of a user turn's text is kept: enough to recognise the engine's own re-prompt, not a copy of every prompt. */
const USER_TEXT_KEPT = 512;

/** The fields of the latest SESSION_EVENT for a session that matter here. */
export interface SessionState {
  alive: boolean;
  /** Empty means ready: the daemon omits `phase` once a session is. */
  phase: string;
  last_error?: string;
  /** `acp`, or `pty` while the user has taken the session over (F2). */
  kind: string;
  removed: boolean;
  name?: string;
  /** Who created the session: `plugin:<id>/workflow:<run>` for an engine node. */
  spawned_by?: string;
  worktree_path?: string;
}

/** The slice of the SDK connection the feed listens on. */
export interface Source {
  on?(event: string, fn: (msg: never) => void): unknown;
}

const MAX_ERRORS = 256;
/** Listings still owed an answer that are remembered: a daemon that never answers cannot grow this. */
const MAX_LISTINGS = 64;
/** The codes GET_ACP_TRANSCRIPT is refused with (daemon `sendACPError`). */
const SNAPSHOT_REFUSALS = new Set(['no_such_session', 'not_acp_session', 'acp_failed', 'session_dead']);

export class SessionFeed {
  readonly #tx = new Map<string, TranscriptState>();
  readonly #info = new Map<string, SessionState>();
  readonly #errors = new Map<string, string>();
  readonly #tracked = new Set<string>();
  readonly #subs = new Map<string, Set<() => void>>();
  /** Callers waiting for the next SESSIONS frame, and for a transcript snapshot of one session. */
  /** One entry per LIST_SESSIONS sent, oldest first: each SESSIONS frame answers the oldest, so a poll never reads the answer to an earlier request. */
  #listing: { done: (m: unknown) => void }[] = [];
  readonly #snapshots = new Map<string, (s: TranscriptState | undefined) => void>();

  attach(src: Source): void {
    src.on?.('ACP_TRANSCRIPT', ((m: unknown) => this.#transcript(m)) as (msg: never) => void);
    src.on?.('SESSION_EVENT', ((m: unknown) => this.#session(m)) as (msg: never) => void);
    // CREATE_SESSION settles on its ack; a worktree or adapter failure after that reaches only this listener, carrying the session id.
    src.on?.('ERROR', ((m: unknown) => this.#error(m)) as (msg: never) => void);
    src.on?.('SESSIONS', ((m: unknown) => this.#sessions(m)) as (msg: never) => void);
  }

  /** The connection was lost: wake every waiter so it can see that its launch is stale and stop waiting. */
  pokeAll(): void {
    for (const id of [...this.#subs.keys()]) this.#notify(id);
    for (const [id, done] of [...this.#snapshots]) {
      this.#snapshots.delete(id);
      done(undefined);
    }
    const waiting = this.#listing;
    this.#listing = [];
    for (const w of waiting) w.done(undefined);
  }

  /**
   * The daemon's sessions right now (LIST_SESSIONS, answered by a SESSIONS
   * frame that carries no id, so it is read as an event). Every session in
   * the answer replaces what was known; one that is not in it is gone.
   * Undefined when the daemon does not answer in time.
   */
  async listSessions(send: (name: string, payload?: object) => void, timeoutMs = 15_000): Promise<(SessionState & { id: string })[] | undefined> {
    // A daemon that owes this many answers is not answering: a further request is declined, not sent, so no owed slot is ever dropped.
    if (this.#listing.length >= MAX_LISTINGS) return undefined;
    let entry: { done: (m: unknown) => void } | undefined;
    const reply = new Promise<unknown>((resolve) => {
      // A listing that gave up keeps its slot in the queue: its answer, if it comes, belongs to it and not to the next request.
      const timer = setTimeout(() => resolve(undefined), timeoutMs);
      timer.unref?.();
      entry = {
        done: (m) => {
          clearTimeout(timer);
          resolve(m);
        },
      };
      this.#listing.push(entry);
    });
    try {
      send('LIST_SESSIONS', {});
    } catch {
      if (entry) this.#listing = this.#listing.filter((e) => e !== entry); // never sent: no answer is owed
      return undefined; // the connection is already gone: the caller reads that as "no word from the daemon"
    }
    const m = await reply;
    if (!isObj(m) || !Array.isArray(m.sessions)) return undefined;
    const out: (SessionState & { id: string })[] = [];
    for (const s of m.sessions) if (isObj(s) && typeof s.id === 'string') out.push({ id: s.id, ...sessionState(s) });
    return out;
  }

  /**
   * The whole transcript state of `id` (GET_ACP_TRANSCRIPT, answered by an
   * ACP_TRANSCRIPT snapshot, so it is read as an event). The session must
   * be tracked. Undefined when the daemon refuses or does not answer.
   */
  async fetchTranscript(send: (name: string, payload?: object) => void, id: string, timeoutMs = 15_000): Promise<TranscriptState | undefined> {
    this.track(id);
    const reply = new Promise<TranscriptState | undefined>((resolve) => {
      const timer = setTimeout(() => {
        this.#snapshots.delete(id);
        resolve(undefined);
      }, timeoutMs);
      timer.unref?.();
      this.#snapshots.set(id, (s) => {
        clearTimeout(timer);
        resolve(s);
      });
    });
    try {
      send('GET_ACP_TRANSCRIPT', { session_id: id });
    } catch {
      this.#snapshots.delete(id);
      return undefined;
    }
    return await reply;
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
    const epoch = Number(m.epoch ?? 0);
    // User turns accumulate within an epoch; a snapshot or a new epoch starts the list again.
    const prev = this.#tx.get(m.session_id);
    const users = new Map(m.reset !== true && prev?.epoch === epoch ? prev.users : []);
    const content = new Set(m.reset !== true && prev?.epoch === epoch ? prev.content : []);
    for (const it of Array.isArray(m.items) ? m.items : []) {
      if (isObj(it) && it.kind !== 'notice' && typeof it.id === 'number') content.add(it.id);
      if (isObj(it) && it.kind === 'user' && typeof it.id === 'number') {
        users.set(it.id, { origin: typeof it.origin === 'string' ? it.origin : '', text: typeof it.text === 'string' ? it.text.slice(0, USER_TEXT_KEPT) : '' });
      }
    }
    const state: TranscriptState = {
      reset: m.reset === true,
      users,
      content,
      epoch,
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
    const asked = this.#snapshots.get(m.session_id);
    if (asked && state.reset) {
      this.#snapshots.delete(m.session_id);
      asked(state);
    }
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
      this.#info.set(id, sessionState(s));
    }
    this.#notify(id);
  }

  /** A SESSIONS snapshot: it is the whole list, so whatever it does not name is gone. */
  #sessions(m: unknown): void {
    const answered = this.#listing.shift();
    if (isObj(m) && Array.isArray(m.sessions)) {
      const named = new Set<string>();
      for (const s of m.sessions) {
        if (!isObj(s) || typeof s.id !== 'string') continue;
        named.add(s.id);
        this.#info.set(s.id, sessionState(s));
      }
      const gone = [...this.#info.keys()].filter((id) => !named.has(id));
      for (const id of gone) this.#info.delete(id);
      // A session the list no longer has is told to its waiters too: they must see it is gone.
      for (const id of [...named, ...gone]) this.#notify(id);
    }
    answered?.done(m);
  }

  #error(m: unknown): void {
    if (!isObj(m) || typeof m.session_id !== 'string' || typeof m.message !== 'string') return;
    // Only an error that says the transcript cannot be had answers a read of it; any other error on the session is not its answer.
    const asked = SNAPSHOT_REFUSALS.has(String(m.code)) ? this.#snapshots.get(m.session_id) : undefined;
    if (asked) {
      this.#snapshots.delete(m.session_id);
      asked(undefined);
    }
    if (this.#errors.size >= MAX_ERRORS) this.#errors.delete(this.#errors.keys().next().value as string);
    this.#errors.set(m.session_id, m.message);
    this.#notify(m.session_id);
  }
}

function sessionState(s: Data): SessionState {
  return {
    alive: s.alive === true,
    phase: typeof s.phase === 'string' ? s.phase : '',
    ...(typeof s.last_error === 'string' && s.last_error !== '' ? { last_error: s.last_error } : {}),
    kind: typeof s.kind === 'string' && s.kind !== '' ? s.kind : 'pty',
    removed: false,
    ...(typeof s.name === 'string' ? { name: s.name } : {}),
    ...(typeof s.spawned_by === 'string' ? { spawned_by: s.spawned_by } : {}),
    ...(typeof s.worktree_path === 'string' ? { worktree_path: s.worktree_path } : {}),
  };
}
