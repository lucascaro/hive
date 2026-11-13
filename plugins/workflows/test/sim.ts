// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// A synchronous driver for the pure reducer, for table-driven tests: no store,
// no processes, nothing async. Whatever a node would do, the test scripts.

import type { IR, Node } from '../sdk/dsl.ts';
import { apply, type Applied, initialRows, type Model, modelFor, step, type Transition } from '../engine/interp.ts';
import type { EventDraft } from '../engine/events.ts';
import type { Row } from '../engine/model.ts';

export type Outcome = { output: unknown } | { fail: string } | 'hang';

export interface Script {
  /** What an agent or check node does when it starts. Defaults to failing, so an unscripted node is noticed. */
  node?: (key: string, node: Node, m: Model) => Outcome;
  /** The answer a waiting human gives, or undefined to leave it waiting. */
  human?: (key: string, node: Node, m: Model) => unknown;
}

export interface Sim {
  m: Model;
  events: EventDraft[];
  /** The key of every node started, in order. */
  started: string[];
  commit(t: Transition): Applied;
  run(): void;
}

export function sim(ir: IR, input: Record<string, unknown>, script: Script = {}): Sim {
  const m = modelFor(ir, input, initialRows(ir));
  const events: EventDraft[] = [];
  const started: string[] = [];
  const commit = (t: Transition): Applied => {
    const a = apply(m, t, { now: () => '2026-01-01T00:00:00.000Z' });
    for (const r of a.change.rows) m.rows.set(r.key, r);
    if (a.change.run?.state) m.state = a.change.run.state;
    events.push(...a.change.events);
    return a;
  };
  const row = (key: string): Row => m.rows.get(key) as Row;
  const hung = new Set<string>();

  const run = (): void => {
    for (let guard = 0; guard < 5000; guard++) {
      const due = step(m, hung);
      for (const t of due.transitions) commit(t);
      for (const a of due.actions) {
        const node = m.ix.byId.get(a.node_id)?.node as Node;
        if (a.a === 'agent') commit({ t: 'turn', key: a.key });
        if (row(a.key).state === 'failed') continue;
        commit({ t: 'start', key: a.key });
        started.push(a.key);
        const out = script.node ? script.node(a.key, node, m) : { fail: `no script for ${a.key}` };
        if (out === 'hang') hung.add(a.key);
        else if ('fail' in out) commit({ t: 'fail', key: a.key, error: out.fail });
        else commit({ t: 'finish', key: a.key, output: out.output });
      }
      let answered = false;
      for (const r of m.rows.values()) {
        if (r.state !== 'waiting') continue;
        const node = m.ix.byId.get(r.node_id)?.node as Node;
        const answer = node.kind === 'human' ? script.human?.(r.key, node, m) : undefined;
        if (answer !== undefined) {
          commit({ t: 'finish', key: r.key, output: answer });
          answered = true;
        }
      }
      if (due.transitions.length === 0 && due.actions.length === 0 && !answered) return;
    }
    throw new Error('sim did not settle');
  };
  return { m, events, started, commit, run };
}

export const state = (s: Sim, key: string): string | undefined => s.m.rows.get(key)?.state;
export const out = (s: Sim, key: string): unknown => s.m.rows.get(key)?.output_raw;
export const keys = (s: Sim): string[] => [...s.m.rows.keys()].sort();
