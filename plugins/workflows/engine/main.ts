// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The plugin's entry (spec 529): open the store, pick up the runs that were
// active when the engine last stopped, and answer the GUI's topics.

import { Engine, type Hive } from './engine.ts';
import { batchForPush } from './events.ts';
import type { AgentDeps } from './agent.ts';
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
  /** How long a cancelled agent turn gets before its session is killed. A test shortens it; never set in a release. */
  cancelGraceMs?: number;
  /** Holds an agent node still at a named point, so a test can crash the plugin exactly there. Never set in a release. */
  pause?: AgentDeps['pause'];
  /** Dials the daemon again after the connection was lost. Without it a lost connection ends the plugin, as before. */
  redial?: () => Promise<HiveConnection>;
  /** The connection is gone and cannot be had again: the plugin ends, and the daemon starts a fresh copy. */
  onGone?: () => void;
  /** Pause between redial attempts, ms. */
  redialDelayMs?: number;
}

/** How many times a lost connection is redialled before the plugin gives up and lets the daemon restart it. */
const REDIAL_ATTEMPTS = 3;

export async function start(hive: HiveConnection, opts: StartOptions): Promise<Engine> {
  const store = new Store(opts.dataDir);
  const engine = new Engine({
    store,
    hive,
    executors: defaultExecutors({ ...(opts.cancelGraceMs === undefined ? {} : { cancelGraceMs: opts.cancelGraceMs }), ...(opts.pause ? { pause: opts.pause } : {}) }),
    onEvents: (runId, events) => {
      for (const batch of batchForPush(runId, events)) hive.postMessage('run.event', { run_id: runId, events: batch });
    },
  });
  const topics = new Topics({ engine, ...(opts.testSeam ? { testSeam: true } : {}) });

  let ended = false;
  const bind = (h: HiveConnection): void => {
    h.on('PLUGIN_MESSAGE', ((msg: PluginMessage) => {
      void (async () => {
        const reply = (await topics.preflight(msg.topic, msg.data)) ?? topics.handle(msg.topic, msg.data);
        if (reply) engine.hive.postMessage(reply.topic, reply.data);
      })().catch((err: Error) => console.error(`workflows: ${msg.topic}: ${err.message}`));
    }) as (msg: never) => void);
    // The daemon closes a connection whose fan-out overflowed (a chatty fleet of sessions): the plugin dials again and re-reads its nodes rather than ending.
    h.on('close', (() => void lost(h)) as (msg: never) => void);
  };
  let redialing = false;
  const lost = async (h: HiveConnection): Promise<void> => {
    if (h !== engine.hive || redialing) return; // a connection that was already replaced
    if (ended || !opts.redial) return opts.onGone?.();
    redialing = true;
    engine.disconnect();
    try {
      for (let i = 0; i < REDIAL_ATTEMPTS && !ended; i++) {
        try {
          const next = await opts.redial();
          bind(next);
          await engine.reconnect(next);
          return;
        } catch (err) {
          console.error(`workflows: redial ${i + 1}/${REDIAL_ATTEMPTS}: ${(err as Error).message}`);
          await new Promise((r) => setTimeout(r, opts.redialDelayMs ?? 300));
        }
      }
      if (!ended) opts.onGone?.();
    } finally {
      redialing = false;
    }
  };
  bind(hive);

  engine.startFlush();
  await engine.recover();

  // A normal `hived` stop gives the plugin a few seconds before KILL.
  if (process.platform !== 'win32') {
    process.on('SIGTERM', () => {
      ended = true;
      void engine.shutdown().finally(() => process.exit(0));
    });
  }
  return engine;
}
