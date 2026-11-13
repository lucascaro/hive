// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The plugin-message topics a GUI talks to the engine on (spec 529, Contract
// for C). A request carries `data.nonce`; the reply goes back on
// `<topic>.reply` as `{nonce, ok, error?, ...}`. Every reply is at most
// 48 KiB as the daemon will send it, by construction: lists are paged by
// encoded size, never by count alone.

import { type Engine, EngineError } from './engine.ts';
import { prepareEvent, type RunEvent } from './events.ts';
import type { Row, RunRow } from './model.ts';
import { redactString, redactValue } from './redact.ts';
import { encodedSize, MAX_MESSAGE_BYTES, truncateEncoded } from './size.ts';
import type { Store } from './store.ts';

export const PLUGIN_ID = 'workflows';
const MAX_LIST = 50;
const MAX_EVENTS = 500;
const NODE_PAGE_ROWS = 1000;
const DEFAULT_OUTPUT_LIMIT = 16 << 10;
const MAX_KEPT_LISTED = 16;
const MAX_KEPT_PATH_BYTES = 256;
const RUN_STATES = ['running', 'waiting', 'done', 'failed', 'cancelled'];

type Data = Record<string, unknown>;

export class TopicError extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

export interface TopicsOptions {
  engine: Engine;
  /** Whether the current source of a run's workflow differs from the one it ran (B4 computes it; until then never). */
  sourceChanged?: (run: RunRow) => boolean;
  /** Turns on `test.start-run`, the seam that starts a run from an IR until `run.start` exists (B4). Never set in production. */
  testSeam?: boolean;
}

export interface Reply {
  topic: string;
  data: Data;
}

/** The bytes a reply occupies on the wire: the whole PLUGIN_MESSAGE, not just its data. */
const wireSize = (topic: string, data: unknown): number => encodedSize({ plugin_id: PLUGIN_ID, topic, data });

const isObj = (v: unknown): v is Data => typeof v === 'object' && v !== null && !Array.isArray(v);
const str = (d: Data, k: string): string => {
  if (typeof d[k] !== 'string' || d[k] === '') throw new TopicError('invalid_request', `${k} must be a string`);
  return d[k] as string;
};
const optStr = (d: Data, k: string): string | undefined => (d[k] === undefined ? undefined : str(d, k));
const optInt = (d: Data, k: string, lo: number, hi: number, dflt: number): number => {
  const v = d[k];
  if (v === undefined) return dflt;
  if (typeof v !== 'number' || !Number.isInteger(v) || v < lo || v > hi) throw new TopicError('invalid_request', `${k} must be an integer in ${lo}..${hi}`);
  return v;
};

export class Topics {
  readonly #engine: Engine;
  readonly #store: Store;
  readonly #opts: TopicsOptions;
  readonly #fns: Record<string, (d: Data) => Data>;

  constructor(opts: TopicsOptions) {
    this.#opts = opts;
    this.#engine = opts.engine;
    this.#store = opts.engine.store;
    this.#fns = this.#table();
  }

  /** Answers one request, or undefined for a topic that is not the engine's. */
  handle(topic: string, data: unknown): Reply | undefined {
    const fn = Object.hasOwn(this.#fns, topic) ? this.#fns[topic] : undefined;
    if (!fn) return undefined;
    const req: Data = isObj(data) ? data : {};
    const nonce = typeof req.nonce === 'string' ? req.nonce : undefined;
    const reply = `${topic}.reply`;
    try {
      const out = fn(req);
      const body: Data = { nonce, ok: true, ...out };
      if (wireSize(reply, body) > MAX_MESSAGE_BYTES) throw new TopicError('reply_too_large', `the reply to ${topic} does not fit one message`);
      return { topic: reply, data: body };
    } catch (err) {
      if (err instanceof TopicError || err instanceof EngineError) return { topic: reply, data: { nonce, ok: false, error: { code: err.code, message: err.message } } };
      return { topic: reply, data: { nonce, ok: false, error: { code: 'internal', message: err instanceof Error ? err.message : String(err) } } };
    }
  }

  #table(): Record<string, (d: Data) => Data> {
    const t: Record<string, (d: Data) => Data> = {
      'runs.list': (d) => this.#runsList(d),
      'run.get': (d) => this.#runGet(d),
      'run.nodes': (d) => this.#runNodes(d),
      'node.get': (d) => this.#nodeGet(d),
      'run.events': (d) => this.#runEvents(d),
      'run.cancel': (d) => (this.#engine.cancelRun(str(d, 'run_id')), {}),
      'run.retry': (d) => (this.#engine.retryRun(str(d, 'run_id')), {}),
      'human.answer': (d) => (this.#engine.answerHuman(str(d, 'run_id'), str(d, 'key'), d.answer), {}),
    };
    if (this.#opts.testSeam) {
      // The engine's own pid, so a test can kill the plugin the way a crash would.
      t['test.pid'] = () => ({ pid: process.pid });
      t['test.start-run'] = (d) => ({
        run_id: this.#engine.startRunFromIR({ project_id: str(d, 'project_id'), file: optStr(d, 'file') ?? 'test.ts', ir: d.ir, input: isObj(d.input) ? d.input : {}, ...(optStr(d, 'source_hash') ? { source_hash: optStr(d, 'source_hash') as string } : {}) }),
      });
    }
    return t;
  }

  #run(d: Data): RunRow {
    const run = this.#store.getRun(str(d, 'run_id'));
    if (!run) throw new TopicError('no_such_run', `no run ${d.run_id}`);
    return run;
  }

  #summary(r: RunRow): Data {
    return {
      run_id: r.run_id,
      project_id: r.project_id,
      workflow: r.workflow,
      file: r.file,
      state: r.state,
      created_at: r.created_at,
      updated_at: r.updated_at,
      source_hash: r.source_hash,
      source_changed: this.#opts.sourceChanged?.(r) ?? false,
    };
  }

  #runsList(d: Data): Data {
    const state = optStr(d, 'state');
    if (state !== undefined && !RUN_STATES.includes(state)) throw new TopicError('invalid_request', `state must be one of ${RUN_STATES.join('|')}`);
    const runs = this.#store.listRuns({
      ...(optStr(d, 'project_id') === undefined ? {} : { project_id: optStr(d, 'project_id') as string }),
      ...(state === undefined ? {} : { state }),
      ...(optStr(d, 'before') === undefined ? {} : { before: optStr(d, 'before') as string }),
      limit: optInt(d, 'limit', 1, MAX_LIST, MAX_LIST),
    });
    // Fifty summaries are well under the cap, but the rule is the same everywhere: trim by encoded size.
    const out: Data[] = [];
    let size = wireSize('runs.list.reply', { nonce: d.nonce, ok: true, runs: [] });
    for (const r of runs) {
      const s = this.#summary(r);
      size += encodedSize(s) + 1;
      if (out.length > 0 && size > MAX_MESSAGE_BYTES) break;
      out.push(s);
    }
    return { runs: out };
  }

  #runGet(d: Data): Data {
    const r = this.#run(d);
    // `ir` is at most 32 KiB and `input` 8 KiB, so everything else is held to 6 KiB (B-6).
    const kept = r.kept_worktrees.slice(0, MAX_KEPT_LISTED).map((p) => truncateEncoded(p, MAX_KEPT_PATH_BYTES).text);
    return {
      run: {
        ...this.#summary(r),
        last_seq: r.last_seq,
        input: redactValue(JSON.parse(r.input)).value,
        base_sha: r.base_sha,
        kept_worktrees: kept,
        kept_worktrees_total: r.kept_worktrees.length,
        error: r.error === undefined ? undefined : truncateEncoded(redactString(r.error).text, 2 << 10).text,
      },
      ir: JSON.parse(r.ir),
    };
  }

  #runNodes(d: Data): Data {
    const r = this.#run(d);
    const after = optStr(d, 'after');
    const page = this.#store.nodePage(r.run_id, after, NODE_PAGE_ROWS);
    const nodes: Data[] = [];
    let size = wireSize('run.nodes.reply', { nonce: d.nonce, ok: true, nodes: [], last_seq: page.last_seq, more: true, next_after: 'x'.repeat(512) });
    for (const row of page.rows) {
      const n = encodedSize(row) + 1;
      if (nodes.length > 0 && size + n > MAX_MESSAGE_BYTES) break;
      size += n;
      nodes.push({ ...row });
    }
    const last = nodes.length > 0 ? (nodes[nodes.length - 1].key as string) : undefined;
    const more = last !== undefined && this.#store.hasNodeAfter(r.run_id, last);
    return { nodes, last_seq: page.last_seq, ...(more ? { more: true, next_after: last } : {}) };
  }

  #runEvents(d: Data): Data {
    const r = this.#run(d);
    const key = optStr(d, 'key');
    const after = optInt(d, 'after_seq', 0, Number.MAX_SAFE_INTEGER, 0);
    const list = this.#store.events(r.run_id, after, optInt(d, 'limit', 1, MAX_EVENTS, MAX_EVENTS), key);
    const events: RunEvent[] = [];
    let size = wireSize('run.events.reply', { nonce: d.nonce, ok: true, events: [], last_seq: r.last_seq, more: true });
    for (const ev of list) {
      const n = encodedSize(ev) + 1;
      if (events.length > 0 && size + n > MAX_MESSAGE_BYTES) break;
      size += n;
      events.push(ev);
    }
    const lastSeq = events.length > 0 ? events[events.length - 1].seq : after;
    return { events, last_seq: r.last_seq, more: this.#store.hasEventAfter(r.run_id, lastSeq, key) };
  }

  #nodeGet(d: Data): Data {
    const run = this.#run(d);
    const key = str(d, 'key');
    const row: Row | undefined = this.#store.getNode(run.run_id, key);
    if (!row) throw new TopicError('no_such_node', `no node ${key} in run ${run.run_id}`);
    const offset = optInt(d, 'offset', 0, Number.MAX_SAFE_INTEGER, 0);
    const limit = d.limit === undefined ? DEFAULT_OUTPUT_LIMIT : d.limit;
    if (typeof limit !== 'number' || !Number.isInteger(limit) || limit < 4) throw new TopicError('invalid_limit', 'limit must be an integer of at least 4');
    const want = Math.min(limit, DEFAULT_OUTPUT_LIMIT);

    // Everything but the output slice is redacted and counted against the budget. Permission ids pass untouched (B-16).
    const fields: Data = {
      key: row.key,
      node_id: row.node_id,
      state: row.state,
      attempt: row.attempt,
      ...(row.session_id ? { session_id: row.session_id } : {}),
      ...(row.branch ? { branch: row.branch } : {}),
      ...(row.worktree ? { worktree: row.worktree } : {}),
      ...(row.error ? { error: redactString(row.error).text } : {}),
      ...(row.conflict ? { conflict: redactValue(row.conflict).value } : {}),
      ...(row.state === 'waiting' && row.waiting
        ? { waiting: prepareEvent({ key, node_id: row.node_id, attempt: row.attempt, type: 'node.waiting', data: row.waiting }, this.#store).data }
        : {}),
    };
    if (row.state !== 'done' || row.output === undefined) return fields;

    const bytes = new TextEncoder().encode(JSON.stringify(row.output));
    const total = bytes.length;
    if (offset > total) throw new TopicError('invalid_offset', `offset ${offset} is past the end (${total} bytes)`);
    if (offset < total && (bytes[offset] & 0xc0) === 0x80) throw new TopicError('invalid_offset', `offset ${offset} is inside a character`);
    let end = Math.min(total, offset + want);
    while (end > offset && end < total && (bytes[end] & 0xc0) === 0x80) end--;
    let slice = new TextDecoder().decode(bytes.subarray(offset, end));
    // The reply, not the slice, must fit 48 KiB once Go has escaped it.
    const room = MAX_MESSAGE_BYTES - wireSize('node.get.reply', { ...fields, nonce: d.nonce, ok: true, output: '', total_bytes: total, next_offset: total, more: true });
    const cut = truncateEncoded(slice, Math.max(0, room));
    slice = cut.text;
    const next = offset + new TextEncoder().encode(slice).length;
    return { ...fields, output: slice, total_bytes: total, next_offset: next, more: next < total };
  }
}
