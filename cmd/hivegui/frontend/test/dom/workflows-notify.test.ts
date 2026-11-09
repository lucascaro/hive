// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// @vitest-environment jsdom
//
// The waiting tracker and its daemon-event hooks (src/app/workflows.ts,
// wired in src/app/events.ts): the boot scan and banner, the human-wait
// notification and bell-click routing, and the plugin_not_running claim.
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { examples } from '../../../../../plugins/workflows/tools/examples.ts';
import type { WorkflowsEngine } from '../e2e/workflows-engine.js';
import { until, wireEngine } from './helpers/workflows-harness.js';

const h = await vi.hoisted(async () =>
  (await import('./helpers/bridge-stub.js')).makeHoisted(),
);
vi.mock('../../src/bridge.js', async (orig) =>
  (await import('./helpers/bridge-stub.js')).stubBridge(h, orig),
);
const flashStatus = vi.fn();
vi.mock('../../src/app/dom.js', async (orig) => ({
  ...(await orig<typeof import('../../src/app/dom.js')>()),
  flashStatus: (...a: unknown[]) => flashStatus(...a),
}));

type M = {
  store: typeof import('../../src/store/store.js');
  wf: typeof import('../../src/store/workflows.js');
  app: typeof import('../../src/app/workflows.js');
};
let m: M;
let engine: WorkflowsEngine;
const switchTo = vi.fn();

beforeAll(async () => {
  document.body.innerHTML =
    '<div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>';
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  const events = await import('../../src/app/events.js');
  m = {
    store: await import('../../src/store/store.js'),
    wf: await import('../../src/store/workflows.js'),
    app: await import('../../src/app/workflows.js'),
  };
  events.wireDaemonEvents({
    switchTo: (id: string) => switchTo(id),
    renderAll: () => {},
    setFocusedTile: () => {},
    refocusActiveTerm: () => {},
    isDaemonRestarting: () => false,
    checkForUpdates: () => {},
  } as unknown as Parameters<typeof events.wireDaemonEvents>[0]);
});

const deliver = (name: string, payload: unknown) => {
  const fn = h.handlers.get(name);
  if (!fn) throw new Error(`no handler for ${name}`);
  fn(typeof payload === 'string' ? payload : JSON.stringify(payload));
};

const PLUGIN = {
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

const ir = examples.reviewLoop();
const run = (extra: Record<string, unknown> = {}) => ({
  run_id: 'r1',
  project_id: 'p1',
  workflow: 'Review loop',
  ir,
  state: 'running',
  nodes: [{ key: 'approve', node_id: 'approve', state: 'waiting', attempt: 1 }],
  events: [
    {
      type: 'node.waiting',
      key: 'approve',
      node_id: 'approve',
      attempt: 1,
      data: {
        reason: 'human',
        prompt: 'ok?',
        output_schema: {
          type: 'object',
          required: [],
          additionalProperties: false,
          properties: {},
        },
      },
    },
  ],
  ...extra,
});

beforeEach(() => {
  m.store.resetStore();
  m.wf.resetWorkflowsStore();
  m.app.resetWorkflowsForTest();
  h.send.mockReset();
  h.notify.mockReset();
  h.notify.mockResolvedValue(undefined);
  flashStatus.mockClear();
  switchTo.mockClear();
  engine = wireEngine(h);
  m.app.initWorkflows({
    setFocusedTile: () => {},
    refocusActiveTerm: () => {},
    switchTo: (id) => switchTo(id),
  });
  m.store.setProjects([
    { id: 'p1', name: 'hive', color: '#888', cwd: '/work/p1' } as never,
  ]);
});

const bootRunning = () => deliver('plugin:list', { plugins: [PLUGIN] });
const banner = () => m.store.appStore.getState().banners.workflow;

describe('waiting scan and banner', () => {
  it('the boot waiting scan is one runs.list state waiting call and raises the banner without notifying', async () => {
    engine.seed({ runs: [run()] });
    bootRunning();
    await until(() => banner().visible, 'banner');
    const lists = engine.requests.filter((r) => r.topic === 'runs.list');
    expect(lists).toHaveLength(1);
    expect(lists[0].data).toMatchObject({ state: 'waiting' });
    expect(banner().text).toBe(
      'Workflow "Review loop" in hive is waiting on you.',
    );
    expect(h.notify).not.toHaveBeenCalled();
  });

  it('no waiting run keeps the banner hidden, and a plugin that stops clears it', async () => {
    engine.seed({ runs: [run()] });
    bootRunning();
    await until(() => banner().visible, 'banner');
    deliver('plugin:event', {
      kind: 'updated',
      plugin: { ...PLUGIN, status: 'stopped' },
    });
    expect(banner().visible).toBe(false);
  });

  it('the banner Open action opens the modal on that run', async () => {
    engine.seed({ runs: [run()] });
    bootRunning();
    await until(() => banner().visible, 'banner');
    m.app.openWaitingWorkflow();
    await until(() => m.store.isModalOpen('workflows'), 'modal');
    expect(m.wf.workflowsStore.getState()).toMatchObject({
      view: 'run',
      runId: 'r1',
    });
  });
});

describe('live waits', () => {
  const seedQuiet = () => {
    engine.seed({
      runs: [
        run({
          nodes: [
            {
              key: 'approve',
              node_id: 'approve',
              state: 'running',
              attempt: 1,
            },
          ],
          events: [],
        }),
      ],
    });
    bootRunning();
  };

  it('a live human node.waiting calls Notify with tag wf:<run_id>:<key>', async () => {
    seedQuiet();
    await until(
      () => engine.requests.some((r) => r.topic === 'runs.list'),
      'scan',
    );
    engine.push('r1', [
      {
        type: 'node.waiting',
        key: 'approve',
        node_id: 'approve',
        attempt: 1,
        data: {
          reason: 'human',
          prompt: 'ok?',
          output_schema: { type: 'object' },
        },
      },
    ]);
    await until(() => h.notify.mock.calls.length > 0, 'notify');
    expect(h.notify).toHaveBeenCalledWith(
      'Review loop',
      'hive',
      expect.stringContaining('click to open'),
      'wf:r1:approve',
    );
    await until(() => banner().visible, 'banner');
  });

  it('a live permission node.waiting raises the banner but does not call Notify from the workflow layer', async () => {
    seedQuiet();
    await until(
      () => engine.requests.some((r) => r.topic === 'runs.list'),
      'scan',
    );
    engine.push('r1', [
      {
        type: 'node.waiting',
        key: 'approve',
        node_id: 'approve',
        attempt: 1,
        data: {
          reason: 'permission',
          session_id: 's1',
          request_id: 'q',
          title: 'Run it?',
          options: [],
        },
      },
    ]);
    await until(() => banner().visible, 'banner');
    expect(h.notify).not.toHaveBeenCalled();
  });
});

describe('bell-click', () => {
  it('a wf: tag opens the run with that key selected', async () => {
    engine.seed({ runs: [run()] });
    deliver('bell-click', 'wf:r1:approve');
    await until(() => m.store.isModalOpen('workflows'), 'modal');
    expect(m.wf.workflowsStore.getState()).toMatchObject({
      view: 'run',
      runId: 'r1',
      selectedKey: 'approve',
    });
    expect(switchTo).not.toHaveBeenCalled();
  });

  it('a session id still switches to the session', () => {
    m.store.setSessions([
      { id: 's9', name: 'x', project_id: 'p1', state: 'idle' } as never,
    ]);
    deliver('bell-click', 's9');
    expect(switchTo).toHaveBeenCalledWith('s9');
    expect(m.store.isModalOpen('workflows')).toBe(false);
  });

  it('a wf: tag for a run in no open project says so instead of opening', async () => {
    engine.seed({ runs: [run({ project_id: 'gone' })] });
    deliver('bell-click', 'wf:r1:approve');
    await until(() => flashStatus.mock.calls.length > 0, 'status');
    expect(m.store.isModalOpen('workflows')).toBe(false);
  });
});

describe('plugin_not_running', () => {
  it('rejects every pending request and is claimed, so the generic error line stays quiet', async () => {
    h.send.mockImplementation(() => Promise.resolve());
    const a = m.app.request('workflows.list', {});
    const b = m.app.request('runs.list', {});
    deliver('control:error', {
      code: 'plugin_not_running',
      message: 'not running',
    });
    await expect(a).rejects.toMatchObject({ code: 'plugin_not_running' });
    await expect(b).rejects.toMatchObject({ code: 'plugin_not_running' });
    expect(flashStatus).not.toHaveBeenCalled();
  });

  it('with nothing pending it is not claimed', async () => {
    deliver('control:error', {
      code: 'plugin_not_running',
      message: 'not running',
    });
    await new Promise((r) => setTimeout(r, 0));
    expect(flashStatus.mock.calls.map((c) => c[0]).join('\n')).toContain(
      'plugin_not_running',
    );
  });
});
