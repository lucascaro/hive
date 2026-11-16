// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The client-side merge of spec 529's "Snapshot-to-live merge" (B-12), as a
// reference implementation: a `run.nodes` page, read at some moment, plus the
// events after its `last_seq`, must equal the final node state. The engine
// side is real (store, topics, engine); only the client fold is written here.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { RunEvent } from '../engine/events.ts';
import { buildIndex, type Index, keyOf } from '../engine/keys.ts';
import type { IR } from '../sdk/dsl.ts';
import { examples } from '../tools/examples.ts';
import { call, type Harness, harness, runState, scripted, waitFor } from './harness.ts';

interface Slim {
  key: string;
  state: string;
  attempt: number;
}
interface Page {
  nodes: Slim[];
  last_seq: number;
  more?: boolean;
  next_after?: string;
}

/** The instance keys a `map.expanded` or `loop.iteration` event implies: every direct child of the container, under the new index. */
function instancesFor(ix: Index, containerKey: string, index: number): string[] {
  const id = containerKey.replace(/\[\d+\]/g, '');
  const ctxOf = (key: string): number[] => [...key.matchAll(/\[(\d+)\]/g)].map((m) => Number(m[1]));
  const out: string[] = [];
  for (const [nid, placed] of ix.byId) {
    if (placed.parent === id) out.push(keyOf(ix, nid, [...ctxOf(containerKey), index]));
  }
  return out;
}

/** Applies one event to the rows of a page, once, and only when the page does not already reflect it. */
function applyEvent(ix: Index, rows: Map<string, Slim>, ev: RunEvent): void {
  if (ev.key === null) return;
  const set = (key: string, patch: Partial<Slim>): void => {
    rows.set(key, { key, state: 'pending', attempt: 0, ...rows.get(key), ...patch });
  };
  switch (ev.type) {
    case 'node.started':
      return set(ev.key, { state: 'running', attempt: ev.attempt });
    case 'node.waiting':
      return set(ev.key, { state: 'waiting' });
    case 'node.resumed':
      return set(ev.key, { state: 'running' });
    case 'node.finished':
      return set(ev.key, { state: String(ev.data.state) });
    case 'node.reset':
      return set(ev.key, { state: 'pending', attempt: 0 });
    case 'map.expanded':
      for (let i = 0; i < Number(ev.data.count); i++) for (const k of instancesFor(ix, ev.key, i)) if (!rows.has(k)) set(k, {});
      return;
    case 'loop.iteration':
      for (const k of instancesFor(ix, ev.key, Number(ev.data.iteration))) if (!rows.has(k)) set(k, {});
      return;
  }
}

/** The merge: apply buffered events with seq > the page's last_seq, in order. */
function merge(ix: Index, page: Page, buffered: RunEvent[]): Map<string, Slim> {
  const rows = new Map(page.nodes.map((n) => [n.key, { ...n }]));
  for (const ev of [...buffered].sort((a, b) => a.seq - b.seq)) if (ev.seq > page.last_seq) applyEvent(ix, rows, ev);
  return rows;
}

const finalState = (h: Harness, id: string): Map<string, Slim> => {
  const out = new Map<string, Slim>();
  let after: string | undefined;
  for (;;) {
    const r = call(h, 'run.nodes', { run_id: id, ...(after ? { after } : {}) }) as unknown as Page;
    for (const n of r.nodes) out.set(n.key, { key: n.key, state: n.state, attempt: n.attempt });
    if (!r.more) return out;
    after = r.next_after;
  }
};

const view = (rows: Map<string, Slim>): [string, string, number][] => [...rows.values()].map((n): [string, string, number] => [n.key, n.state, n.attempt]).sort((a, b) => (a[0] < b[0] ? -1 : 1));

async function runWithSnapshots(name: string, ir: IR, input: Record<string, unknown>, snapAt: (key: string) => boolean): Promise<{ h: Harness; id: string; pages: Page[] }> {
  const pages: Page[] = [];
  let id = '';
  const h = harness(
    scripted((key) => {
      if (id && snapAt(key)) pages.push(call(h, 'run.nodes', { run_id: id }) as unknown as Page);
      if (key === 'split') return { tasks: ['a', 'b', 'c'] };
      if (key.endsWith('/review')) return { verdict: key.includes('[3]') ? 'approve' : 'revise', feedback: 'f' };
      return { summary: key };
    }),
  );
  id = h.engine.startRunFromIR({ project_id: 'p', file: `${name}.ts`, ir, input });
  await waitFor(() => runState(h, id) === 'done', 'the run');
  return { h, id, pages };
}

test('a page read mid-run plus the events after its last_seq equals the final node state, including rows a later event creates', async () => {
  const ir = examples.fanOutThenVerify();
  const ix = buildIndex(ir);
  // One page before the map expands, one while its copies run.
  const { h, id, pages } = await runWithSnapshots('fan', ir, { goal: 'g' }, (k) => k === 'split' || k === 'work/each[1]/do');
  try {
    assert.equal(pages.length, 2);
    assert.ok(![...pages[0].nodes].some((n) => n.key.startsWith('work/each[')), 'the first page predates the copies');
    assert.ok(pages[1].nodes.some((n) => n.key.startsWith('work/each[2]')), 'the second page has them, pending, from the same commit as map.expanded');
    const events = h.store.events(id, 0, 10_000);
    const want = view(finalState(h, id));
    for (const page of pages) assert.deepEqual(view(merge(ix, page, events)), want, `page at seq ${page.last_seq}`);
  } finally {
    h.close();
  }
});

test('loop iterations merge the same way, and an event the page already reflects is not applied twice', async () => {
  const ir = examples.reviewLoop();
  const ix = buildIndex(ir);
  const { h, id, pages } = await runWithSnapshots('loop', ir, { task: 't' }, (k) => k === 'fix[2]/work');
  try {
    const events = h.store.events(id, 0, 10_000);
    const want = view(finalState(h, id));
    assert.deepEqual(view(merge(ix, pages[0], events)), want);
    // Replaying every event on top of the page changes nothing past last_seq: seq <= last_seq is dropped.
    const twice = merge(ix, pages[0], [...events, ...events]);
    assert.deepEqual(view(twice), want);
    // Applying the pre-page events would have been wrong for this page: it proves the watermark is doing work.
    const naive = new Map(pages[0].nodes.map((n) => [n.key, { ...n }]));
    for (const ev of events) applyEvent(ix, naive, ev);
    assert.notDeepEqual(view(naive), view(new Map(pages[0].nodes.map((n) => [n.key, n]))), 'sanity: the fold does move rows');
  } finally {
    h.close();
  }
});

test('retry events fold by the same rule: node.reset returns a row to pending and run.retried touches no node', async () => {
  const ir = examples.reviewLoop();
  const ix = buildIndex(ir);
  let fail = true;
  const h = harness(scripted((k) => (k === 'fix[1]/work' && fail ? { fail: 'boom' } : { summary: k, verdict: 'approve', feedback: 'f' })));
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'l.ts', ir, input: { task: 't' } });
    await waitFor(() => runState(h, id) === 'failed', 'the failure');
    const beforeRetry = call(h, 'run.nodes', { run_id: id }) as unknown as Page;
    fail = false;
    assert.equal(call(h, 'run.retry', { run_id: id }).ok, true);
    await waitFor(() => runState(h, id) === 'done', 'the retried run');
    const events = h.store.events(id, 0, 10_000);
    assert.deepEqual(view(merge(ix, beforeRetry, events)), view(finalState(h, id)));
    assert.equal(finalState(h, id).get('fix[1]/work')?.attempt, 1, 'the retry started over: attempt 1, not 2');
  } finally {
    h.close();
  }
});

test('a hole in the buffered events is a gap, closed by looping run.events past one page, with the live push deduplicated', async () => {
  const ir = examples.fanOutThenVerify();
  const ix = buildIndex(ir);
  const { h, id, pages } = await runWithSnapshots('gap', ir, { goal: 'g' }, (k) => k === 'split');
  try {
    const page = pages[0];
    const all = h.store.events(id, 0, 10_000);
    const live = all.filter((e) => e.seq > page.last_seq);
    // The client missed events page.last_seq+1 .. +5 and saw only the later ones; it must notice.
    const buffered = live.slice(5);
    const contiguous = (list: RunEvent[], from: number): boolean => list.every((e, i) => e.seq === from + i + 1);
    assert.equal(contiguous(buffered, page.last_seq), false, 'a hole is detected from the smallest page last_seq');

    // Close it: loop run.events (3 at a time, so several pages) until it meets the buffer, applying each event once.
    const applied = new Map<number, RunEvent>();
    let after = page.last_seq;
    let loops = 0;
    for (;;) {
      const r = call(h, 'run.events', { run_id: id, after_seq: after, limit: 3 }) as { events: RunEvent[]; more: boolean };
      for (const ev of r.events) applied.set(ev.seq, ev);
      after = r.events.at(-1)?.seq ?? after;
      loops++;
      if (!r.more) break;
    }
    assert.ok(loops > 3, 'the gap took more than one page');
    for (const ev of buffered) applied.set(ev.seq, ev); // the live pushes that exposed the gap: a Map dedupes them by seq
    const ordered = [...applied.values()].sort((a, b) => a.seq - b.seq);
    assert.equal(ordered.length, live.length, 'every event once');
    assert.equal(contiguous(ordered, page.last_seq), true);
    assert.deepEqual(view(merge(ix, page, ordered)), view(finalState(h, id)));
  } finally {
    h.close();
  }
});
