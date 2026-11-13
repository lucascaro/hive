// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The checkpoint store (spec 529, Store): `runs.db`, written with Node's
// built-in `node:sqlite`. Every transition is one BEGIN IMMEDIATE ... COMMIT
// that updates the node rows and appends the events that describe them (B-12),
// so a read of node state and `last_seq` always agree.
//
// Raw outputs live in `nodes.output_raw`, which only the reducer reads (B-5).
// No read method here returns it except `loadRows`, which feeds the reducer.

import { randomBytes } from 'node:crypto';
import { chmodSync, closeSync, mkdirSync, openSync } from 'node:fs';
import { DatabaseSync } from 'node:sqlite';
import { join } from 'node:path';
import { type EventDraft, type Handles, prepareEvent, type RunEvent } from './events.ts';
import type { Change, Row, RunRow, RunState } from './model.ts';

const SCHEMA_VERSION = 1;

const SCHEMA = `
CREATE TABLE IF NOT EXISTS meta (schema_version INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS runs (
  run_id TEXT PRIMARY KEY, project_id TEXT NOT NULL, workflow TEXT NOT NULL, file TEXT NOT NULL,
  state TEXT NOT NULL, source_hash TEXT NOT NULL, ir TEXT NOT NULL, input TEXT NOT NULL,
  base_sha TEXT NOT NULL DEFAULT '', nonce TEXT NOT NULL, boot_id TEXT NOT NULL DEFAULT '',
  kept_worktrees TEXT NOT NULL DEFAULT '[]', error TEXT, last_seq INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS runs_by_state ON runs (state, created_at);
CREATE INDEX IF NOT EXISTS runs_by_project ON runs (project_id, created_at);
CREATE TABLE IF NOT EXISTS nodes (
  run_id TEXT NOT NULL, key TEXT NOT NULL, node_id TEXT NOT NULL, state TEXT NOT NULL,
  attempt INTEGER NOT NULL DEFAULT 0, ready INTEGER NOT NULL DEFAULT 0, progress INTEGER NOT NULL DEFAULT 0,
  turns INTEGER NOT NULL DEFAULT 0, output_raw TEXT, output TEXT, error TEXT, cap TEXT,
  session_id TEXT, branch TEXT, worktree TEXT, pid INTEGER, pgid INTEGER, proc_start TEXT,
  prompt_id INTEGER, epoch INTEGER, boot_id TEXT, conflict TEXT, waiting TEXT,
  started_at TEXT, ended_at TEXT,
  PRIMARY KEY (run_id, key)
);
CREATE TABLE IF NOT EXISTS events (
  run_id TEXT NOT NULL, seq INTEGER NOT NULL, ts TEXT NOT NULL, key TEXT, node_id TEXT,
  attempt INTEGER NOT NULL DEFAULT 0, type TEXT NOT NULL, data TEXT NOT NULL,
  PRIMARY KEY (run_id, seq)
);
CREATE INDEX IF NOT EXISTS events_by_key ON events (run_id, key, seq);
CREATE TABLE IF NOT EXISTS handles (
  run_id TEXT NOT NULL, handle TEXT NOT NULL, real_id TEXT NOT NULL, PRIMARY KEY (run_id, handle)
);
`;

type Sql = string | number | null;
type Rec = Record<string, Sql>;

const json = (v: unknown): string | null => (v === undefined ? null : JSON.stringify(v));
const parse = (s: unknown): unknown => (typeof s === 'string' ? JSON.parse(s) : undefined);
const opt = <T>(v: Sql | undefined): T | undefined => (v === null || v === undefined ? undefined : (v as T));

function rowToDb(runId: string, r: Row): Record<string, Sql> {
  return {
    run_id: runId,
    key: r.key,
    node_id: r.node_id,
    state: r.state,
    attempt: r.attempt,
    ready: r.ready ? 1 : 0,
    progress: r.progress,
    turns: r.turns,
    output_raw: json(r.output_raw),
    output: json(r.output),
    error: r.error ?? null,
    cap: r.cap ?? null,
    session_id: r.session_id ?? null,
    branch: r.branch ?? null,
    worktree: r.worktree ?? null,
    pid: r.pid ?? null,
    pgid: r.pgid ?? null,
    proc_start: r.proc_start ?? null,
    prompt_id: r.prompt_id ?? null,
    epoch: r.epoch ?? null,
    boot_id: r.boot_id ?? null,
    conflict: json(r.conflict),
    waiting: json(r.waiting),
    started_at: r.started_at ?? null,
    ended_at: r.ended_at ?? null,
  };
}

function dbToRow(d: Rec): Row {
  const row: Row = {
    key: d.key as string,
    node_id: d.node_id as string,
    state: d.state as Row['state'],
    attempt: d.attempt as number,
    ready: d.ready === 1,
    progress: d.progress as number,
    turns: d.turns as number,
  };
  const set = <K extends keyof Row>(k: K, v: Row[K] | undefined): void => {
    if (v !== undefined) row[k] = v;
  };
  set('output_raw', parse(d.output_raw));
  set('output', parse(d.output));
  set('error', opt(d.error));
  set('cap', opt(d.cap));
  set('session_id', opt(d.session_id));
  set('branch', opt(d.branch));
  set('worktree', opt(d.worktree));
  set('pid', opt(d.pid));
  set('pgid', opt(d.pgid));
  set('proc_start', opt(d.proc_start));
  set('prompt_id', opt(d.prompt_id));
  set('epoch', opt(d.epoch));
  set('boot_id', opt(d.boot_id));
  set('conflict', parse(d.conflict) as Row['conflict']);
  set('waiting', parse(d.waiting) as Row['waiting']);
  set('started_at', opt(d.started_at));
  set('ended_at', opt(d.ended_at));
  return row;
}

function dbToRun(d: Rec): RunRow {
  return {
    run_id: d.run_id as string,
    project_id: d.project_id as string,
    workflow: d.workflow as string,
    file: d.file as string,
    state: d.state as RunState,
    source_hash: d.source_hash as string,
    ir: d.ir as string,
    input: d.input as string,
    base_sha: d.base_sha as string,
    nonce: d.nonce as string,
    boot_id: d.boot_id as string,
    kept_worktrees: JSON.parse(d.kept_worktrees as string) as string[],
    ...(d.error === null ? {} : { error: d.error as string }),
    last_seq: d.last_seq as number,
    created_at: d.created_at as string,
    updated_at: d.updated_at as string,
  };
}

export interface NewRun {
  run_id: string;
  project_id: string;
  workflow: string;
  file: string;
  source_hash: string;
  ir: string;
  input: string;
  boot_id?: string;
}

export interface StoreOptions {
  /** Test seam: called inside the commit transaction after the node rows are written and before the events are. */
  afterRows?: () => void;
  now?: () => Date;
}

export class Store {
  readonly #db: DatabaseSync;
  readonly #afterRows: (() => void) | undefined;
  readonly #now: () => Date;
  /** Events waiting for the next flush, per run: agent.* and check.output (Write batching). */
  readonly #buffered = new Map<string, EventDraft[]>();

  constructor(dir: string, opts: StoreOptions = {}) {
    mkdirSync(dir, { recursive: true, mode: 0o700 });
    const path = join(dir, 'runs.db');
    // There is deliberately no global umask: children inherit it. The files are
    // created here at 0600, the WAL and shm too, before SQLite makes them.
    for (const p of [path, `${path}-wal`, `${path}-shm`]) {
      closeSync(openSync(p, 'a', 0o600));
      chmodSync(p, 0o600);
    }
    this.#db = new DatabaseSync(path);
    this.#db.exec('PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON;');
    this.#db.exec(SCHEMA);
    const v = this.#db.prepare('SELECT schema_version AS v FROM meta').get() as { v: number } | undefined;
    if (v === undefined) this.#db.prepare('INSERT INTO meta (schema_version) VALUES (?)').run(SCHEMA_VERSION);
    else if (v.v > SCHEMA_VERSION) throw new Error(`runs.db is schema ${v.v}; this engine understands ${SCHEMA_VERSION}`);
    this.#afterRows = opts.afterRows;
    this.#now = opts.now ?? (() => new Date());
  }

  close(): void {
    this.#db.close();
  }

  #iso(): string {
    return this.#now().toISOString();
  }

  #tx<T>(fn: () => T): T {
    this.#db.exec('BEGIN IMMEDIATE');
    try {
      const out = fn();
      this.#db.exec('COMMIT');
      return out;
    } catch (err) {
      this.#db.exec('ROLLBACK');
      throw err;
    }
  }

  // ------------------------------------------------------------ writes

  #insertRow(runId: string, r: Row): void {
    const d = rowToDb(runId, r);
    const cols = Object.keys(d);
    this.#db
      .prepare(`INSERT OR REPLACE INTO nodes (${cols.join(',')}) VALUES (${cols.map(() => '?').join(',')})`)
      .run(...cols.map((c) => d[c]));
  }

  #appendEvents(runId: string, drafts: readonly EventDraft[]): RunEvent[] {
    const last = (this.#db.prepare('SELECT last_seq AS s FROM runs WHERE run_id = ?').get(runId) as { s: number }).s;
    const ts = this.#iso();
    const out: RunEvent[] = [];
    const ins = this.#db.prepare('INSERT INTO events (run_id, seq, ts, key, node_id, attempt, type, data) VALUES (?,?,?,?,?,?,?,?)');
    const handles = this.handlesFor(runId);
    for (const draft of drafts) {
      const d = prepareEvent(draft, handles);
      const ev: RunEvent = { run_id: runId, seq: last + out.length + 1, ts, ...d } as RunEvent;
      ins.run(runId, ev.seq, ts, ev.key, ev.node_id, ev.attempt, ev.type, JSON.stringify(ev.data));
      out.push(ev);
    }
    return out;
  }

  /** Inserts a run, its rows and its first events in one transaction. */
  createRun(run: NewRun, rows: readonly Row[], events: readonly EventDraft[]): RunEvent[] {
    const now = this.#iso();
    return this.#tx(() => {
      this.#db
        .prepare(
          `INSERT INTO runs (run_id, project_id, workflow, file, state, source_hash, ir, input, nonce, boot_id, created_at, updated_at)
           VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
        )
        .run(run.run_id, run.project_id, run.workflow, run.file, 'running', run.source_hash, run.ir, run.input, randomBytes(16).toString('hex'), run.boot_id ?? '', now, now);
      for (const r of rows) this.#insertRow(run.run_id, r);
      this.#afterRows?.();
      const evs = this.#appendEvents(run.run_id, events);
      this.#db.prepare('UPDATE runs SET last_seq = ? WHERE run_id = ?').run(evs.length, run.run_id);
      return evs;
    });
  }

  /**
   * One transition: the rows, the run row and the events, atomically. Events
   * buffered since the last flush take their seq first, so a node's output
   * lines come before the `node.finished` that follows them. Returns the
   * events as stored.
   */
  commit(runId: string, change: Change): RunEvent[] {
    const pending = this.#buffered.get(runId) ?? [];
    const evs = this.#tx(() => {
      for (const r of change.rows) this.#insertRow(runId, r);
      this.#afterRows?.();
      const stored = this.#appendEvents(runId, [...pending, ...change.events]);
      const sets = ['last_seq = ?', 'updated_at = ?'];
      const args: Sql[] = [stored.length === 0 ? this.lastSeq(runId) : stored[stored.length - 1].seq, this.#iso()];
      const run = change.run;
      if (run?.state !== undefined) (sets.push('state = ?'), args.push(run.state));
      if (run && 'error' in run) (sets.push('error = ?'), args.push(run.error ?? null));
      if (run?.base_sha !== undefined) (sets.push('base_sha = ?'), args.push(run.base_sha));
      if (run?.kept_worktrees !== undefined) (sets.push('kept_worktrees = ?'), args.push(JSON.stringify(run.kept_worktrees)));
      this.#db.prepare(`UPDATE runs SET ${sets.join(', ')} WHERE run_id = ?`).run(...args, runId);
      return stored;
    });
    this.#buffered.delete(runId);
    return evs;
  }

  /** Holds an agent.* or check.output event for the next flush. It takes its seq at commit. */
  buffer(runId: string, draft: EventDraft): void {
    const list = this.#buffered.get(runId) ?? [];
    list.push(draft);
    this.#buffered.set(runId, list);
  }

  /** Commits every buffered event, one transaction per run. Returns what was stored. */
  flush(): RunEvent[] {
    const out: RunEvent[] = [];
    for (const runId of [...this.#buffered.keys()]) out.push(...this.commit(runId, { rows: [], events: [] }));
    return out;
  }

  // ---- permission-id handles (B-16)

  /** The handles of one run: a stand-in for each permission id that cannot pass through unredacted, minted once and kept. */
  handlesFor(runId: string): Handles {
    return {
      handleFor: (realId) => {
        const existing = this.#db.prepare('SELECT handle AS h FROM handles WHERE run_id = ? AND real_id = ?').get(runId, realId) as { h: string } | undefined;
        if (existing) return existing.h;
        const handle = `h-${randomBytes(12).toString('hex')}`;
        this.#db.prepare('INSERT INTO handles (run_id, handle, real_id) VALUES (?,?,?)').run(runId, handle, realId);
        return handle;
      },
    };
  }

  /** The real permission id a handle stands for, or the input when it is not a handle. */
  realIdFor(runId: string, id: string): string {
    const r = this.#db.prepare('SELECT real_id AS r FROM handles WHERE run_id = ? AND handle = ?').get(runId, id) as { r: string } | undefined;
    return r?.r ?? id;
  }

  // ------------------------------------------------------------- reads

  lastSeq(runId: string): number {
    return (this.#db.prepare('SELECT last_seq AS s FROM runs WHERE run_id = ?').get(runId) as { s: number } | undefined)?.s ?? 0;
  }

  getRun(runId: string): RunRow | undefined {
    const d = this.#db.prepare('SELECT * FROM runs WHERE run_id = ?').get(runId) as Rec | undefined;
    return d && dbToRun(d);
  }

  listRuns(q: { project_id?: string; state?: string; before?: string; before_id?: string; limit: number }): RunRow[] {
    const where: string[] = [];
    const args: Sql[] = [];
    if (q.project_id !== undefined) (where.push('project_id = ?'), args.push(q.project_id));
    if (q.state !== undefined) (where.push('state = ?'), args.push(q.state));
    if (q.before !== undefined) {
      // The sort is (created_at, run_id), so the cursor is too: runs created in the same millisecond are not skipped between pages.
      if (q.before_id === undefined) (where.push('created_at < ?'), args.push(q.before));
      else (where.push('(created_at < ? OR (created_at = ? AND run_id < ?))'), args.push(q.before, q.before, q.before_id));
    }
    const sql = `SELECT * FROM runs ${where.length ? `WHERE ${where.join(' AND ')}` : ''} ORDER BY created_at DESC, run_id DESC LIMIT ?`;
    return (this.#db.prepare(sql).all(...args, q.limit) as Rec[]).map(dbToRun);
  }

  /** Runs the engine still has work for. */
  activeRuns(): RunRow[] {
    return (this.#db.prepare("SELECT * FROM runs WHERE state IN ('running','waiting') ORDER BY created_at").all() as Rec[]).map(dbToRun);
  }

  /** Every row of a run, raw outputs included: for the reducer only. */
  loadRows(runId: string): Row[] {
    return (this.#db.prepare('SELECT * FROM nodes WHERE run_id = ? ORDER BY key').all(runId) as Rec[]).map(dbToRow);
  }

  /** One row, with its raw output left out: what the topics serve. */
  getNode(runId: string, key: string): Row | undefined {
    const d = this.#db.prepare('SELECT * FROM nodes WHERE run_id = ? AND key = ?').get(runId, key) as Rec | undefined;
    if (!d) return undefined;
    const { output_raw: _raw, ...rest } = dbToRow(d);
    return rest;
  }

  /** Slim node rows after `after`, in key order, with the `last_seq` they reflect. */
  nodePage(runId: string, after: string | undefined, limit: number): { rows: SlimRow[]; last_seq: number } {
    // One read transaction: the rows and the last_seq they reflect come from the same snapshot (B-12).
    this.#db.exec('BEGIN');
    let last_seq: number;
    let rows: Rec[];
    try {
      last_seq = this.lastSeq(runId);
      rows = this.#db
        .prepare('SELECT key, node_id, state, attempt, started_at, ended_at, cap FROM nodes WHERE run_id = ? AND key > ? ORDER BY key LIMIT ?')
        .all(runId, after ?? '', limit) as Rec[];
    } finally {
      this.#db.exec('COMMIT');
    }
    return {
      last_seq,
      rows: rows.map((d) => ({
        key: d.key as string,
        node_id: d.node_id as string,
        state: d.state as Row['state'],
        attempt: d.attempt as number,
        ...(d.started_at === null ? {} : { started_at: d.started_at as string }),
        ...(d.ended_at === null ? {} : { finished_at: d.ended_at as string }),
        ...(d.cap === null ? {} : { cap: d.cap as string }),
      })),
    };
  }

  hasNodeAfter(runId: string, key: string): boolean {
    return this.#db.prepare('SELECT 1 AS x FROM nodes WHERE run_id = ? AND key > ? LIMIT 1').get(runId, key) !== undefined;
  }

  events(runId: string, afterSeq: number, limit: number, key?: string): RunEvent[] {
    const sql = `SELECT * FROM events WHERE run_id = ? AND seq > ? ${key === undefined ? '' : 'AND key = ?'} ORDER BY seq LIMIT ?`;
    const args: Sql[] = key === undefined ? [runId, afterSeq, limit] : [runId, afterSeq, key, limit];
    return (this.#db.prepare(sql).all(...args) as Rec[]).map(dbToEvent);
  }

  hasEventAfter(runId: string, seq: number, key?: string): boolean {
    const sql = `SELECT 1 AS x FROM events WHERE run_id = ? AND seq > ? ${key === undefined ? '' : 'AND key = ?'} LIMIT 1`;
    return this.#db.prepare(sql).get(...(key === undefined ? [runId, seq] : [runId, seq, key])) !== undefined;
  }
}

export interface SlimRow {
  key: string;
  node_id: string;
  state: Row['state'];
  attempt: number;
  started_at?: string;
  finished_at?: string;
  cap?: string;
}

function dbToEvent(d: Rec): RunEvent {
  return {
    run_id: d.run_id as string,
    seq: d.seq as number,
    ts: d.ts as string,
    key: d.key as string | null,
    node_id: d.node_id as string | null,
    attempt: d.attempt as number,
    type: d.type as RunEvent['type'],
    data: JSON.parse(d.data as string),
  };
}
