// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Workflows: the headless engine (spec 529). The entry stays plain JavaScript
// so the plugin starts on `node main.mjs`; the engine is TypeScript run by
// Node's native type stripping, which needs Node 26 here (the manifest says so).

import { connect } from './hive-plugin.mjs';
import { start } from './engine/main.ts';

const dataDir = process.env.HIVE_PLUGIN_DATA_DIR;
if (!dataDir) {
  console.error('workflows: HIVE_PLUGIN_DATA_DIR is not set; run me under Hive');
  process.exit(1);
}

// A connection the daemon closes on us (its fan-out overflowed) is dialled again; the engine decides when to give up.
const dial = () => connect({ exitOnClose: false });
const hive = await dial();
const testSeam = process.env.HIVE_WF_TEST_SEAM === '1';
// The 10 s a cancelled turn gets before its session is killed: a test shortens it, and only with the seam on.
const grace = Number(process.env.HIVE_WF_CANCEL_GRACE_MS);
// A test can hold an agent node still at one point ("after-create:3000"), so it can crash the plugin exactly there.
const [pausePoint, pauseMs] = (testSeam ? (process.env.HIVE_WF_TEST_PAUSE ?? '') : '').split(':');
const pause = pausePoint && Number(pauseMs) > 0 ? (point) => (point === pausePoint ? new Promise((r) => setTimeout(r, Number(pauseMs))) : Promise.resolve()) : undefined;
await start(hive, {
  dataDir,
  testSeam,
  redial: dial,
  onGone: () => process.exit(0),
  ...(testSeam && Number.isFinite(grace) && grace > 0 ? { cancelGraceMs: grace } : {}),
  ...(pause ? { pause } : {}),
});
console.log('workflows: ready');
