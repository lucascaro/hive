// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Playwright helpers for the Workflows specs: turn the (mock) engine on,
// seed it, and read the app's own view of a run.

import { expect, type Page } from '@playwright/test';
import type { MockWorkflowsSpec } from '../workflows-engine.js';

/** Seeds the scripted engine and turns the builtin plugin on, the way the
 * Turn on button does. */
export async function startWorkflows(page: Page, spec: MockWorkflowsSpec) {
  // The spec carries IR types deep enough to trip evaluate's Serializable
  // check; it is plain JSON on the wire.
  await page.evaluate(
    (s) => window.__hive.seedWorkflows?.(s as never),
    spec as unknown as Record<string, unknown>,
  );
  await page.evaluate(async () => {
    const url = '/src/bridge.ts';
    const b = await import(/* @vite-ignore */ url);
    await b.SetPluginEnabled('workflows', true);
  });
  await expect
    .poll(() =>
      page.evaluate(async () => {
        const url = '/src/store/store.ts';
        const m = await import(/* @vite-ignore */ url);
        return m.appStore
          .getState()
          .plugins.find((p: { id: string }) => p.id === 'workflows')?.status;
      }),
    )
    .toBe('running');
}

/** Opens the Workflows modal through its command. */
export async function openWorkflowsModal(page: Page) {
  await page.evaluate(async () => {
    const url = '/src/app/command-registry.ts';
    const m = await import(/* @vite-ignore */ url);
    if (!m.runCommand('workflows')) throw new Error('workflows declined');
  });
  await expect(page.locator('#workflows')).toBeVisible();
}

/** The overlay state of every instance of the open run, as the app holds
 * it: key -> state. */
export function overlayStates(page: Page): Promise<Record<string, string>> {
  return page.evaluate(async () => {
    const url = '/src/store/workflows.ts';
    const m = await import(/* @vite-ignore */ url);
    const run = m.workflowsStore.getState().run;
    return Object.fromEntries(
      Object.entries(
        (run?.overlay.instances ?? {}) as Record<string, { state: string }>,
      ).map(([k, v]) => [k, v.state]),
    );
  });
}

/** The mock engine's ground-truth rows for a run: key -> state. */
export function truthStates(
  page: Page,
  runId: string,
): Promise<Record<string, string>> {
  return page.evaluate(
    (id) =>
      Object.fromEntries(
        (window.__hive.workflowsEngine?.truth(id) ?? []).map((n) => [
          n.key,
          n.state,
        ]),
      ),
    runId,
  );
}
