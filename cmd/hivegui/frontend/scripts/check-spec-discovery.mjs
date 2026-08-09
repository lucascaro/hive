#!/usr/bin/env node
// Fail if a *.spec.* file on disk is not discovered by its Playwright config.
//
// The failure this exists for is SILENT. Playwright treats a spec file that
// matches no `testMatch` glob as absent, not as an error — so a rename that
// falls out of the glob makes those tests stop running while CI stays green
// and the coverage claim reads as true. That is failure mode (2) in
// docs/exec-plans/active/typescript-migration.md's "Silent breakage is the
// real risk", and wave 7 renames all 22 spec files at once.
//
// Compares SETS, not counts, so there is no expected number to maintain: a
// new spec passes on the day it is added, and only a spec that exists but
// isn't collected fails. Runs with --list, which does not execute the
// suites (and so does not fire playwright.real.config.js's globalSetup, which
// would spawn a real hived — verified, not assumed).
//
// Blind spot, stated so nobody trusts it further than it goes: a rename that
// stops the file LOOKING like a spec at all (`x.spec.js` → `x.speec.js`)
// drops off both sides of the comparison and passes. Guarding that needs a
// baseline of what used to be a spec, which is what `git status` and the
// per-wave count comparison already are. What this catches is the case those
// two miss because it is invisible in both: the file is renamed correctly and
// the CONFIG is what's stale.

import { execSync } from 'node:child_process';
import { readdirSync } from 'node:fs';

const SUITES = [
  ['playwright.config.js', 'test/e2e'],
  ['playwright.real.config.js', 'test/e2e-real'],
];
const IS_SPEC = /\.spec\.[cm]?[jt]s$/;

// Playwright nests suites per file and per describe block; the `file` key
// appears at every level, so collect it recursively rather than assuming a
// depth that a reporter change could invalidate.
function collectFiles(node, into) {
  if (Array.isArray(node)) {
    for (const n of node) collectFiles(n, into);
    return into;
  }
  if (!node || typeof node !== 'object') return into;
  if (typeof node.file === 'string' && IS_SPEC.test(node.file))
    into.add(node.file.replaceAll('\\', '/'));
  for (const v of Object.values(node)) collectFiles(v, into);
  return into;
}

let failed = false;
for (const [config, dir] of SUITES) {
  const onDisk = readdirSync(dir, { recursive: true })
    .map((f) => String(f).replaceAll('\\', '/'))
    .filter((f) => IS_SPEC.test(f))
    .sort();

  const out = execSync(
    `npx playwright test --config=${config} --list --reporter=json`,
    {
      encoding: 'utf8',
      maxBuffer: 64 * 1024 * 1024,
      stdio: ['ignore', 'pipe', 'inherit'],
    },
  );
  // Report paths are already relative to testDir, which is the same shape
  // readdirSync gives — so the two sets compare directly.
  const discovered = collectFiles(JSON.parse(out), new Set());

  const missing = onDisk.filter((f) => !discovered.has(f));
  if (missing.length) {
    failed = true;
    console.error(
      `${config}: ${missing.length} spec file(s) on disk that ${config} does NOT collect:`,
    );
    for (const f of missing) console.error(`  ${dir}/${f}`);
  } else {
    console.log(`${config}: all ${onDisk.length} spec files collected`);
  }
}

process.exit(failed ? 1 : 0);
