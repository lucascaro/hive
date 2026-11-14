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

const hive = await connect();
await start(hive, { dataDir, testSeam: process.env.HIVE_WF_TEST_SEAM === '1' });
console.log('workflows: ready');
