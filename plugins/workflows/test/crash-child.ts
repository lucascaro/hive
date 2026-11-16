// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// A child process for store.test.ts: opens the store, commits one transition,
// then starts a second and kills itself with SIGKILL inside its transaction,
// after the node rows are written and before the events are. What the parent
// finds on reopen is what a real crash would leave.

import { Store } from '../engine/store.ts';
import type { Row } from '../engine/model.ts';

const dir = process.argv[2];
let armed = false;
const store = new Store(dir, {
  afterRows: () => {
    if (armed) process.kill(process.pid, 'SIGKILL');
  },
});

const row = (state: Row['state'], extra: Partial<Row> = {}): Row => ({ key: 'a', node_id: 'a', state, attempt: 1, ready: true, progress: 0, turns: 0, ...extra });
store.createRun(
  { run_id: 'r1', project_id: 'p', workflow: 'w', file: 'w.ts', source_hash: 'h', ir: '{}', input: '{}' },
  [row('pending', { attempt: 0 })],
  [{ key: null, node_id: null, attempt: 0, type: 'run.started', data: {} }],
);
store.commit('r1', { rows: [row('running')], events: [{ key: 'a', node_id: 'a', attempt: 1, type: 'node.started', data: { kind: 'check' } }] });
armed = true;
store.commit('r1', {
  rows: [row('done', { output_raw: { ok: true }, output: { ok: true } })],
  events: [
    { key: 'a', node_id: 'a', attempt: 1, type: 'node.output', data: { output: { ok: true } } },
    { key: 'a', node_id: 'a', attempt: 1, type: 'node.finished', data: { state: 'done' } },
  ],
  run: { state: 'done' },
});
console.log('not reached: the commit should have killed this process');
