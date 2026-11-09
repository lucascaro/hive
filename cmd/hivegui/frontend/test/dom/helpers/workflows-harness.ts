// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Shared setup for the Workflows dom suites (spec 530). The bridge is
// stubbed with every export as a resolved no-op, except the few the suite
// observes (bridge-stub.ts); SendPluginMessage feeds the same scripted engine the Playwright
// mock uses (test/e2e/workflows-engine.ts), whose replies and pushes come
// back through the app's own plugin-message path.

import { deliverPluginMessage } from '../../../src/app/plugins.js';
import { WorkflowsEngine } from '../../e2e/workflows-engine.js';
import type { Hoisted } from './bridge-stub.js';

/** A fresh scripted engine wired to the app's plugin-message delivery, with
 * SendPluginMessage routed into it. Replies arrive asynchronously, as they
 * do over the daemon. */
export function wireEngine(h: Hoisted): WorkflowsEngine {
  const engine = new WorkflowsEngine((topic, data) =>
    deliverPluginMessage({ plugin_id: 'workflows', topic, data }),
  );
  h.send.mockImplementation(
    (pluginId: string, topic: string, dataJSON: string) => {
      if (pluginId === 'workflows')
        engine.handle(topic, JSON.parse(dataJSON || '{}'));
      return Promise.resolve();
    },
  );
  return engine;
}

export const flush = (ms = 0): Promise<void> =>
  new Promise((r) => setTimeout(r, ms));

/** Waits until fn() is truthy, in small slices (the engine answers on
 * timers). */
export async function until<T>(
  fn: () => T | null | undefined | false,
  what = 'condition',
): Promise<T> {
  for (let i = 0; i < 200; i++) {
    const v = fn();
    if (v) return v;
    await flush(5);
  }
  throw new Error(`timed out waiting for ${what}`);
}
