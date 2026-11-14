// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// A child process for check.test.ts: a whole engine with the real check
// executor, running one check that sleeps the first time and exits at once the
// second. The parent kills this process the way a plugin crash would, and the
// check, in its own process group, outlives it.

import { existsSync } from 'node:fs';
import { checkExecutor } from '../engine/nodes.ts';
import { workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { FakeHive, harness } from './harness.ts';

const [dir, marker, worktree] = process.argv.slice(2);
const script = `const fs=require('fs');const m=${JSON.stringify(marker)};const first=!fs.existsSync(m);if(first)fs.writeFileSync(m,'1');if(!first)process.exit(0);setInterval(()=>{},1000)`;

const hive = new FakeHive();
hive.replies.PREPARE_WORKTREE = () => ({ path: worktree, branch: 'b', base_sha: 'sha-1', reattached: false });
const never = (): Promise<void> => new Promise(() => {});
const h = harness({ agent: never, check: checkExecutor() }, { dir, hive });
const ir = workflow('sleeper', s.object({}), (g) => void g.check('c', { argv: [process.execPath, '-e', script], timeoutSec: 120 }));
const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'c.ts', ir, input: {} });

const timer = setInterval(() => {
  const row = h.store.loadRows(id).find((r) => r.key === 'c');
  // The marker exists once the check's script has run its first lines: only then is it committed to sleeping.
  if (row?.state === 'running' && row.pid !== undefined && existsSync(marker)) {
    clearInterval(timer);
    console.log(JSON.stringify({ id, pid: row.pid, pgid: row.pgid }));
  }
}, 20);
setInterval(() => {}, 1000);
