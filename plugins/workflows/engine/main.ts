// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The plugin's entry (spec 529): open the store, pick up the runs that were
// active when the engine last stopped, and answer the GUI's topics.

import { Engine, type Hive } from './engine.ts';
import { batchForPush } from './events.ts';
import { defaultExecutors } from './nodes.ts';
import { Store } from './store.ts';
import { Topics } from './topics.ts';

/** The SDK connection: what the engine uses, plus the event source. */
export interface HiveConnection extends Hive {
  on(event: string, fn: (msg: never) => void): unknown;
}

interface PluginMessage {
  plugin_id: string;
  topic: string;
  data?: unknown;
}

export interface StartOptions {
  dataDir: string;
  /** Turns on the `test.start-run` topic. Set from HIVE_WF_TEST_SEAM by main.mjs; never in a release. */
  testSeam?: boolean;
}

export async function start(hive: HiveConnection, opts: StartOptions): Promise<Engine> {
  const store = new Store(opts.dataDir);
  const engine = new Engine({
    store,
    hive,
    executors: defaultExecutors(),
    onEvents: (runId, events) => {
      for (const batch of batchForPush(runId, events)) hive.postMessage('run.event', { run_id: runId, events: batch });
    },
  });
  const topics = new Topics({ engine, ...(opts.testSeam ? { testSeam: true } : {}) });

  hive.on('PLUGIN_MESSAGE', ((msg: PluginMessage) => {
    const reply = topics.handle(msg.topic, msg.data);
    if (reply) hive.postMessage(reply.topic, reply.data);
  }) as (msg: never) => void);

  engine.startFlush();
  await engine.recover();

  // A normal `hived` stop gives the plugin a few seconds before KILL.
  if (process.platform !== 'win32') {
    process.on('SIGTERM', () => {
      void engine.shutdown().finally(() => process.exit(0));
    });
  }
  return engine;
}
