// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { describe, expect, it } from 'vitest';
import { examples } from '../../../../../plugins/workflows/tools/examples.ts';
import {
  type OverlayState,
  aggregate,
  applyPage,
  displayRunState,
  emptyOverlay,
  fold,
  hasGap,
  mapProgress,
  waitingKeys,
} from '../../src/lib/workflow-overlay.js';
import type { NodeRow, RunEvent } from '../../src/lib/workflow-types.js';

const RUN = 'r1';
const loopIr = examples.reviewLoop();
const mapIr = examples.fanOutThenVerify();

let seq = 0;
const ev = (
  type: string,
  key: string | null,
  data: Record<string, unknown> = {},
  attempt = 1,
  s = ++seq,
): RunEvent => ({
  run_id: RUN,
  seq: s,
  ts: '2026-11-09T12:00:00Z',
  key,
  node_id: key,
  attempt,
  type,
  data,
});

const row = (key: string, state: NodeRow['state'], attempt = 1): NodeRow => ({
  key,
  node_id: key.replace(/\[\d+\]/g, ''),
  state,
  attempt,
});

const page = (nodes: NodeRow[], last_seq: number) => ({ nodes, last_seq });

describe('fold', () => {
  it('node.* events drive instance state', () => {
    let s = emptyOverlay(RUN, 'running');
    s = fold(
      s,
      [
        ev('node.ready', 'split', {}, 0, 1),
        ev('node.started', 'split', {}, 1, 2),
        ev('node.finished', 'split', { state: 'done' }, 1, 3),
      ],
      mapIr,
    );
    expect(s.instances.split).toEqual({ state: 'done', attempt: 1 });
    expect(s.maxSeq).toBe(3);
  });

  it('human node.waiting carries prompt and output_schema', () => {
    const waiting = {
      reason: 'human',
      prompt: 'ok?',
      output_schema: { type: 'object' },
    };
    const s = fold(
      emptyOverlay(RUN, 'running'),
      [ev('node.waiting', 'approve', waiting, 1, 1)],
      mapIr,
    );
    expect(s.instances.approve.waiting).toEqual(waiting);
    expect(waitingKeys(s)).toEqual(['approve']);
    expect(displayRunState(s)).toBe('waiting');
  });

  it('permission node.waiting then node.resumed clears the wait', () => {
    let s = fold(
      emptyOverlay(RUN, 'running'),
      [
        ev(
          'node.waiting',
          'n',
          {
            reason: 'permission',
            session_id: 's',
            request_id: 'q',
            title: 't',
            options: [],
          },
          1,
          1,
        ),
      ],
      mapIr,
    );
    expect(s.instances.n.state).toBe('waiting');
    s = fold(
      s,
      [ev('node.resumed', 'n', { reason: 'permission' }, 1, 2)],
      mapIr,
    );
    expect(s.instances.n).toEqual({ state: 'running', attempt: 1 });
    expect(displayRunState(s)).toBe('running');
  });

  it('node.finished conflict and error are kept on the instance', () => {
    const conflict = { branch: 'b', files: ['x'], worktree: '/w' };
    const s = fold(
      emptyOverlay(RUN, 'running'),
      [
        ev(
          'node.finished',
          'n',
          { state: 'failed', error: 'boom', conflict },
          1,
          1,
        ),
      ],
      mapIr,
    );
    expect(s.instances.n).toMatchObject({
      state: 'failed',
      error: 'boom',
      conflict,
    });
  });

  it('node.reset returns an instance to pending with attempt cleared', () => {
    let s = fold(
      emptyOverlay(RUN, 'failed'),
      [ev('node.finished', 'n', { state: 'failed', error: 'x' }, 2, 1)],
      mapIr,
    );
    s = fold(
      s,
      [
        ev('node.reset', 'n', {}, 0, 2),
        ev('run.retried', null, { reset: 1 }, 0, 3),
      ],
      mapIr,
    );
    expect(s.instances.n).toEqual({ state: 'pending', attempt: 0 });
    expect(s.runState).toBe('running');
  });

  it('events of another run_id are ignored', () => {
    const other = { ...ev('node.started', 'n', {}, 1, 1), run_id: 'other' };
    const s = fold(emptyOverlay(RUN, 'running'), [other], mapIr);
    expect(s.instances).toEqual({});
    expect(s.maxSeq).toBe(0);
  });

  it('run.finished then run.retried in seq order ends running whatever order run.get was read in', () => {
    const events = [
      ev('run.retried', null, { reset: 0 }, 0, 11),
      ev('run.finished', null, { state: 'failed' }, 0, 10),
    ];
    for (const initial of ['failed', 'running', 'done'] as const) {
      const s = fold(emptyOverlay(RUN, initial), events, mapIr);
      expect(s.runState).toBe('running');
    }
  });

  it('a batch past maxSeq + 1 reports a gap', () => {
    const s = applyPage(
      emptyOverlay(RUN, 'running'),
      page([row('a', 'done')], 10),
      mapIr,
    );
    expect(hasGap(s, [ev('node.started', 'a', {}, 1, 11)])).toBe(false);
    expect(hasGap(s, [ev('node.started', 'a', {}, 1, 9)])).toBe(false);
    expect(hasGap(s, [ev('node.started', 'a', {}, 1, 13)])).toBe(true);
    // seq starts at 1, so a first batch past it is a gap even on a blank run.
    expect(
      hasGap(emptyOverlay(RUN), [ev('node.started', 'a', {}, 1, 13)]),
    ).toBe(true);
    expect(hasGap(emptyOverlay(RUN), [ev('node.started', 'a', {}, 1, 1)])).toBe(
      false,
    );
  });
});

describe('pages and events', () => {
  it('applyPage replaces the page keys and records pageSeq', () => {
    let s = fold(
      emptyOverlay(RUN, 'running'),
      [ev('node.started', 'a', {}, 1, 1)],
      mapIr,
    );
    s = applyPage(
      s,
      page([row('a', 'done', 2), row('b', 'running')], 7),
      mapIr,
    );
    expect(s.instances.a).toEqual({ state: 'done', attempt: 2 });
    expect(s.pageSeq).toMatchObject({ a: 7, b: 7 });
    expect(s.maxSeq).toBe(7);
  });

  it('an event with seq <= its key pageSeq is a no-op', () => {
    let s = applyPage(
      emptyOverlay(RUN, 'running'),
      page([row('a', 'done')], 7),
      mapIr,
    );
    const before = s.instances.a;
    s = fold(
      s,
      [ev('node.started', 'a', {}, 1, 7), ev('node.reset', 'a', {}, 0, 5)],
      mapIr,
    );
    expect(s.instances.a).toBe(before);
  });

  it('an event with seq > pageSeq applies once, and applying it twice is a no-op', () => {
    let s = applyPage(
      emptyOverlay(RUN, 'running'),
      page([row('a', 'running')], 7),
      mapIr,
    );
    const e = ev('node.finished', 'a', { state: 'done' }, 1, 8);
    s = fold(s, [e], mapIr);
    const once = s;
    expect(s.instances.a.state).toBe('done');
    s = fold(s, [e], mapIr);
    expect(s.instances).toEqual(once.instances);
    expect(s.pageSeq.a).toBe(8);
  });

  it('rows-then-events equals the state of folding the same events onto empty rows', () => {
    const events = [
      ev('node.ready', 'split', {}, 0, 1),
      ev('node.started', 'split', {}, 1, 2),
      ev('node.finished', 'split', { state: 'done' }, 1, 3),
      ev('node.started', 'work/each[0]/do', {}, 1, 4),
      ev('node.started', 'work/each[1]/do', {}, 1, 5),
      ev('node.finished', 'work/each[0]/do', { state: 'done' }, 1, 6),
      ev(
        'node.finished',
        'work/each[1]/do',
        { state: 'failed', error: 'e' },
        1,
        7,
      ),
    ];
    const incremental = fold(emptyOverlay(RUN, 'running'), events, mapIr);
    // The engine's rows after event 5, then events 6 and 7 on top.
    let materialized = applyPage(
      emptyOverlay(RUN, 'running'),
      page(
        [
          row('split', 'done'),
          row('work/each[0]/do', 'running'),
          row('work/each[1]/do', 'running'),
        ],
        5,
      ),
      mapIr,
    );
    materialized = fold(materialized, events.slice(5), mapIr);
    expect(materialized.instances).toEqual(incremental.instances);
  });

  it('a row created after its page was read is created by the event', () => {
    let s = applyPage(
      emptyOverlay(RUN, 'running'),
      page([row('a', 'done')], 3),
      mapIr,
    );
    s = fold(s, [ev('node.started', 'late', {}, 1, 4)], mapIr);
    expect(s.instances.late.state).toBe('running');
  });
});

describe('loops, maps and aggregates', () => {
  const key = (n: number) => `work/each[${n}]/do`;

  it('map copies aggregate under the template as k of n done', () => {
    let s: OverlayState = emptyOverlay(RUN, 'running');
    s = fold(
      s,
      [
        ev('map.expanded', 'work/each', { count: 5, concurrency: 4 }, 0, 1),
        ev('node.finished', key(0), { state: 'done' }, 1, 2),
        ev('node.finished', key(1), { state: 'done' }, 1, 3),
        ev('node.started', key(2), {}, 1, 4),
      ],
      mapIr,
    );
    expect(mapProgress(s, 'work/each')).toEqual({ done: 2, count: 5 });
    expect(aggregate(s, 'work/each/do')).toBe('running');
  });

  it('loop.iteration sets round k of N, and iteration derives from keys plus IR', () => {
    let s = emptyOverlay(RUN, 'running');
    s = fold(
      s,
      [ev('loop.iteration', 'fix', { iteration: 2, max_iters: 3 }, 0, 1)],
      loopIr,
    );
    expect(s.loops.fix).toEqual({ iteration: 2, max_iters: 3 });
    // Rows alone: the highest [k] under the loop, max_iters from the IR.
    const fromRows = applyPage(
      emptyOverlay(RUN, 'running'),
      page([row('fix[1]/work', 'done'), row('fix[2]/work', 'running')], 4),
      loopIr,
    );
    expect(fromRows.loops.fix).toEqual({ iteration: 2, max_iters: 3 });
  });

  it('map count derives from the distinct copy indexes in the rows', () => {
    const s = applyPage(
      emptyOverlay(RUN, 'running'),
      page(
        [row(key(0), 'done'), row(key(1), 'running'), row(key(3), 'pending')],
        4,
      ),
      mapIr,
    );
    expect(s.maps['work/each']).toEqual({ count: 3, concurrency: 4 });
  });

  it('aggregate ranks waiting over running over failed', () => {
    let s = emptyOverlay(RUN, 'running');
    s = applyPage(
      s,
      page(
        [
          row(key(0), 'done'),
          row(key(1), 'failed'),
          row(key(2), 'running'),
          row(key(3), 'waiting'),
        ],
        9,
      ),
      mapIr,
    );
    expect(aggregate(s, 'work/each/do')).toBe('waiting');
    s = applyPage(s, page([row(key(3), 'done')], 10), mapIr);
    expect(aggregate(s, 'work/each/do')).toBe('running');
    s = applyPage(s, page([row(key(2), 'done')], 11), mapIr);
    expect(aggregate(s, 'work/each/do')).toBe('failed');
    expect(aggregate(s, 'never-started')).toBe('pending');
  });
});
