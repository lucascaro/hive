// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// @vitest-environment jsdom
//
// The workflow transport and run loader (src/app/workflows.ts): nonce
// correlation over PLUGIN_MESSAGE, and loadRunState's subscribe-first
// merge of run.nodes pages with buffered live events.
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { PluginInfo } from '../../src/app/state.js';
import { examples } from '../../../../../plugins/workflows/tools/examples.ts';
import { deliverPluginMessage } from '../../src/app/plugins.js';
import {
  WorkflowError,
  fetchHistory,
  initWorkflows,
  loadRunState,
  request,
  resetWorkflowsForTest,
  resyncWorkflows,
} from '../../src/app/workflows.js';
import { resetStore, setPlugins } from '../../src/store/store.js';
import {
  resetWorkflowsStore,
  workflowsStore,
} from '../../src/store/workflows.js';
import { aggregate } from '../../src/lib/workflow-overlay.js';
import type { WorkflowsEngine } from '../e2e/workflows-engine.js';
import { flush, until, wireEngine } from './helpers/workflows-harness.js';

const h = await vi.hoisted(async () =>
  (await import('./helpers/bridge-stub.js')).makeHoisted(),
);
vi.mock('../../src/bridge.js', async (orig) =>
  (await import('./helpers/bridge-stub.js')).stubBridge(h, orig),
);
vi.mock('../../src/app/dom.js', () => ({
  flashStatus: vi.fn(),
  setStatus: vi.fn(),
  reportFailure: () => () => {},
}));

const RUNNING: PluginInfo = {
  id: 'workflows',
  name: 'Workflows',
  version: '1.0.0',
  api_version: '0.2',
  source: 'builtin',
  command: [],
  enabled: true,
  status: 'running',
  restarts: 0,
  builtin: true,
};

const ir = examples.fanOutThenVerify();
const KEYS = [
  'split',
  'work/each[0]/do',
  'work/each[1]/do',
  'work/each[2]/do',
  'work/verify',
];

let engine: WorkflowsEngine;

beforeEach(() => {
  resetStore();
  resetWorkflowsStore();
  resetWorkflowsForTest();
  setPlugins([{ ...RUNNING }]);
  h.send.mockReset();
  h.notify.mockReset();
  engine = wireEngine(h);
  initWorkflows({
    setFocusedTile: () => {},
    refocusActiveTerm: () => {},
    switchTo: () => {},
  });
});

const run = (extra: Record<string, unknown> = {}) => ({
  run_id: 'r1',
  workflow: 'fan-out-then-verify',
  ir,
  state: 'running',
  nodes: [
    { key: 'split', node_id: 'split', state: 'done', attempt: 1 },
    ...[0, 1, 2].map((i) => ({
      key: `work/each[${i}]/do`,
      node_id: 'work/each/do',
      state: 'running',
      attempt: 1,
    })),
    {
      key: 'work/verify',
      node_id: 'work/verify',
      state: 'pending',
      attempt: 0,
    },
  ],
  ...extra,
});

const sentNonce = (i = -1): string =>
  JSON.parse(h.send.mock.calls.at(i)?.[2] as string).nonce;

describe('request', () => {
  it('reply settles only its own nonce', async () => {
    h.send.mockImplementation(() => Promise.resolve());
    const a = request<{ n: number }>('workflows.list', {});
    const b = request<{ n: number }>('workflows.list', {});
    const [na, nb] = h.send.mock.calls.map(
      (c) => JSON.parse(c[2] as string).nonce,
    );
    expect(na).not.toBe(nb);
    deliverPluginMessage({
      plugin_id: 'workflows',
      topic: 'workflows.list.reply',
      data: { nonce: nb, ok: true, n: 2 },
    });
    await expect(b).resolves.toMatchObject({ n: 2 });
    deliverPluginMessage({
      plugin_id: 'workflows',
      topic: 'workflows.list.reply',
      data: { nonce: na, ok: true, n: 1 },
    });
    await expect(a).resolves.toMatchObject({ n: 1 });
  });

  it('a reply for another window is ignored', async () => {
    h.send.mockImplementation(() => Promise.resolve());
    const a = request('workflows.list', {}, { timeoutMs: 30 });
    deliverPluginMessage({
      plugin_id: 'workflows',
      topic: 'workflows.list.reply',
      data: { nonce: 'someone-else', ok: true },
    });
    deliverPluginMessage({
      plugin_id: 'other-plugin',
      topic: 'workflows.list.reply',
      data: { nonce: sentNonce(), ok: true },
    });
    await expect(a).rejects.toMatchObject({ code: 'timeout' });
  });

  it('ok false rejects with code, message and type_errors', async () => {
    h.send.mockImplementation(() => Promise.resolve());
    const p = request('workflow.build', {});
    deliverPluginMessage({
      plugin_id: 'workflows',
      topic: 'workflow.build.reply',
      data: {
        nonce: sentNonce(),
        ok: false,
        error: {
          code: 'type_errors',
          message: 'bad',
          type_errors: [{ file: 'a.ts', line: 1, col: 2, message: 'm' }],
        },
      },
    });
    const e = await p.catch((x: unknown) => x);
    expect(e).toBeInstanceOf(WorkflowError);
    expect(e).toMatchObject({
      code: 'type_errors',
      message: 'bad',
      type_errors: [{ file: 'a.ts', line: 1, col: 2, message: 'm' }],
    });
  });

  it('a timeout rejects', async () => {
    h.send.mockImplementation(() => Promise.resolve());
    await expect(request('x', {}, { timeoutMs: 10 })).rejects.toMatchObject({
      code: 'timeout',
    });
  });

  it('a rejected SendPluginMessage settles the request', async () => {
    h.send.mockImplementation(() => Promise.reject(new Error('no control')));
    await expect(request('x', {})).rejects.toThrow('no control');
  });

  it('the budget counts UTF-8 bytes, not code units', async () => {
    // 20,000 characters, 60,000 bytes.
    await expect(
      request('run.start', { input: '\u65e5'.repeat(20_000) }),
    ).rejects.toMatchObject({ code: 'too_large' });
    expect(h.send).not.toHaveBeenCalled();
  });

  it('a payload over 48 KiB is refused locally with too_large and never sent', async () => {
    await expect(
      request('run.start', { input: 'x'.repeat(49 * 1024) }),
    ).rejects.toMatchObject({ code: 'too_large' });
    expect(h.send).not.toHaveBeenCalled();
  });
});

describe('loadRunState', () => {
  beforeEach(() => {
    engine.seed({ pageRows: 2, runs: [run()] });
  });

  const requests = (topic: string) =>
    engine.requests.filter((r) => r.topic === topic);

  it('pages run.nodes via next_after until more is false, and never calls run.events', async () => {
    await loadRunState('r1');
    const w = workflowsStore.getState().run;
    expect(w?.status).toBe('ready');
    const nodeReqs = requests('run.nodes');
    expect(nodeReqs).toHaveLength(3);
    expect(nodeReqs[0].data.after).toBeUndefined();
    expect(nodeReqs[1].data.after).toBe('work/each[0]/do');
    expect(Object.keys(w?.overlay.instances ?? {}).sort()).toEqual(
      [...KEYS].sort(),
    );
    expect(requests('run.events')).toHaveLength(0);
    expect(requests('run.get')).toHaveLength(1);
    // run.get carries no nodes: state came from the pages.
    expect(w?.overlay.instances.split.state).toBe('done');
  });

  it('subscribes before its first request: an event pushed right after the send is not lost', async () => {
    const loading = loadRunState('r1');
    // The run.get request is on the wire; its reply has not come back.
    engine.push('r1', [
      {
        type: 'node.started',
        key: 'work/verify',
        node_id: 'work/verify',
        attempt: 1,
      },
    ]);
    await loading;
    expect(
      workflowsStore.getState().run?.overlay.instances['work/verify'].state,
    ).toBe('running');
  });

  it("events arriving between pages apply only above each page's last_seq", async () => {
    engine.holdNodesPage(2);
    const loading = loadRunState('r1');
    await until(() => engine.heldCount() === 1, 'page 2 held');
    // Page 1 (split + each[0]) was read at seq 0. Both events are above it
    // for its keys; the third is for a key on a page not yet served.
    engine.push('r1', [
      {
        type: 'node.finished',
        key: 'work/each[0]/do',
        node_id: 'work/each/do',
        attempt: 1,
        data: { state: 'done' },
      },
      {
        type: 'node.finished',
        key: 'work/each[1]/do',
        node_id: 'work/each/do',
        attempt: 1,
        data: { state: 'failed', error: 'x' },
      },
      {
        type: 'node.started',
        key: 'work/verify',
        node_id: 'work/verify',
        attempt: 1,
      },
    ]);
    engine.releaseHeld();
    await loading;
    const ov = workflowsStore.getState().run?.overlay;
    const truth = Object.fromEntries(
      engine.truth('r1').map((n) => [n.key, n.state]),
    );
    for (const k of KEYS) expect(ov?.instances[k].state, k).toBe(truth[k]);
    expect(ov?.instances['work/each[1]/do'].state).toBe('failed');
  });

  it('a page with a higher last_seq than an already-buffered event leaves that event unapplied for its keys', async () => {
    engine.holdNodesPage(1);
    const loading = loadRunState('r1');
    await until(() => engine.heldCount() === 1, 'page 1 held');
    // Committed (and pushed) before ANY page is read: the rows already
    // reflect it, so applying the buffered copy again must be a no-op.
    const [e] = engine.push('r1', [
      {
        type: 'node.finished',
        key: 'work/each[0]/do',
        node_id: 'work/each/do',
        attempt: 1,
        data: { state: 'done' },
      },
    ]);
    engine.releaseHeld();
    await loading;
    const ov = workflowsStore.getState().run?.overlay;
    expect(ov?.instances['work/each[0]/do'].state).toBe('done');
    expect(ov?.pageSeq['work/each[0]/do']).toBeGreaterThanOrEqual(e.seq);
  });

  it('a gap reloads run.nodes instead of paging events', async () => {
    await loadRunState('r1');
    const before = requests('run.nodes').length;
    // seq jumps: the engine committed two events the GUI never received.
    const r = engine.runs.get('r1');
    if (!r) throw new Error('no run');
    r.seq += 5;
    engine.push('r1', [
      {
        type: 'node.finished',
        key: 'work/verify',
        node_id: 'work/verify',
        attempt: 1,
        data: { state: 'done' },
      },
    ]);
    await until(() => requests('run.nodes').length > before, 'reload');
    await until(
      () =>
        workflowsStore.getState().run?.overlay.instances['work/verify']
          .state === 'done',
      'verify done',
    );
    expect(requests('run.events')).toHaveLength(0);
    // The reload re-reads the run (a missed run.finished), once more.
    expect(requests('run.get')).toHaveLength(2);
  });

  it('reconnect reloads the open run', async () => {
    await loadRunState('r1');
    const before = requests('run.nodes').length;
    resyncWorkflows();
    // resync only acts while the modal is open on a run.
    await flush(20);
    expect(requests('run.nodes').length).toBe(before);
  });

  it('live events after the load apply directly and idempotently', async () => {
    await loadRunState('r1');
    const [e] = engine.push('r1', [
      {
        type: 'node.finished',
        key: 'work/each[2]/do',
        node_id: 'work/each/do',
        attempt: 1,
        data: { state: 'done' },
      },
    ]);
    const ov1 = workflowsStore.getState().run?.overlay;
    expect(ov1?.instances['work/each[2]/do'].state).toBe('done');
    deliverPluginMessage({
      plugin_id: 'workflows',
      topic: 'run.event',
      data: { run_id: 'r1', events: [e] },
    });
    const ov2 = workflowsStore.getState().run?.overlay;
    expect(ov2?.instances).toEqual(ov1?.instances);
    expect(aggregate(ov2 as NonNullable<typeof ov2>, 'work/each/do')).toBe(
      'running',
    );
  });

  it('fetchHistory pages run.events only when called, keeps one key, last 200', async () => {
    const events = Array.from({ length: 5 }, (_, i) => ({
      type: 'agent.message',
      key: 'work/each[0]/do',
      node_id: 'work/each/do',
      attempt: 1,
      data: { session_id: 's', item_id: `i${i}`, text: `m${i}` },
    }));
    engine.seed({
      pageRows: 2,
      runs: [
        run({
          events: [
            ...events,
            {
              type: 'agent.message',
              key: 'work/each[1]/do',
              node_id: 'work/each/do',
              attempt: 1,
              data: { text: 'other' },
            },
          ],
        }),
      ],
    });
    await loadRunState('r1');
    expect(requests('run.events')).toHaveLength(0);
    await fetchHistory('r1', 'work/each[0]/do');
    expect(requests('run.events').length).toBeGreaterThan(0);
    const hist = workflowsStore.getState().history;
    expect(hist?.status).toBe('ready');
    expect(hist?.events.map((e) => (e.data as { text: string }).text)).toEqual([
      'm0',
      'm1',
      'm2',
      'm3',
      'm4',
    ]);
  });
});
