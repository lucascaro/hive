// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { expect, type Page, test } from '@playwright/test';
import { examples } from '../../../../../plugins/workflows/tools/examples.ts';
import { boot, prepare } from './fixtures/key-sweep.js';
import {
  overlayStates,
  startWorkflows,
  truthStates,
} from './fixtures/workflows.js';
import { rowsFor, shipIr, VERDICT } from './fixtures/workflows-ir.js';
import type { MockEvent } from './workflows-engine.js';

// Workflows (spec 530) against the mock bridge's scripted engine. Layout is
// asserted here, in real Chromium: vitest is CSS-blind.

const wf = (page: Page) => page.locator('#workflows');
const BUILD = () => {
  const ir = shipIr();
  return {
    name: 'ship',
    ir,
    input_schema: ir.input,
    source_hash: 'h1',
  };
};

const push = (page: Page, runId: string, events: Partial<MockEvent>[]) =>
  page.evaluate(
    ([id, evs]) => window.__hive.pushRunEvents?.(id as string, evs as never),
    [runId, events] as const,
  );

test.beforeEach(async ({ page }) => {
  await prepare(page, 'other');
  await boot(page);
});

test('turn on, list, trust, review, start, live overlay, human answer, reopen finished run', async ({
  page,
}) => {
  await page.evaluate((s) => window.__hive.seedWorkflows?.(s), {
    trust: 'none',
    folder: '/work/p1/.hive/workflows',
    files: [{ path: 'ship.ts', bytes: 2150 }],
    workflows: [{ file: 'ship.ts' }],
    builds: { 'ship.ts': BUILD() },
    startedRun: { run_id: 'run', workflow: 'ship', ir: shipIr() },
  } as never);
  // Off by default: the panel offers Turn on.
  await page.keyboard.press('Control+Shift+KeyR');
  await expect(wf(page)).toBeVisible();
  await expect(wf(page)).toContainText('Workflows are off.');
  await wf(page).getByRole('button', { name: 'Turn on' }).click();

  // Not trusted yet: rows inert, Review & trust offered.
  await expect(wf(page)).toContainText('not trusted');
  await expect(
    wf(page).getByRole('button', { name: /ship\.ts/ }),
  ).toBeDisabled();
  await wf(page).getByRole('button', { name: 'Review & trust…' }).click();
  await expect(wf(page)).toContainText('Contents');
  await expect(wf(page).getByRole('button', { name: 'Cancel' })).toBeFocused();
  await expect(
    wf(page).getByRole('button', { name: 'Trust and continue' }),
  ).toHaveAttribute('data-kind', 'danger');
  await page.keyboard.press('y');

  // Trusted: open the file, fill the form, start.
  await expect(wf(page)).toContainText('trusted');
  await wf(page)
    .getByRole('button', { name: /ship\.ts/ })
    .click();
  await wf(page).getByLabel(/task/).fill('fix the bug');
  await wf(page).getByLabel('strict').check();
  await wf(page).getByRole('button', { name: 'Start run' }).click();

  // The run opens; the engine reports events live.
  await expect(wf(page)).toContainText('› Run');
  await push(page, 'run-1', [
    { type: 'run.started' },
    { type: 'node.started', key: 'work', node_id: 'work', attempt: 1 },
  ]);
  const work = wf(page).locator('button[data-node-id="work"]');
  await expect(work).toHaveAttribute('data-state', 'running');
  await expect(work).toHaveAttribute(
    'aria-label',
    /agent claude, running, attempt 1/,
  );

  await push(page, 'run-1', [
    {
      type: 'node.finished',
      key: 'work',
      node_id: 'work',
      attempt: 1,
      data: { state: 'done' },
    },
    {
      type: 'node.waiting',
      key: 'approve',
      node_id: 'approve',
      attempt: 1,
      data: { reason: 'human', prompt: 'Ship it?', output_schema: VERDICT },
    },
  ]);
  await expect(work).toHaveAttribute('data-state', 'done');
  const approve = wf(page).locator('button[data-node-id="approve"]');
  await expect(approve).toHaveAttribute('data-state', 'waiting');
  // A live human wait: the banner and one desktop notification.
  await expect(page.locator('#workflow-banner')).toBeVisible();
  await expect(page.locator('#workflow-banner')).toContainText(
    'is waiting on you',
  );
  const calls = await page.evaluate(() => window.__hive.notifyCalls?.() ?? []);
  expect(calls.filter((c) => c.tag.startsWith('wf:'))).toEqual([
    expect.objectContaining({ tag: 'wf:run-1:approve', title: 'ship' }),
  ]);

  // The detail pane shows the question; answer it.
  await expect(wf(page).getByText('Ship it?')).toBeVisible();
  await wf(page).getByRole('radio', { name: 'approve' }).check();
  await wf(page).getByRole('button', { name: 'Answer' }).click();
  await expect(approve).toHaveAttribute('data-state', 'done');
  await expect(wf(page).locator('.hv-wf-run__head')).toContainText('done');
  await expect(page.locator('#workflow-banner')).toBeHidden();

  // Close and reopen the finished run from the list: stored rows only.
  await page.keyboard.press('Escape'); // run -> list
  await expect(wf(page).getByText('Recent runs')).toBeVisible();
  await wf(page).locator('button[data-run-id="run-1"]').click();
  await expect(
    wf(page).locator('button[data-node-id="approve"]'),
  ).toHaveAttribute('data-state', 'done');
  expect(await overlayStates(page)).toEqual(await truthStates(page, 'run-1'));
});

test('run.nodes paging: three pages, events pushed between them, every event applied exactly once', async ({
  page,
}) => {
  const ir = examples.fanOutThenVerify();
  const nodes = rowsFor(ir);
  await startWorkflows(page, {
    trust: 'trusted',
    pageRows: 2,
    runs: [{ run_id: 'r1', workflow: 'fan-out', ir, nodes }],
  });
  const pages = await page.evaluate(() => {
    const e = window.__hive.workflowsEngine;
    if (!e) throw new Error('no engine');
    e.holdNodesPage(2);
    return e.truth('r1').length;
  });
  expect(pages).toBeGreaterThan(4); // three pages of two or more rows

  // Open the run: page 1 is served, page 2 is held.
  await page.evaluate(async () => {
    const url = '/src/app/workflows.ts';
    const m = await import(/* @vite-ignore */ url);
    await m.openWorkflowRun('r1');
  });
  await expect
    .poll(() => page.evaluate(() => window.__hive.workflowsEngine?.heldCount()))
    .toBe(1);

  // Events between the pages: for a key on page 1 (above its last_seq),
  // for a key that page 2 will already reflect, and for a key on a page
  // not yet served.
  const keys = nodes.map((n) => n.key);
  await push(page, 'r1', [
    {
      type: 'node.finished',
      key: keys[0],
      node_id: nodes[0].node_id,
      attempt: 1,
      data: { state: 'done' },
    },
    {
      type: 'node.finished',
      key: keys[2],
      node_id: nodes[2].node_id,
      attempt: 1,
      data: { state: 'failed', error: 'x' },
    },
    {
      type: 'node.started',
      key: keys[keys.length - 1],
      node_id: nodes[keys.length - 1].node_id,
      attempt: 2,
    },
  ]);
  await page.evaluate(() => window.__hive.workflowsEngine?.releaseHeld());

  await expect(wf(page).locator('.hv-wf-run__head')).toBeVisible();
  await expect
    .poll(async () => (await overlayStates(page))[keys[keys.length - 1]])
    .toBe('running');
  expect(await overlayStates(page)).toEqual(await truthStates(page, 'r1'));
  const reqs = await page.evaluate(
    () => window.__hive.workflowsEngine?.requests.map((r) => r.topic) ?? [],
  );
  expect(reqs.filter((t) => t === 'run.nodes').length).toBeGreaterThanOrEqual(
    3,
  );
  expect(reqs).not.toContain('run.events');
});

test('waiting banner opens the run', async ({ page }) => {
  const ir = shipIr();
  await startWorkflows(page, {
    trust: 'trusted',
    runs: [
      {
        run_id: 'w1',
        workflow: 'ship',
        ir,
        nodes: [
          { key: 'approve', node_id: 'approve', state: 'waiting', attempt: 1 },
        ],
        events: [
          {
            type: 'node.waiting',
            key: 'approve',
            node_id: 'approve',
            attempt: 1,
            data: {
              reason: 'human',
              prompt: 'Ship it?',
              output_schema: VERDICT,
            },
          },
        ],
      },
    ],
  });
  // The boot scan raised it without announcing it.
  const banner = page.locator('#workflow-banner');
  await expect(banner).toBeVisible();
  await expect(banner).toContainText('Workflow "ship"');
  expect(
    await page.evaluate(() => window.__hive.notifyCalls?.() ?? []),
  ).toEqual([]);
  await banner.getByRole('button', { name: /^Open/ }).click();
  await expect(wf(page)).toBeVisible();
  await expect(wf(page).getByText('Ship it?')).toBeVisible();
});

test('permission wait: the node session raises its own attention and the workflow layer adds no second Notify', async ({
  page,
}) => {
  const ir = shipIr();
  await startWorkflows(page, {
    trust: 'trusted',
    runs: [
      {
        run_id: 'p1r',
        workflow: 'ship',
        ir,
        nodes: [{ key: 'work', node_id: 'work', state: 'running', attempt: 1 }],
      },
    ],
  });
  await expect(page.locator('#workflow-banner')).toBeHidden();
  // The node's session is an ordinary session; its permission wait is what
  // the daemon turns into needs_attention (the mock models that edge).
  const sid = await page.evaluate(() => {
    const id = window.__hive_state?.sessions.find((x) => x.name === 's2')?.id;
    if (!id) throw new Error('no s2');
    window.__hive.ringBell?.(id);
    return id;
  });
  await push(page, 'p1r', [
    {
      type: 'node.waiting',
      key: 'work',
      node_id: 'work',
      attempt: 1,
      data: {
        reason: 'permission',
        session_id: sid,
        request_id: 'q1',
        title: 'Run go test?',
        options: [
          { option_id: 'allow', name: 'Allow once', kind: 'allow_once' },
        ],
      },
    },
  ]);
  await expect(page.locator('#workflow-banner')).toBeVisible();
  await expect
    .poll(
      async () =>
        (await page.evaluate(() => window.__hive.notifyCalls?.() ?? [])).length,
    )
    .toBe(1);
  const calls = await page.evaluate(() => window.__hive.notifyCalls?.() ?? []);
  expect(calls).toEqual([expect.objectContaining({ tag: sid })]);

  // Answerable from the node detail, with the existing ANSWER_PERMISSION.
  await page
    .locator('#workflow-banner')
    .getByRole('button', { name: /^Open/ })
    .click();
  await wf(page).getByRole('button', { name: 'Allow once' }).click();
  const answered = await page.evaluate(
    () => window.__hive.bridgeCalls?.('AnswerPermission') ?? [],
  );
  expect(answered).toEqual([
    { method: 'AnswerPermission', args: [sid, 'q1', 'allow'] },
  ]);
});

test('the engine absent or refused never breaks the panel', async ({
  page,
}) => {
  // No workflows plugin in the list at all.
  await page.evaluate(async () => {
    const url = '/src/store/store.ts';
    const m = await import(/* @vite-ignore */ url);
    m.setPlugins([]);
  });
  await page.keyboard.press('Control+Shift+KeyR');
  await expect(wf(page)).toContainText('This Hive has no workflow engine.');
  await page.keyboard.press('Escape');
  await expect(wf(page)).toBeHidden();
  // Refused, with A's status_detail.
  await page.evaluate(async () => {
    const url = '/src/store/store.ts';
    const m = await import(/* @vite-ignore */ url);
    m.setPlugins([
      {
        id: 'workflows',
        name: 'Workflows',
        version: '1.0.0',
        api_version: '0.2',
        source: 'builtin',
        command: [],
        enabled: true,
        status: 'refused',
        status_detail:
          'requires Node.js 26.0.0 or newer (found v24.11.0 at /usr/local/bin/node)',
        restarts: 0,
        builtin: true,
      },
    ]);
  });
  await page.keyboard.press('Control+Shift+KeyR');
  await expect(wf(page)).toContainText('requires Node.js 26.0.0 or newer');
  await expect(wf(page).getByRole('button', { name: 'Restart' })).toBeVisible();
});

const THEMES = ['hive-dark', 'hive-light'] as const;
const SIZES = [
  { width: 1280, height: 800 },
  { width: 900, height: 700 },
] as const;

for (const [name, make] of Object.entries(examples)) {
  for (const theme of THEMES) {
    for (const size of SIZES) {
      test(`layout: ${name} under ${theme} at ${size.width}x${size.height}`, async ({
        page,
      }) => {
        await page.setViewportSize(size);
        await page.evaluate((t) => {
          document.documentElement.dataset.theme = t;
        }, theme);
        const ir = make();
        await startWorkflows(page, {
          trust: 'trusted',
          runs: [{ run_id: 'L', workflow: name, ir, nodes: rowsFor(ir) }],
        });
        await page.evaluate(async () => {
          const url = '/src/app/workflows.ts';
          const m = await import(/* @vite-ignore */ url);
          await m.openWorkflowRun('L');
        });
        await expect(
          wf(page).locator('button[data-node-id]').first(),
        ).toBeVisible();

        // The page never scrolls horizontally; the graph pane scrolls inside
        // itself.
        const overflow = await page.evaluate(() => ({
          page: document.documentElement.scrollWidth - window.innerWidth,
          panel: (() => {
            const p = document.getElementById('workflows-panel');
            const r = p?.getBoundingClientRect();
            return r ? { right: r.right, w: window.innerWidth } : null;
          })(),
        }));
        expect(overflow.page).toBeLessThanOrEqual(0);
        expect(overflow.panel?.right).toBeLessThanOrEqual(
          overflow.panel?.w ?? 0,
        );

        // Every edge ends inside its source and target boxes (a point on
        // the bottom/top border counts: 1px slack).
        const bad = await page.evaluate(() => {
          const out: string[] = [];
          const near = (x: number, y: number, r: DOMRect) =>
            x >= r.left - 1 &&
            x <= r.right + 1 &&
            y >= r.top - 1 &&
            y <= r.bottom + 1;
          for (const path of document.querySelectorAll<SVGPathElement>(
            '#workflows .hv-wf-edges path[data-from]',
          )) {
            const svg = path.ownerSVGElement?.getBoundingClientRect();
            const nums =
              (path.getAttribute('d') ?? '')
                .match(/-?\d+(\.\d+)?/g)
                ?.map(Number) ?? [];
            if (!svg || nums.length < 8) continue;
            const body = path.closest('.hv-wf-body');
            const find = (id: string | null) =>
              [
                ...(body?.querySelectorAll<HTMLElement>('[data-node-id]') ??
                  []),
              ].find((e) => e.dataset.nodeId === id);
            const a = find(
              path.getAttribute('data-from'),
            )?.getBoundingClientRect();
            const b = find(
              path.getAttribute('data-to'),
            )?.getBoundingClientRect();
            if (!a || !b) {
              out.push(
                `missing endpoint ${path.getAttribute('data-from')}->${path.getAttribute('data-to')}`,
              );
              continue;
            }
            if (!near(svg.left + nums[0], svg.top + nums[1], a))
              out.push(`start of ${path.getAttribute('data-from')}`);
            if (!near(svg.left + nums[6], svg.top + nums[7], b))
              out.push(`end at ${path.getAttribute('data-to')}`);
          }
          return out;
        });
        expect(bad).toEqual([]);

        // The element at each node's centre is that node.
        const ids = await wf(page)
          .locator('button[data-node-id]')
          .evaluateAll((els) =>
            els.map((e) => (e as HTMLElement).dataset.nodeId),
          );
        for (const id of ids) {
          const hit = await page.evaluate((nid) => {
            const el = [
              ...document.querySelectorAll<HTMLElement>(
                '#workflows button[data-node-id]',
              ),
            ].find((e) => e.dataset.nodeId === nid);
            if (!el) return 'missing';
            el.scrollIntoView({ block: 'center', inline: 'center' });
            const r = el.getBoundingClientRect();
            const top = document.elementFromPoint(
              r.left + r.width / 2,
              r.top + r.height / 2,
            );
            return (
              top
                ?.closest('button[data-node-id]')
                ?.getAttribute('data-node-id') ?? String(top)
            );
          }, id);
          expect(hit, `centre of ${id}`).toBe(id);
        }
      });
    }
  }
}
