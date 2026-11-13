// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The topics a GUI reads and writes (spec 529, Contract for C): scoping,
// paging, and the 48 KiB budget measured as the daemon sends it.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_EVENT_BYTES } from '../engine/events.ts';
import type { Row } from '../engine/model.ts';
import { encodedSize, MAX_MESSAGE_BYTES } from '../engine/size.ts';
import { PLUGIN_ID } from '../engine/topics.ts';
import { type IR, p, workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { examples } from '../tools/examples.ts';
import { call, type Harness, harness, rowOf, runState, scripted, waitFor } from './harness.ts';

const never = (): Promise<void> => new Promise(() => {});
const parked = { agent: never, check: never };

const humanIr = (): IR =>
  workflow('ask', s.object({ who: s.string() }), (g, input) => {
    g.human('ask', { prompt: p`Hello ${input.who}`, output: s.object({ yes: s.boolean() }) });
  });
const checkIr = (): IR => workflow('chk', s.object({}), (g) => void g.check('c', { argv: ['true'] }));

const wire = (topic: string, data: unknown): number => encodedSize({ plugin_id: PLUGIN_ID, topic: `${topic}.reply`, data });

function seed(h: Harness, id: string, rows: Row[], extra: { project?: string; ir?: unknown; input?: unknown } = {}): void {
  h.store.createRun(
    { run_id: id, project_id: extra.project ?? 'p1', workflow: 'w', file: 'w.ts', source_hash: 'h', ir: JSON.stringify(extra.ir ?? { ir: 1, name: 'w', input: { type: 'object', properties: {}, required: [], additionalProperties: false }, nodes: [], edges: [] }), input: JSON.stringify(extra.input ?? {}) },
    rows,
    [{ key: null, node_id: null, attempt: 0, type: 'run.started', data: {} }],
  );
}
const row = (key: string, state: Row['state'], extra: Partial<Row> = {}): Row => ({ key, node_id: key.replace(/\[\d+\]/g, ''), state, attempt: 1, ready: true, progress: 0, turns: 0, ...extra });

// ------------------------------------------------------------------ runs.list

test('runs.list scopes by project and state, and without project_id spans every project (B-2)', async () => {
  const h = harness(scripted(() => ({}), () => ({ ok: true, exit_code: 0 })));
  try {
    const a = h.engine.startRunFromIR({ project_id: 'p1', file: 'a.ts', ir: humanIr(), input: { who: 'x' } });
    const b = h.engine.startRunFromIR({ project_id: 'p2', file: 'b.ts', ir: checkIr(), input: {} });
    const c = h.engine.startRunFromIR({ project_id: 'p1', file: 'c.ts', ir: checkIr(), input: {} });
    await waitFor(() => runState(h, b) === 'done' && runState(h, c) === 'done', 'checks to finish');
    const all = call(h, 'runs.list') as { runs: { run_id: string; project_id: string; state: string }[] };
    assert.deepEqual(new Set(all.runs.map((r) => r.run_id)), new Set([a, b, c]));
    assert.deepEqual(new Set(all.runs.map((r) => r.project_id)), new Set(['p1', 'p2']), 'every row says which project it is in');

    const waiting = call(h, 'runs.list', { state: 'waiting' }) as { runs: { run_id: string }[] };
    assert.deepEqual(waiting.runs.map((r) => r.run_id), [a]);

    const p1done = call(h, 'runs.list', { project_id: 'p1', state: 'done' }) as { runs: { run_id: string }[] };
    assert.deepEqual(p1done.runs.map((r) => r.run_id), [c]);
    assert.equal((call(h, 'runs.list', { state: 'nonsense' }).error as { code: string }).code, 'invalid_request');
    const one = (call(h, 'runs.list') as { runs: Record<string, unknown>[] }).runs[0];
    assert.deepEqual(Object.keys(one).sort(), ['created_at', 'file', 'project_id', 'run_id', 'source_changed', 'source_hash', 'state', 'updated_at', 'workflow']);
  } finally {
    h.close();
  }
});

// -------------------------------------------------------------------- run.get

test('run.get returns {run, ir} with last_seq, and stays under 48 KiB with a maximal IR, input and run row', () => {
  const h = harness(parked);
  try {
    const ir = { ir: 1, name: 'big', input: examples.reviewLoop().input, nodes: [{ id: 'c', kind: 'check', argv: ['true'], timeout_s: 1 }], edges: [], pad: '<'.repeat(5000) };
    seed(h, 'r1', [row('c', 'pending')], { ir, input: { task: '<'.repeat(1300) } });
    const many = Array.from({ length: 100 }, (_, i) => `/worktrees/${'w'.repeat(400)}${i}`);
    h.store.commit('r1', { rows: [], events: [], run: { state: 'failed', error: '<'.repeat(5000), kept_worktrees: many } });
    const r = call(h, 'run.get', { run_id: 'r1' }) as { ok: boolean; run: Record<string, unknown>; ir: unknown };
    assert.equal(r.ok, true);
    assert.equal(r.run.last_seq, h.store.getRun('r1')?.last_seq, 'B-14: the run carries the seq it reflects');
    assert.equal((r.run.kept_worktrees as string[]).length, 16);
    assert.equal(r.run.kept_worktrees_total, 100);
    assert.ok(encodedSize(r.run.error) <= 2048 + 2, 'error is cut to 2 KiB as sent');
    assert.ok(wire('run.get', r) <= MAX_MESSAGE_BYTES);
    assert.deepEqual(Object.keys(r.ir as object).includes('nodes'), true);
    assert.equal((call(h, 'run.get', { run_id: 'nope' }).error as { code: string }).code, 'no_such_run');
  } finally {
    h.close();
  }
});

test('run.get serves input redacted', () => {
  const h = harness(parked);
  try {
    seed(h, 'r1', [], { input: { task: 'use AKIAABCDEFGHIJKLMNOP please' } });
    assert.deepEqual((call(h, 'run.get', { run_id: 'r1' }).run as { input: unknown }).input, { task: 'use [redacted] please' });
  } finally {
    h.close();
  }
});

// ------------------------------------------------------------------ run.nodes

test('run.nodes pages by key, each page tagged with its own last_seq, and a page always fits 48 KiB with at least one row', () => {
  const h = harness(parked);
  try {
    // The longest keys the engine allows (256 bytes), one commit of 1,200 rows.
    const rows = Array.from({ length: 1200 }, (_, i) => row(`${'a'.repeat(240)}${String(i).padStart(5, '0')}`, 'done', { started_at: '2026-01-01T00:00:00.000Z', ended_at: '2026-01-01T00:00:01.000Z', cap: 'x'.repeat(20) }));
    seed(h, 'r1', rows);
    const seen: string[] = [];
    let after: string | undefined;
    const seqs: number[] = [];
    for (let pages = 0; pages < 1000; pages++) {
      const r = call(h, 'run.nodes', { run_id: 'r1', ...(after ? { after } : {}) }) as { nodes: { key: string }[]; last_seq: number; more?: boolean; next_after?: string };
      assert.ok(r.nodes.length >= 1);
      assert.ok(wire('run.nodes', r) <= MAX_MESSAGE_BYTES, `page ${pages} is ${wire('run.nodes', r)} bytes`);
      seen.push(...r.nodes.map((n) => n.key));
      seqs.push(r.last_seq);
      if (pages === 0) h.store.commit('r1', { rows: [], events: [{ key: null, node_id: null, attempt: 0, type: 'run.retried', data: { reset: 0 } }] });
      if (!r.more) break;
      assert.equal(r.next_after, r.nodes.at(-1)?.key, 'next_after is the last key returned');
      after = r.next_after;
    }
    assert.deepEqual(seen, rows.map((r) => r.key).sort(), 'every row once, in key order');
    assert.ok(seqs.length > 5, 'it took several pages');
    assert.ok(seqs[1] > seqs[0], "each page carries the last_seq when that page was read");
  } finally {
    h.close();
  }
});

test('run.nodes rows are slim: no output, no error', () => {
  const h = harness(parked);
  try {
    seed(h, 'r1', [row('a', 'failed', { error: 'boom', output: { x: 1 }, output_raw: { x: 1 } })]);
    assert.deepEqual(Object.keys((call(h, 'run.nodes', { run_id: 'r1' }).nodes as object[])[0]).sort(), ['attempt', 'key', 'node_id', 'state']);
  } finally {
    h.close();
  }
});

test('a map expansion writes one row per node instance of every copy, and the count matches map.expanded', async () => {
  const h = harness(parked);
  try {
    const ir = workflow('m', s.object({ items: s.array(s.string()) }), (g, input) => {
      g.map('m', { over: input.items, concurrency: 1 }, (b, item) => {
        const a = b.agent('a', { agent: 'claude', prompt: p`do ${item}`, output: s.object({ r: s.string() }) });
        const c = b.check('c', { argv: ['true'] });
        b.edge(a, c);
        return c;
      });
    });
    const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'm.ts', ir, input: { items: ['x', 'y', 'z', 'w'] } });
    await waitFor(() => h.store.events(id, 0, 100).some((e) => e.type === 'map.expanded'), 'the expansion');
    const keys = (call(h, 'run.nodes', { run_id: id }).nodes as { key: string; state: string }[]).filter((n) => n.key !== 'm');
    const copies = new Set(keys.map((n) => /\[(\d+)\]/.exec(n.key)?.[1]));
    const expanded = h.store.events(id, 0, 100).find((e) => e.type === 'map.expanded');
    assert.equal(copies.size, (expanded?.data as { count: number }).count);
    assert.deepEqual(keys.map((n) => n.key).sort(), ['m[0]/a', 'm[0]/c', 'm[1]/a', 'm[1]/c', 'm[2]/a', 'm[2]/c', 'm[3]/a', 'm[3]/c']);
    // Copy 0 is admitted (concurrency 1); the others wait, with rows, pending.
    assert.ok(keys.filter((n) => n.key.startsWith('m[3]')).every((n) => n.state === 'pending'));
  } finally {
    h.close();
  }
});

test('node ids and keys over the cap are refused with invalid_ir', () => {
  const h = harness(parked);
  try {
    const bad = (id: string) => ({ ir: 1, name: 'x', input: examples.reviewLoop().input, nodes: [{ id, kind: 'check', argv: ['true'], timeout_s: 1 }], edges: [] });
    const r = call(h, 'test.start-run', { project_id: 'p1', ir: bad('a'.repeat(25_000)), input: { task: 't' } }) as { ok: boolean; error: { code: string; message: string } };
    assert.equal(r.ok, false);
    assert.equal(r.error.code, 'invalid_ir');
    assert.match(r.error.message, /id is 25000 bytes, the limit is 128/);
  } finally {
    h.close();
  }
});

// -------------------------------------------------------------------- node.get

test('node.get waiting payload only while waiting (B-13)', async () => {
  const h = harness(parked);
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'a.ts', ir: humanIr(), input: { who: 'Ada' } });
    const waiting = call(h, 'node.get', { run_id: id, key: 'ask' }) as { state: string; waiting: { reason: string; prompt: string; output_schema: unknown } };
    assert.equal(waiting.state, 'waiting');
    assert.deepEqual(waiting.waiting, { reason: 'human', prompt: 'Hello Ada', output_schema: (humanIr().nodes[0] as { output: unknown }).output });
    assert.equal(call(h, 'human.answer', { run_id: id, key: 'ask', answer: { yes: true } }).ok, true);
    const done = call(h, 'node.get', { run_id: id, key: 'ask' });
    assert.equal(done.state, 'done');
    assert.equal('waiting' in done, false, 'gone once answered');
    assert.equal(done.output, '{"yes":true}');
  } finally {
    h.close();
  }
});

test('node.get carries session_id, branch, worktree, error and conflict, redacted, and the worst case still fits 48 KiB', () => {
  const h = harness(parked);
  try {
    const waiting = { reason: 'human', prompt: '<'.repeat(2700), output_schema: { type: 'object', properties: { a: { type: 'string', enum: ['<'.repeat(1500)] } }, required: ['a'], additionalProperties: false } };
    const out = { text: '<'.repeat(30_000) };
    seed(h, 'r1', [
      row('n', 'waiting', {
        session_id: 's-1',
        branch: 'hive/wf/r1/n',
        worktree: '/w/n',
        error: `failed with ghp_${'a'.repeat(36)} in it ${'<'.repeat(900)}`,
        conflict: { branch: 'hive/wf/r1/m', files: Array.from({ length: 40 }, (_, i) => `src/${'<'.repeat(30)}${i}.go`), worktree: '/w/m' },
        waiting,
        output: out,
        output_raw: out,
      }),
    ]);
    const r = call(h, 'node.get', { run_id: 'r1', key: 'n' }) as Record<string, unknown>;
    assert.equal(r.ok, true, JSON.stringify(r.error));
    assert.equal(r.session_id, 's-1');
    assert.equal(r.branch, 'hive/wf/r1/n');
    assert.equal(r.worktree, '/w/n');
    assert.match(String(r.error), /^failed with \[redacted\] in it/);
    assert.equal((r.conflict as { branch: string }).branch, 'hive/wf/r1/m');
    assert.ok(wire('node.get', r) <= MAX_MESSAGE_BYTES);
    // A waiting node has no output yet, so the output fields are absent.
    assert.equal('output' in r, false);

    // Done: the same fields, plus an output slice that fits whatever room is left.
    h.store.commit('r1', { rows: [row('n', 'done', { error: 'e'.repeat(1900), conflict: { branch: 'b', files: ['<'.repeat(250)], worktree: '/w' }, output: out, output_raw: out })], events: [] });
    const d = call(h, 'node.get', { run_id: 'r1', key: 'n' }) as { output: string; next_offset: number; more: boolean; total_bytes: number };
    assert.ok(wire('node.get', d) <= MAX_MESSAGE_BYTES);
    assert.ok(d.output.length >= 1, 'forward progress: at least one character');
    assert.equal(d.total_bytes, new TextEncoder().encode(JSON.stringify(out)).length);
    assert.ok(d.more);
  } finally {
    h.close();
  }
});

test('node.get pages the serialized redacted output by byte, on character boundaries, and the pieces rebuild it (B-7)', () => {
  const h = harness(parked);
  try {
    const out = { text: `é😀<${'日本語>&'.repeat(6000)}`, n: 7 };
    seed(h, 'r1', [row('n', 'done', { output: out, output_raw: out })]);
    const whole = JSON.stringify(out);
    let got = '';
    let offset = 0;
    let pages = 0;
    for (;;) {
      const r = call(h, 'node.get', { run_id: 'r1', key: 'n', offset }) as { output: string; next_offset: number; more: boolean; total_bytes: number };
      assert.ok(wire('node.get', r) <= MAX_MESSAGE_BYTES);
      assert.ok(r.next_offset > offset, 'always forward');
      got += r.output;
      offset = r.next_offset;
      pages++;
      if (!r.more) break;
    }
    assert.equal(got, whole);
    assert.ok(pages > 3, 'a 100 KB output needs several pages');
    // A small limit is honoured, and a character is never split.
    const small = call(h, 'node.get', { run_id: 'r1', key: 'n', offset: 0, limit: 6 }) as { output: string; next_offset: number };
    assert.equal(small.output, '{"text');
    const emoji = call(h, 'node.get', { run_id: 'r1', key: 'n', offset: 11, limit: 4 }) as { output: string; next_offset: number };
    assert.deepEqual([emoji.output, emoji.next_offset], ['😀', 15], 'a 4-byte character fits a 4-byte limit exactly');
    const cut = call(h, 'node.get', { run_id: 'r1', key: 'n', offset: 9, limit: 4 }) as { output: string; next_offset: number };
    assert.deepEqual([cut.output, cut.next_offset], ['é', 11], 'a limit that would cut the next character stops before it');
    assert.equal((call(h, 'node.get', { run_id: 'r1', key: 'n', limit: 3 }).error as { code: string }).code, 'invalid_limit');
    assert.equal((call(h, 'node.get', { run_id: 'r1', key: 'n', offset: 10 }).error as { code: string }).code, 'invalid_offset', 'offset 10 is inside the é');
  } finally {
    h.close();
  }
});

// ------------------------------------------------------------------ run.events

test('run.events pages by seq, byte-budgeted with at least one event, and the key filter returns one node (B-17)', () => {
  const h = harness(parked);
  try {
    seed(h, 'r1', [row('a', 'running'), row('b', 'running')]);
    for (let i = 0; i < 12; i++) {
      const key = i % 3 === 0 ? 'a' : 'b';
      h.store.commit('r1', { rows: [], events: [{ key, node_id: key, attempt: 1, type: 'check.output', data: { stream: 'stdout', text: '<'.repeat(2900) + i } }] });
    }
    const page = (after: number, key?: string) => call(h, 'run.events', { run_id: 'r1', after_seq: after, ...(key ? { key } : {}) }) as { events: { seq: number; key: string }[]; last_seq: number; more: boolean };
    let after = 0;
    const seqs: number[] = [];
    let sizes = 0;
    for (;;) {
      const r = page(after);
      assert.ok(r.events.length >= 1);
      assert.ok(wire('run.events', r) <= MAX_MESSAGE_BYTES);
      sizes++;
      seqs.push(...r.events.map((e) => e.seq));
      after = r.events.at(-1)?.seq ?? after;
      if (!r.more) break;
      assert.ok(r.events.length < 500, 'more can be true well below the limit: the budget, not the count, stopped it');
    }
    assert.deepEqual(seqs, Array.from({ length: 13 }, (_, i) => i + 1), 'no gap, no repeat');
    assert.ok(sizes > 3);
    const a = page(0, 'a');
    assert.deepEqual([...new Set(a.events.map((e) => e.key))], ['a']);
    assert.equal(a.last_seq, 13, 'last_seq is the run-level one even when filtered');
  } finally {
    h.close();
  }
});

test('every stored event is at most 24 KiB as sent, so a page can always hold one (B-11)', () => {
  const h = harness(parked);
  try {
    seed(h, 'r1', [row('a', 'running')]);
    h.store.commit('r1', { rows: [], events: [
      { key: 'a', node_id: 'a', attempt: 1, type: 'agent.message', data: { session_id: 's', item_id: 'i', text: '<'.repeat(20_000) } },
      { key: 'a', node_id: 'a', attempt: 1, type: 'agent.plan', data: { session_id: 's', entries: Array.from({ length: 400 }, (_, i) => ({ content: `step ${i} ${'<'.repeat(100)}`, status: 'pending' })) } },
      { key: 'a', node_id: 'a', attempt: 1, type: 'node.output', data: { output: { text: '<'.repeat(20_000) } } },
    ] });
    const evs = h.store.events('r1', 1, 10);
    for (const e of evs) assert.ok(encodedSize(e.data) <= MAX_EVENT_BYTES, `${e.type} is ${encodedSize(e.data)}`);
    assert.equal(evs[0].data.truncated, true);
    assert.ok(typeof evs[1].data.entries_omitted === 'number' && evs[1].data.entries_omitted > 0);
    assert.equal(evs[2].data.truncated, true, 'node.output carries a prefix; node.get has the rest');
    assert.equal(typeof evs[2].data.output, 'string');
  } finally {
    h.close();
  }
});

// ---------------------------------------------------------- permission ids (B-16)

test('permission ids pass redaction when they validate, and are swapped for a handle when they do not (B-16)', async () => {
  const h = harness(scripted(() => 'hang'));
  try {
    const ir = workflow('a', s.object({}), (g) => void g.agent('a', { agent: 'claude', prompt: 'go', output: s.object({ r: s.string() }) }));
    const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'a.ts', ir, input: {} });
    await waitFor(() => rowOf(h, id, 'a')?.state === 'running', 'the agent to run');
    const tokenShaped = 'AKIAABCDEFGHIJKLMNOP'; // would be redacted as a secret if it were not exempt
    const options = [
      { option_id: `sk-${'z'.repeat(30)}`, name: 'Allow once', kind: 'allow_once' },
      { option_id: 'bad id/with spaces', name: 'Reject', kind: 'reject_once' },
    ];
    h.engine.commit(id, { t: 'wait', key: 'a', waiting: { reason: 'permission', session_id: 's-a', request_id: tokenShaped, title: `Run curl with ghp_${'a'.repeat(36)}`, options } });
    const r = call(h, 'node.get', { run_id: id, key: 'a' }) as { state: string; waiting: { request_id: string; title: string; options: { option_id: string; name: string }[] } };
    assert.equal(r.state, 'waiting');
    assert.equal(r.waiting.request_id, tokenShaped, 'a valid id reaches ANSWER_PERMISSION untouched');
    assert.equal(r.waiting.options[0].option_id, `sk-${'z'.repeat(30)}`);
    assert.match(r.waiting.title, /^Run curl with \[redacted\]$/, 'everything else is still redacted');
    const handle = r.waiting.options[1].option_id;
    assert.match(handle, /^h-[0-9a-f]{24}$/);
    assert.equal(h.store.realIdFor(id, handle), 'bad id/with spaces', 'the engine can map the handle back');
    // The event carries the same prepared data as node.get.
    const ev = h.store.events(id, 0, 100).find((e) => e.type === 'node.waiting');
    assert.equal((ev?.data as { request_id: string }).request_id, tokenShaped);
    // node.get serves the stored data as it is: no re-preparation, and no write on a read.
    assert.deepEqual(r.waiting, ev?.data, 'node.get and the event agree exactly');
    assert.equal((call(h, 'node.get', { run_id: id, key: 'a' }) as { waiting: { options: { option_id: string }[] } }).waiting.options[1].option_id, handle);
  } finally {
    h.close();
  }
});

test('handles belong to their run: a commit on another run never changes what node.get serves (B-16)', async () => {
  const h = harness(scripted(() => 'hang'));
  try {
    const ir = workflow('a', s.object({}), (g) => void g.agent('a', { agent: 'claude', prompt: 'go', output: s.object({ r: s.string() }) }));
    const runA = h.engine.startRunFromIR({ project_id: 'p1', file: 'a.ts', ir, input: {} });
    const runB = h.engine.startRunFromIR({ project_id: 'p1', file: 'b.ts', ir, input: {} });
    await waitFor(() => rowOf(h, runA, 'a')?.state === 'running' && rowOf(h, runB, 'a')?.state === 'running', 'both agents to run');
    const wait = { reason: 'permission', session_id: 's-a', request_id: '1-1', title: 'Allow?', options: [{ option_id: 'bad id/with spaces', name: 'Reject', kind: 'reject_once' }] };
    h.engine.commit(runA, { t: 'wait', key: 'a', waiting: wait });
    const handleA = (call(h, 'node.get', { run_id: runA, key: 'a' }) as { waiting: { options: { option_id: string }[] } }).waiting.options[0].option_id;
    // Run B commits, with the same bad id in its own wait.
    h.engine.commit(runB, { t: 'wait', key: 'a', waiting: { ...wait, session_id: 's-b' } });
    const handleB = (call(h, 'node.get', { run_id: runB, key: 'a' }) as { waiting: { options: { option_id: string }[] } }).waiting.options[0].option_id;
    assert.notEqual(handleA, handleB, 'each run mints its own');
    const again = (call(h, 'node.get', { run_id: runA, key: 'a' }) as { waiting: { options: { option_id: string }[] } }).waiting.options[0].option_id;
    assert.equal(again, handleA, 'a read never mints a new handle');
    const ev = h.store.events(runA, 0, 100).find((e) => e.type === 'node.waiting');
    assert.equal((ev?.data as { options: { option_id: string }[] }).options[0].option_id, handleA, 'the event and node.get agree');
    assert.equal(h.store.realIdFor(runA, handleA), 'bad id/with spaces');
    assert.equal(h.store.realIdFor(runB, handleA), handleA, 'run B knows nothing of run A handle');
  } finally {
    h.close();
  }
});

// --------------------------------------------------------------------- errors

test('requests are answered on <topic>.reply with their nonce, and failures carry a code', () => {
  const h = harness(parked);
  try {
    const bad = h.topics.handle('run.get', { nonce: 'abc', run_id: 'nope' });
    assert.equal(bad?.topic, 'run.get.reply');
    assert.deepEqual(bad?.data, { nonce: 'abc', ok: false, error: { code: 'no_such_run', message: 'no run nope' } });
    assert.equal(h.topics.handle('not.a.topic', {}), undefined);
    assert.equal(h.topics.handle('toString', {}), undefined, 'inherited names are not topics');
    const noSeam = harness(parked, { testSeam: false });
    try {
      assert.equal(noSeam.topics.handle('test.start-run', {}), undefined, 'the seam is off unless asked for');
    } finally {
      noSeam.close();
    }
  } finally {
    h.close();
  }
});
