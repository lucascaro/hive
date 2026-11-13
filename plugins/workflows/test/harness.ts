// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// A real store and engine against a fake hive connection (spec 529, Tests):
// nothing mocked but the daemon and the processes, so the transactions are the
// real ones.

import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Engine, type Executors, type Hive, type NodeContext } from '../engine/engine.ts';
import type { RunEvent } from '../engine/events.ts';
import type { Row } from '../engine/model.ts';
import { Store, type StoreOptions } from '../engine/store.ts';
import { Topics } from '../engine/topics.ts';
import type { Node } from '../sdk/dsl.ts';

export class FakeHive implements Hive {
  requests: { name: string; payload: Record<string, unknown> }[] = [];
  posts: { topic: string; data: unknown }[] = [];
  /** How a request is answered; unlisted verbs answer `{}`. */
  replies: Record<string, (payload: Record<string, unknown>) => unknown> = {};

  // biome-ignore lint/suspicious/noExplicitAny: wire replies are decoded JSON
  request(name: string, payload: object = {}): Promise<any> {
    this.requests.push({ name, payload: payload as Record<string, unknown> });
    const f = this.replies[name];
    return Promise.resolve(f ? f(payload as Record<string, unknown>) : {});
  }

  postMessage(topic: string, data?: unknown): void {
    this.posts.push({ topic, data });
  }
}

export const tick = (): Promise<void> => new Promise((r) => setImmediate(r));

export async function waitFor(cond: () => boolean, what: string, ms = 5000): Promise<void> {
  const until = Date.now() + ms;
  while (!cond()) {
    if (Date.now() > until) throw new Error(`timed out waiting for ${what}`);
    await new Promise((r) => setTimeout(r, 5));
  }
}

export type AgentFn = (key: string, node: Node, row: Row) => unknown | { fail: string } | 'hang';

/**
 * Executors that play the nodes from a script. An agent or check answers with
 * its output; `{fail}` fails the node; 'hang' leaves it running for the test
 * to finish by hand.
 */
export function scripted(agent: AgentFn, check: AgentFn = () => ({ ok: true, exit_code: 0 })): Executors {
  const play = (kind: 'agent' | 'check', fn: AgentFn) => async ({ engine, run_id, key, node }: NodeContext) => {
    await tick();
    if (kind === 'agent' && !engine.reserveTurn(run_id, key)) return;
    const row = engine.run(run_id).model.rows.get(key) as Row;
    const out = fn(key, node, row);
    if (!engine.beginNode(run_id, key, { session_id: `s-${key}` })) return;
    if (out === 'hang') return;
    await tick();
    if (typeof out === 'object' && out !== null && 'fail' in out) engine.failNode(run_id, key, String((out as { fail: string }).fail));
    else engine.completeNode(run_id, key, out);
  };
  return { agent: play('agent', agent), check: play('check', check) };
}

export interface Harness {
  dir: string;
  store: Store;
  engine: Engine;
  topics: Topics;
  hive: FakeHive;
  pushed: RunEvent[];
  close(): void;
}

export function harness(executors: Executors, opts: { store?: StoreOptions; dir?: string; hive?: FakeHive; testSeam?: boolean } = {}): Harness {
  const dir = opts.dir ?? mkdtempSync(join(tmpdir(), 'wf-'));
  const store = new Store(dir, opts.store);
  const hive = opts.hive ?? new FakeHive();
  const pushed: RunEvent[] = [];
  const engine = new Engine({ store, hive, executors, onEvents: (_id, evs) => pushed.push(...evs) });
  const topics = new Topics({ engine, testSeam: opts.testSeam ?? true });
  return {
    dir,
    store,
    engine,
    topics,
    hive,
    pushed,
    close: () => {
      engine.stopFlush();
      try {
        store.close();
      } catch {}
      if (!opts.dir) rmSync(dir, { recursive: true, force: true });
    },
  };
}

/** The reply data of a topic, asserting it succeeded. */
export function call(h: Harness, topic: string, data: Record<string, unknown> = {}): Record<string, unknown> {
  const r = h.topics.handle(topic, { nonce: 'n1', ...data });
  if (!r) throw new Error(`no topic ${topic}`);
  return r.data;
}

export const runState = (h: Harness, id: string): string | undefined => h.store.getRun(id)?.state;
export const rowOf = (h: Harness, id: string, key: string): Row | undefined => h.store.loadRows(id).find((r) => r.key === key);
