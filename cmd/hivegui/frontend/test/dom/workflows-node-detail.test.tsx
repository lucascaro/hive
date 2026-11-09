// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// @vitest-environment jsdom
//
// NodeDetail (src/components/workflows/NodeDetail.tsx): the wait a node is
// blocked on, its output paging, and its activity, all against untrusted
// engine text.
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
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
import { examples } from '../../../../../plugins/workflows/tools/examples.ts';
import type { WorkflowsEngine } from '../e2e/workflows-engine.js';
import { until, wireEngine } from './helpers/workflows-harness.js';

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

type M = {
  store: typeof import('../../src/store/store.js');
  wf: typeof import('../../src/store/workflows.js');
  app: typeof import('../../src/app/workflows.js');
  ov: typeof import('../../src/lib/workflow-overlay.js');
  NodeDetail: typeof import('../../src/components/workflows/NodeDetail.js')['NodeDetail'];
};
let m: M;
let engine: WorkflowsEngine;
const switchTo = vi.fn();

beforeAll(async () => {
  m = {
    store: await import('../../src/store/store.js'),
    wf: await import('../../src/store/workflows.js'),
    app: await import('../../src/app/workflows.js'),
    ov: await import('../../src/lib/workflow-overlay.js'),
    ...(await import('../../src/components/workflows/NodeDetail.js')),
  };
});

const ir = examples.planImplementVerify();
const KEY = 'approve';
const SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['verdict'],
  properties: {
    verdict: { type: 'string', enum: ['approve', 'reject'] },
    note: { type: 'string' },
  },
};

beforeEach(() => {
  m.store.resetStore();
  m.wf.resetWorkflowsStore();
  m.app.resetWorkflowsForTest();
  h.send.mockReset();
  h.answerPermission.mockReset();
  h.answerPermission.mockResolvedValue(undefined);
  switchTo.mockClear();
  engine = wireEngine(h);
  m.app.initWorkflows({
    setFocusedTile: () => {},
    refocusActiveTerm: () => {},
    switchTo: (id) => switchTo(id),
  });
});
afterEach(() => cleanup());

const waitingEvent = (data: Record<string, unknown>, key = KEY) => ({
  type: 'node.waiting',
  key,
  node_id: key,
  attempt: 1,
  data,
});

const seedRun = (
  key: string,
  state: string,
  events: unknown[] = [],
  outputs: Record<string, string> = {},
  extra: Record<string, unknown> = {},
) => {
  engine.seed({
    ...extra,
    runs: [
      {
        run_id: 'r1',
        workflow: 'w',
        ir,
        state: 'running',
        nodes: [{ key, node_id: key, state, attempt: 1 }],
        events: events as never,
        outputs,
      },
    ],
  });
};

/** Mounts NodeDetail on an overlay built the way the loader builds it:
 * from run.nodes rows only (no wait payload). */
const mount = async (key = KEY, withLiveWait?: Record<string, unknown>) => {
  const g = await new Promise<{ nodes: never[]; last_seq: number }>((res) => {
    void m.app
      .request<{ nodes: never[]; last_seq: number }>('run.nodes', {
        run_id: 'r1',
      })
      .then(res);
  });
  let ov = m.ov.applyPage(m.ov.emptyOverlay('r1', 'running'), g as never, ir);
  if (withLiveWait) {
    ov = m.ov.fold(
      ov,
      [
        {
          run_id: 'r1',
          seq: 99,
          ts: '',
          key,
          node_id: key,
          attempt: 1,
          type: 'node.waiting',
          data: withLiveWait,
        },
      ],
      ir,
    );
  }
  return render(<m.NodeDetail runId="r1" nodeKey={key} ir={ir} overlay={ov} />);
};

describe('human wait', () => {
  it('a waiting node opened fresh takes its prompt and output_schema from node.get waiting', async () => {
    seedRun(KEY, 'waiting', [
      waitingEvent({
        reason: 'human',
        prompt: 'Ship it?',
        output_schema: SCHEMA,
      }),
    ]);
    await mount();
    await screen.findByText('Ship it?');
    expect(screen.getByRole('radio', { name: 'approve' })).toBeTruthy();
  });

  it('human answer submits human.answer', async () => {
    seedRun(KEY, 'waiting', [
      waitingEvent({
        reason: 'human',
        prompt: 'Ship it?',
        output_schema: SCHEMA,
      }),
    ]);
    await mount();
    fireEvent.click(await screen.findByRole('radio', { name: 'approve' }));
    fireEvent.change(screen.getByLabelText('note'), {
      target: { value: 'lgtm' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
    await until(
      () => engine.requests.find((r) => r.topic === 'human.answer'),
      'answer',
    );
    expect(
      engine.requests.find((r) => r.topic === 'human.answer')?.data,
    ).toMatchObject({
      run_id: 'r1',
      key: KEY,
      answer: { verdict: 'approve', note: 'lgtm' },
    });
  });

  it('invalid_answer shows the path message', async () => {
    // The engine requires answer.verdict; this schema has no such field.
    seedRun(KEY, 'waiting', [
      waitingEvent({
        reason: 'human',
        prompt: 'Note?',
        output_schema: {
          type: 'object',
          required: [],
          additionalProperties: false,
          properties: { note: { type: 'string' } },
        },
      }),
    ]);
    await mount();
    fireEvent.click(await screen.findByRole('button', { name: 'Answer' }));
    await screen.findByText('answer.verdict: required');
  });

  it('a human answer draft survives close and reopen', async () => {
    seedRun(KEY, 'waiting', [
      waitingEvent({
        reason: 'human',
        prompt: 'Ship it?',
        output_schema: SCHEMA,
      }),
    ]);
    const first = await mount();
    fireEvent.change(await screen.findByLabelText('note'), {
      target: { value: 'half' },
    });
    first.unmount();
    await mount();
    expect(
      ((await screen.findByLabelText('note')) as HTMLInputElement).value,
    ).toBe('half');
  });

  it('a successful answer drops the draft', async () => {
    seedRun(KEY, 'waiting', [
      waitingEvent({
        reason: 'human',
        prompt: 'Ship it?',
        output_schema: SCHEMA,
      }),
    ]);
    await mount();
    fireEvent.click(await screen.findByRole('radio', { name: 'approve' }));
    expect(Object.keys(m.wf.workflowsStore.getState().drafts)).toHaveLength(1);
    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
    await waitFor(() =>
      expect(m.wf.workflowsStore.getState().drafts).toEqual({}),
    );
  });

  it('prompt text renders as text, never markup', async () => {
    const prompt = '<img src=x onerror=alert(1)> **not bold** \u202Egnp';
    seedRun(KEY, 'waiting', [
      waitingEvent({ reason: 'human', prompt, output_schema: SCHEMA }),
    ]);
    const { container } = await mount();
    await screen.findByText(/not bold/);
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('strong')).toBeNull();
    expect(container.textContent).not.toContain('\u202E');
    expect(container.textContent).toContain('\\u{202E}');
  });
});

describe('permission wait', () => {
  const PERM = {
    reason: 'permission',
    session_id: 's7',
    request_id: 'req1',
    title: 'Run `go test ./...`?',
    options: [
      { option_id: 'allow', name: 'Allow once', kind: 'allow_once' },
      { option_id: 'reject', name: 'Reject', kind: 'reject_once' },
    ],
  };

  it('permission options from node.get waiting call AnswerPermission with session_id request_id option_id', async () => {
    seedRun('review', 'waiting', [waitingEvent(PERM, 'review')]);
    await mount('review');
    await screen.findByText('Run `go test ./...`?');
    fireEvent.click(screen.getByRole('button', { name: 'Allow once' }));
    expect(h.answerPermission).toHaveBeenCalledWith('s7', 'req1', 'allow');
  });

  it('a live wait on the overlay is used without asking node.get', async () => {
    seedRun('review', 'waiting', []);
    await mount('review', PERM);
    await screen.findByText('Run `go test ./...`?');
  });

  it('Open session switches to the node session and closes the modal', async () => {
    seedRun('review', 'waiting', [waitingEvent(PERM, 'review')]);
    m.store.openModal({
      id: 'workflows',
      projectId: 'p1',
      projectName: 'hive',
    });
    await mount('review');
    fireEvent.click(
      await screen.findByRole('button', { name: 'Open session' }),
    );
    expect(switchTo).toHaveBeenCalledWith('s7');
    expect(m.store.isModalOpen('workflows')).toBe(false);
  });
});

describe('output paging', () => {
  const FULL = '{"summary":"hello world, this is a long output","n":1}';

  it('Load more appends the next offset slice into the same pre and never parses it', async () => {
    seedRun('review', 'done', [], { review: FULL }, { sliceChars: 20 });
    const { container } = await mount('review');
    const pre = await waitFor(() => {
      const el = container.querySelector('pre.hv-wf-output');
      if (!el) throw new Error('no pre');
      return el;
    });
    expect(pre.textContent).toBe(FULL.slice(0, 20));
    // A truncated page is not valid JSON, and nothing tried to read it.
    expect(() => JSON.parse(pre.textContent ?? '')).toThrow();
    while (screen.queryByRole('button', { name: 'Load more' })) {
      const before = pre.textContent?.length ?? 0;
      fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
      await waitFor(() =>
        expect(pre.textContent?.length).toBeGreaterThan(before),
      );
    }
    expect(container.querySelectorAll('pre.hv-wf-output')).toHaveLength(1);
    expect(pre.textContent).toBe(FULL);
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
  });

  it('Load more is hidden when more is false', async () => {
    seedRun('review', 'done', [], { review: 'short' });
    const { container } = await mount('review');
    await waitFor(() =>
      expect(container.querySelector('pre.hv-wf-output')?.textContent).toBe(
        'short',
      ),
    );
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
  });
});

describe('activity', () => {
  const msg = (text: string, seq: number) => ({
    type: 'agent.message',
    key: 'review',
    node_id: 'review',
    attempt: 1,
    data: { session_id: 's', item_id: `i${seq}`, text },
  });

  it('agent.message renders through Markdown without raw HTML', async () => {
    seedRun('review', 'running', [
      msg(
        'Hello **bold** <img src=x onerror=alert(1)> <script>boom()</script>',
        1,
      ),
    ]);
    const { container } = await mount('review');
    await waitFor(() =>
      expect(container.querySelector('strong')?.textContent).toBe('bold'),
    );
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('script')).toBeNull();
    expect(container.textContent).toContain('<img src=x onerror=alert(1)>');
  });

  it('check.output strips CSI sequences', async () => {
    seedRun('review', 'running', [
      {
        type: 'check.output',
        key: 'review',
        node_id: 'review',
        attempt: 1,
        data: {
          stream: 'stdout',
          text: '\u001B[31mFAIL\u001B[0m ok\u001B]0;t\u0007',
        },
      },
    ]);
    const { container } = await mount('review');
    await waitFor(() => {
      const pre = container.querySelectorAll('pre.hv-wf-output');
      expect(pre[pre.length - 1]?.textContent).toBe('FAIL ok');
    });
  });

  it('facts show the session, error and conflict from node.get as text', async () => {
    seedRun('review', 'failed', [
      {
        type: 'node.started',
        key: 'review',
        node_id: 'review',
        attempt: 1,
        data: {
          session_id: 's1',
          branch: 'hive/wf/r1/review',
          worktree: '/w/review',
        },
      },
      {
        type: 'node.finished',
        key: 'review',
        node_id: 'review',
        attempt: 1,
        data: {
          state: 'failed',
          error: '<b>boom</b>',
          conflict: {
            branch: 'main',
            files: ['a.go', 'b\u202Ec.go'],
            worktree: '/w/review',
          },
        },
      },
    ]);
    const { container } = await mount('review');
    await screen.findByText('hive/wf/r1/review');
    await screen.findByText('<b>boom</b>');
    expect(container.querySelector('b')).toBeNull();
    expect(container.textContent).toContain('b\\u{202E}c.go');
    expect(container.textContent).toContain('then retry the run');
    act(() => {});
  });
});
