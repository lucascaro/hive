// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// @vitest-environment jsdom
//
// The Workflows modal (src/components/modals/Workflows.tsx) against the
// scripted engine: the plugin gate, list, trust, review-and-start and the
// run view, with the real key pipeline for y / n / Esc / the opener chord.
import {
  act,
  cleanup,
  fireEvent,
  render,
  waitFor,
  within,
} from '@testing-library/react';
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest';
import type { PluginInfo } from '../../src/app/state.js';
import { examples } from '../../../../../plugins/workflows/tools/examples.ts';
import type { WorkflowsEngine } from '../e2e/workflows-engine.js';
import { until, wireEngine } from './helpers/workflows-harness.js';

const h = await vi.hoisted(async () =>
  (await import('./helpers/bridge-stub.js')).makeHoisted(),
);
vi.mock('../../src/bridge.js', async (orig) =>
  (await import('./helpers/bridge-stub.js')).stubBridge(h, orig),
);

const MARKUP = `
  <div id="terms"></div><ul id="projects"></ul>
  <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
  <div id="app">
    <div id="workflows" class="hv-dialog hidden" role="dialog" aria-modal="true"
      aria-labelledby="workflows-title"></div>
    <div id="choice-dialog" class="hv-dialog hidden" role="alertdialog"
      aria-modal="true" aria-labelledby="choice-dialog-title"></div>
  </div>`;

type Mods = {
  store: typeof import('../../src/store/store.js');
  wfStore: typeof import('../../src/store/workflows.js');
  app: typeof import('../../src/app/workflows.js');
  keyboard: typeof import('../../src/app/keyboard.js');
  Workflows: typeof import('../../src/components/modals/Workflows.js')['Workflows'];
  ChoiceDialog: typeof import('../../src/components/modals/ChoiceDialog.js')['ChoiceDialog'];
};
let m: Mods;
let engine: WorkflowsEngine;

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  // The key pipeline pulls in the grid, which observes its container.
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  m = {
    store: await import('../../src/store/store.js'),
    wfStore: await import('../../src/store/workflows.js'),
    app: await import('../../src/app/workflows.js'),
    keyboard: await import('../../src/app/keyboard.js'),
    ...(await import('../../src/components/modals/Workflows.js')),
    ...(await import('../../src/components/modals/ChoiceDialog.js')),
  };
});

const el = (id: string) => document.getElementById(id) as HTMLElement;

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

const ir = examples.reviewLoop();
const BUILD = {
  name: 'review-loop',
  ir,
  input_schema: ir.input,
  source_hash: 'h1',
};
const RUN = {
  run_id: 'r1',
  workflow: 'review-loop',
  ir,
  state: 'running',
  nodes: [
    { key: 'fix[1]/work', node_id: 'fix/work', state: 'done', attempt: 1 },
    {
      key: 'fix[1]/review',
      node_id: 'fix/review',
      state: 'running',
      attempt: 1,
    },
  ],
};

let refocus: () => void;
const unmounts: Array<() => void> = [];

beforeEach(() => {
  m.store.resetStore();
  m.wfStore.resetWorkflowsStore();
  m.app.resetWorkflowsForTest();
  h.send.mockReset();
  h.notify.mockReset();
  h.setPluginEnabled.mockReset();
  h.setPluginEnabled.mockResolvedValue(undefined);
  engine = wireEngine(h);
  refocus = vi.fn();
  m.app.initWorkflows({
    setFocusedTile: () => {},
    refocusActiveTerm: refocus,
    switchTo: () => {},
  });
  m.store.setProjects([
    { id: 'p1', name: 'hive', color: '#888', cwd: '/work/p1' } as never,
  ]);
  m.store.setPlugins([{ ...RUNNING }]);
  unmounts.push(
    render(<m.Workflows root={el('workflows')} />, {
      container: el('workflows'),
    }).unmount,
    render(<m.ChoiceDialog root={el('choice-dialog')} />, {
      container: el('choice-dialog'),
    }).unmount,
  );
});

afterEach(() => {
  act(() => {
    m.app.closeWorkflows();
  });
  cleanup();
  unmounts.length = 0;
});

const open = async () => {
  await act(async () => {
    m.app.openWorkflows(m.store.appStore.getState().projects[0]);
  });
};
const dialog = () => within(el('workflows'));
const key = (init: KeyboardEventInit, mac = false) => {
  const e = new KeyboardEvent('keydown', {
    bubbles: true,
    cancelable: true,
    ...init,
  });
  act(() => {
    m.keyboard.dispatchKey(e, mac);
  });
  return e;
};
const isOpen = () => m.store.isModalOpen('workflows');

describe('plugin gate', () => {
  it('absent plugin says update Hive', async () => {
    m.store.setPlugins([]);
    await open();
    expect(
      dialog().getByText('This Hive has no workflow engine.'),
    ).toBeTruthy();
    expect(dialog().getByText(/Update Hive/)).toBeTruthy();
    expect(h.send).not.toHaveBeenCalled();
  });

  it('off plugin offers Turn on and calls SetPluginEnabled', async () => {
    m.store.setPlugins([{ ...RUNNING, enabled: false, status: 'stopped' }]);
    await open();
    fireEvent.click(dialog().getByRole('button', { name: 'Turn on' }));
    expect(h.setPluginEnabled).toHaveBeenCalledWith('workflows', true);
  });

  it('refused shows status_detail with the required Node version and Restart', async () => {
    const detail =
      'requires Node.js 26.0.0 or newer (found v24.11.0 at /usr/local/bin/node)';
    m.store.setPlugins([
      { ...RUNNING, status: 'refused', status_detail: detail },
    ]);
    await open();
    expect(dialog().getByText(detail)).toBeTruthy();
    fireEvent.click(dialog().getByRole('button', { name: 'Restart' }));
    expect(h.setPluginEnabled).toHaveBeenCalledWith('workflows', true);
  });

  it('failed shows Restart', async () => {
    m.store.setPlugins([
      { ...RUNNING, status: 'failed', status_detail: 'exited 1' },
    ]);
    await open();
    expect(dialog().getByText('exited 1')).toBeTruthy();
    expect(dialog().getByRole('button', { name: 'Restart' })).toBeTruthy();
  });

  it('enabled but stopped shows the starting spinner', async () => {
    m.store.setPlugins([{ ...RUNNING, status: 'stopped' }]);
    await open();
    expect(dialog().getByText(/Starting the workflow engine/)).toBeTruthy();
  });
});

describe('list', () => {
  it('shows the trust chip, files and recent runs', async () => {
    engine.seed({
      trust: 'trusted',
      workflows: [
        { file: 'review-loop.ts', name: 'Review loop' },
        { file: 'plan.ts' },
      ],
      runs: [{ ...RUN, source_changed: true }],
    });
    await open();
    await dialog().findByText('review-loop.ts');
    expect(dialog().getByText('trusted')).toBeTruthy();
    expect(dialog().getByText('Review loop')).toBeTruthy();
    expect(dialog().getByText('(not built yet)')).toBeTruthy();
    // source_changed chip on the run row.
    const row = await dialog().findByText(/\(source changed\)/);
    expect(row).toBeTruthy();
  });

  it('untrusted list offers Review & trust and no file opens', async () => {
    engine.seed({
      trust: 'none',
      workflows: [{ file: 'review-loop.ts', name: 'Review loop' }],
    });
    await open();
    const rowBtn = (await dialog().findByText('review-loop.ts')).closest(
      'button',
    );
    expect(rowBtn?.disabled).toBe(true);
    expect(dialog().getByText('not trusted')).toBeTruthy();
    expect(
      dialog().getByRole('button', { name: 'Review & trust…' }),
    ).toBeTruthy();
    fireEvent.click(rowBtn as HTMLElement);
    expect(m.wfStore.workflowsStore.getState().view).toBe('list');
  });

  it('Refresh asks again', async () => {
    engine.seed({ trust: 'trusted', workflows: [] });
    await open();
    await dialog().findByText('No workflow files in this folder.');
    const before = engine.requests.filter(
      (r) => r.topic === 'workflows.list',
    ).length;
    fireEvent.click(dialog().getByRole('button', { name: 'Refresh' }));
    await until(
      () =>
        engine.requests.filter((r) => r.topic === 'workflows.list').length >
        before,
      'refresh',
    );
  });
});

const toTrust = async (seed: Parameters<WorkflowsEngine['seed']>[0] = {}) => {
  engine.seed({
    trust: 'none',
    folder: '/Users/me/hive/.hive/workflows',
    files: [
      { path: 'review-loop.ts', bytes: 2150 },
      { path: 'fan-out.ts', bytes: 1400 },
    ],
    workflows: [{ file: 'review-loop.ts' }],
    builds: { 'review-loop.ts': BUILD },
    ...seed,
  });
  await open();
  fireEvent.click(
    await dialog().findByRole('button', { name: 'Review & trust…' }),
  );
  await dialog().findByRole('button', { name: 'Cancel' });
};

describe('trust view', () => {
  it('shows folder, hash, files, consequences and the hints', async () => {
    await toTrust();
    await dialog().findByText('review-loop.ts');
    expect(dialog().getByText('/Users/me/hive/.hive/workflows')).toBeTruthy();
    expect(dialog().getByText('a1b2c3d4e5f6')).toBeTruthy();
    expect(dialog().getByText('2.1 KB')).toBeTruthy();
    expect(
      dialog().getByText(/defence in depth, not a security boundary/),
    ).toBeTruthy();
    expect(dialog().getByText(/Nothing has been built yet/)).toBeTruthy();
    const hints = dialog()
      .getAllByText(/^(\(y\)|\(n\)\/\[esc\])$/)
      .map((k) => k.nextSibling?.textContent);
    expect(el('workflows').textContent).toContain('(y)');
    expect(el('workflows').textContent).toContain('(n)/[esc]');
    expect(hints.length).toBe(2);
  });

  it('opens with focus on Cancel and Trust styled danger', async () => {
    await toTrust();
    await dialog().findByRole('button', { name: 'Trust and continue' });
    expect(document.activeElement).toBe(
      dialog().getByRole('button', { name: 'Cancel' }),
    );
    expect(
      dialog().getByRole('button', { name: 'Trust and continue' }).dataset.kind,
    ).toBe('danger');
  });

  it('y confirms', async () => {
    await toTrust({ builds: { 'review-loop.ts': BUILD } });
    await dialog().findByRole('button', { name: 'Trust and continue' });
    key({ key: 'y', code: 'KeyY' });
    await until(() => engine.trust === 'trusted', 'trust');
    await waitFor(() =>
      expect(m.wfStore.workflowsStore.getState().view).toBe('list'),
    );
  });

  it('n and Escape cancel without trusting', async () => {
    for (const k of [
      { key: 'n', code: 'KeyN' },
      { key: 'Escape', code: 'Escape' },
    ]) {
      await toTrust();
      await dialog().findByRole('button', { name: 'Trust and continue' });
      key(k);
      expect(m.wfStore.workflowsStore.getState().view).toBe('list');
      expect(engine.trust).toBe('none');
      expect(isOpen()).toBe(true);
      act(() => m.app.closeWorkflows());
      await open();
    }
  });

  it('Enter is left to the browser: it activates the focused button, Cancel until Tab', async () => {
    await toTrust();
    const trust = await dialog().findByRole('button', {
      name: 'Trust and continue',
    });
    const cancel = dialog().getByRole('button', { name: 'Cancel' });
    expect(document.activeElement).toBe(cancel);
    // No scope binds Enter, so the key pipeline leaves the event alone and
    // the browser clicks whichever button has focus.
    expect(key({ key: 'Enter', code: 'Enter' }).defaultPrevented).toBe(false);
    trust.focus();
    expect(key({ key: 'Enter', code: 'Enter' }).defaultPrevented).toBe(false);
    expect(document.activeElement).toBe(trust);
    expect(engine.trust).toBe('none');
    // Clicking Cancel goes back; nothing was trusted.
    fireEvent.click(cancel);
    expect(m.wfStore.workflowsStore.getState().view).toBe('list');
  });

  it('a path with a bidi override shows the escape, not the reordered text', async () => {
    await toTrust({
      files: [{ path: 'evil\u202Egnp.ts', bytes: 10 }],
      folder: '/x/\u2066hidden\u2069',
    });
    await dialog().findByText('evil\\u{202E}gnp.ts');
    expect(el('workflows').textContent).not.toContain('\u202E');
    expect(el('workflows').textContent).toContain(
      '/x/\\u{2066}hidden\\u{2069}',
    );
  });

  it('confirm changed re-describes', async () => {
    await toTrust();
    await dialog().findByRole('button', { name: 'Trust and continue' });
    engine.hash = 'ffffffffffffffffffffffff';
    const before = engine.requests.filter(
      (r) => r.topic === 'workflow.trust.describe',
    ).length;
    // The view still holds the old hash, so the confirm is refused.
    engine.hash = 'bbbbbbbbbbbbbbbbbbbbbbbb';
    fireEvent.click(
      dialog().getByRole('button', { name: 'Trust and continue' }),
    );
    await dialog().findByText(/The files changed while you were reading/);
    await until(
      () =>
        engine.requests.filter((r) => r.topic === 'workflow.trust.describe')
          .length > before,
      're-describe',
    );
    await dialog().findByText('bbbbbbbbbbbb');
  });

  it('a symlink error offers no confirm', async () => {
    await toTrust({ describeError: 'symlink' });
    await dialog().findByText(/symbolic link/);
    expect(
      dialog().queryByRole('button', { name: 'Trust and continue' }),
    ).toBeNull();
    // y has nothing to confirm.
    key({ key: 'y', code: 'KeyY' });
    expect(engine.trust).toBe('none');
  });
});

describe('review and start', () => {
  const toReview = async (
    seed: Parameters<WorkflowsEngine['seed']>[0] = {},
  ) => {
    engine.seed({
      trust: 'trusted',
      workflows: [{ file: 'review-loop.ts', name: 'Review loop' }],
      builds: { 'review-loop.ts': BUILD },
      startedRun: RUN,
      ...seed,
    });
    await open();
    fireEvent.click(
      (await dialog().findByText('review-loop.ts')).closest(
        'button',
      ) as HTMLElement,
    );
  };

  it('type_errors render file:line:col', async () => {
    await toReview({
      builds: {
        'review-loop.ts': {
          error: {
            code: 'type_errors',
            message: 'type errors',
            type_errors: [
              {
                file: 'review-loop.ts',
                line: 12,
                col: 7,
                message: "Type 'number' is not assignable to type 'string'.",
              },
            ],
          },
        },
      },
    });
    await dialog().findByText('review-loop.ts:12:7');
    expect(dialog().getByText(/is not assignable/)).toBeTruthy();
  });

  it('start sends coerced input and source_hash', async () => {
    await toReview();
    const task = (await dialog().findByLabelText(/task/)) as HTMLInputElement;
    fireEvent.change(task, { target: { value: 'fix the bug' } });
    fireEvent.click(dialog().getByRole('button', { name: 'Start run' }));
    await until(
      () => engine.requests.find((r) => r.topic === 'run.start'),
      'run.start',
    );
    const req = engine.requests.find((r) => r.topic === 'run.start');
    expect(req?.data).toMatchObject({
      project_id: 'p1',
      file: 'review-loop.ts',
      input: { task: 'fix the bug' },
      source_hash: 'h1',
    });
    await waitFor(() =>
      expect(m.wfStore.workflowsStore.getState().view).toBe('run'),
    );
  });

  it('run.start changed rebuilds and says so', async () => {
    await toReview();
    engine.currentHash = 'h2';
    const task = (await dialog().findByLabelText(/task/)) as HTMLInputElement;
    fireEvent.change(task, { target: { value: 'x' } });
    const builds = () =>
      engine.requests.filter((r) => r.topic === 'workflow.build').length;
    const before = builds();
    fireEvent.click(dialog().getByRole('button', { name: 'Start run' }));
    await dialog().findByText('The workflow changed since you reviewed it.');
    expect(builds()).toBeGreaterThan(before);
  });

  it('a run-start draft survives close and reopen', async () => {
    await toReview();
    const task = (await dialog().findByLabelText(/task/)) as HTMLInputElement;
    fireEvent.change(task, { target: { value: 'half typed' } });
    act(() => m.app.closeWorkflows());
    await open();
    fireEvent.click(
      (await dialog().findByText('review-loop.ts')).closest(
        'button',
      ) as HTMLElement,
    );
    const again = (await dialog().findByLabelText(/task/)) as HTMLInputElement;
    expect(again.value).toBe('half typed');
  });

  it('Escape in review goes back to the list', async () => {
    await toReview();
    await dialog().findByLabelText(/task/);
    key({ key: 'Escape', code: 'Escape' });
    expect(m.wfStore.workflowsStore.getState().view).toBe('list');
    expect(isOpen()).toBe(true);
    key({ key: 'Escape', code: 'Escape' });
    expect(isOpen()).toBe(false);
  });
});

describe('the toggle', () => {
  it('menu:workflows while the modal is open closes it, from the trust view too', async () => {
    await toTrust();
    expect(isOpen()).toBe(true);
    act(() => m.app.toggleWorkflows());
    expect(isOpen()).toBe(false);
  });

  it('off macOS the opener chord runs the toggle and closes, from the trust view too', async () => {
    await toTrust();
    await dialog().findByRole('button', { name: 'Trust and continue' });
    key({ key: 'R', code: 'KeyR', ctrlKey: true, shiftKey: true });
    expect(isOpen()).toBe(false);
    expect(engine.trust).toBe('none');
  });
});

const toRun = async (seed: Parameters<WorkflowsEngine['seed']>[0] = {}) => {
  engine.seed({ trust: 'trusted', workflows: [], ...seed });
  await open();
  await act(async () => {
    void m.app.loadRunState('r1');
    m.wfStore.setView('run', { runId: 'r1', selectedKey: null });
  });
};

describe('run view', () => {
  it('takes node state from run.nodes; run.get carries no nodes', async () => {
    await toRun({ runs: [RUN] });
    await dialog().findByRole('button', { name: /^work, agent claude, done/ });
    expect(
      dialog().getByRole('button', { name: /^review, agent codex, running/ }),
    ).toBeTruthy();
    expect(engine.requests.some((r) => r.topic === 'run.nodes')).toBe(true);
    expect(engine.requests.some((r) => r.topic === 'run.events')).toBe(false);
  });

  it('a finished run renders its stored final rows', async () => {
    await toRun({
      runs: [
        {
          ...RUN,
          state: 'done',
          nodes: RUN.nodes.map((n) => ({ ...n, state: 'done' })),
        },
      ],
    });
    await dialog().findByRole('button', { name: /^review, agent codex, done/ });
    expect(dialog().queryByRole('button', { name: 'Cancel run' })).toBeNull();
    expect(dialog().queryByRole('button', { name: 'Retry' })).toBeNull();
  });

  it('shows the source changed chip in the run header', async () => {
    await toRun({ runs: [{ ...RUN, source_changed: true }] });
    await dialog().findByText('source changed since this run started');
  });

  it('retry shows only for failed and cancelled', async () => {
    for (const [state, retry] of [
      ['running', false],
      ['done', false],
      ['failed', true],
      ['cancelled', true],
    ] as const) {
      cleanup();
      unmounts.length = 0;
      m.wfStore.resetWorkflowsStore();
      render(<m.Workflows root={el('workflows')} />, {
        container: el('workflows'),
      });
      await toRun({ runs: [{ ...RUN, state }] });
      await until(
        () =>
          el('workflows').querySelector(
            `.hv-wf-run__head [data-state="${state}"]`,
          ),
        `${state} header`,
      );
      expect(!!dialog().queryByRole('button', { name: 'Retry' }), state).toBe(
        retry,
      );
      act(() => m.app.closeWorkflows());
    }
  });

  it('cancel run asks through the choice dialog and keeps running on Escape', async () => {
    await toRun({ runs: [RUN] });
    fireEvent.click(
      await dialog().findByRole('button', { name: 'Cancel run' }),
    );
    const choices = within(el('choice-dialog'));
    await choices.findByText('Cancel this run?');
    expect(document.activeElement).toBe(
      choices.getByRole('button', { name: 'Keep running' }),
    );
    key({ key: 'Escape', code: 'Escape' });
    await waitFor(() =>
      expect(el('choice-dialog').classList.contains('hidden')).toBe(true),
    );
    expect(engine.requests.some((r) => r.topic === 'run.cancel')).toBe(false);

    fireEvent.click(dialog().getByRole('button', { name: 'Cancel run' }));
    fireEvent.click(await choices.findByRole('button', { name: 'Cancel run' }));
    await until(
      () => engine.requests.some((r) => r.topic === 'run.cancel'),
      'run.cancel',
    );
  });

  it('engine text renders as plain text', async () => {
    const prompt = '<img src=x onerror=alert(1)> and **md**';
    await toRun({
      runs: [
        {
          ...RUN,
          state: 'running',
          nodes: [
            {
              key: 'approve',
              node_id: 'approve',
              state: 'waiting',
              attempt: 1,
            },
          ],
          events: [
            {
              type: 'node.waiting',
              key: 'approve',
              node_id: 'approve',
              attempt: 1,
              data: {
                reason: 'human',
                prompt,
                output_schema: {
                  type: 'object',
                  required: ['verdict'],
                  additionalProperties: false,
                  properties: { verdict: { type: 'string' } },
                },
              },
            },
          ],
        },
      ],
    });
    await dialog().findByText(prompt);
    expect(el('workflows').querySelector('img')).toBeNull();
    expect(el('workflows').innerHTML).not.toContain('<img');
  });
});
