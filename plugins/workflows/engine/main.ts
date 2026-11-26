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
  /** How long recovery waits for the daemon's answer to a read before it assumes nothing survived. A test shortens it. */
  daemonWaitMs?: number;
  /** Pause before the second redial in a row, ms; it doubles with each further one, up to 5 s. */
  redialDelayMs?: number;
  /** Called each time a redialled connection has been read and the engine is running on it. A test waits on it. */
  onReconnected?: () => void;
  /** How long a redialled connection must stay open before redials stop counting against the plugin, ms. */
  stableMs?: number;
}

/** How many times a lost connection is redialled before the plugin gives up and lets the daemon restart it. */
const REDIAL_ATTEMPTS = 3;
/** How many redials in a row, across losses, before the plugin gives up: a connection that dies right after every read would otherwise be redialled forever. */
const REDIAL_STREAK_MAX = 10;
const REDIAL_DELAY_CAP_MS = 5_000;

export async function start(hive: HiveConnection, opts: StartOptions): Promise<Engine> {
  const store = new Store(opts.dataDir);
  const engine = new Engine({
    store,
    hive,
    ...(opts.daemonWaitMs ? { daemonWaitMs: opts.daemonWaitMs } : {}),
    executors: defaultExecutors({ ...(opts.cancelGraceMs === undefined ? {} : { cancelGraceMs: opts.cancelGraceMs }), ...(opts.pause ? { pause: opts.pause } : {}) }),
    onEvents: (runId, events) => {
      for (const batch of batchForPush(runId, events)) {
        try {
          // The connection the engine is on now, not the one it started with: that one is closed after a reconnect.
          engine.hive.postMessage('run.event', { run_id: runId, events: batch });
        } catch {
          // No connection right now: the event is stored, and the GUI closes the gap with run.events when it asks again.
        }
      }
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
    h.on('close', (() => lost(h)) as (msg: never) => void);
  };
  let redialing = false;
  /** Redials since a connection last stayed open for `stableMs`. */
  let streak = 0;
  let stable: ReturnType<typeof setTimeout> | undefined;
  const lost = (h: HiveConnection): void => {
    // Every close of the current connection is recorded at once, even one that lands while a redial is reading its nodes: that read then
    // knows its connection is dead and the loop below dials again, instead of the engine going on as if it were open.
    if (!engine.disconnect(h)) return; // a connection that was already replaced or lost
    clearTimeout(stable);
    if (ended || !opts.redial) return opts.onGone?.();
    if (!redialing) void redialLoop();
  };
  const redialLoop = async (): Promise<void> => {
    redialing = true;
    try {
      for (let i = 0; i < REDIAL_ATTEMPTS && !ended; i++) {
        if (streak >= REDIAL_STREAK_MAX) break;
        // The first redial of a streak is at once; each one after waits twice as long as the one before.
        if (streak > 0) await new Promise((r) => setTimeout(r, Math.min((opts.redialDelayMs ?? 300) * 2 ** (streak - 1), REDIAL_DELAY_CAP_MS)));
        streak++;
        try {
          const next = await (opts.redial as () => Promise<HiveConnection>)();
          bind(next);
          await engine.reconnect(next);
          if (!engine.disconnected) {
            // Only a connection that stays up clears the streak, so one that dies right after each read cannot be redialled without bound.
            clearTimeout(stable);
            stable = setTimeout(() => (streak = 0), opts.stableMs ?? 30_000);
            stable.unref?.();
            opts.onReconnected?.();
            return;
          }
          console.error(`workflows: redial ${streak}: the new connection closed while its nodes were read`);
        } catch (err) {
          console.error(`workflows: redial ${streak}: ${(err as Error).message}`);
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
