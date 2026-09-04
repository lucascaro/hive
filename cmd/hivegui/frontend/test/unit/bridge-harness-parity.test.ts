// The two Playwright harnesses (test/e2e/wails-mock.ts and
// test/e2e-real/wails-bridge.ts) are SUBSTITUTED for the generated
// wailsjs modules by the resolveId plugin in vite.config.js. So a name
// src/bridge.ts re-exports but a harness does not define is not a
// missing method that fails one assertion — it is a module-load error.
// The app never boots, every spec sits on the boot overlay until it
// times out, and a 292-spec suite takes 40 minutes to say so on three
// platforms at once. That happened; this is the guard.
//
// Checked here rather than by tsc: the harnesses are only ever type-
// checked as ordinary modules, never against the specifier they stand
// in for, so nothing else compares the two surfaces.
//
// Sources arrive through Vite's `?raw`, not node:fs: tsconfig's `types`
// is ["vite/client"] on purpose (see test/e2e-real/node-shim.d.ts for
// why pulling in @types/node is not free), and `?raw` is already typed
// by vite/client. It also means a moved or deleted harness fails this
// test at import time rather than at a path that silently reads
// nothing.
import { describe, it, expect } from 'vitest';
import bridgeSrc from '../../src/bridge.ts?raw';
import mockSrc from '../e2e/wails-mock.ts?raw';
import realSrc from '../e2e-real/wails-bridge.ts?raw';

/** Names src/bridge.ts re-exports from the two substituted specifiers. */
function bridgeNames(): string[] {
  const src = bridgeSrc;
  const names: string[] = [];
  // export { A, B, C } from '../wailsjs/...'
  const re = /export\s*\{([^}]*)\}\s*from\s*'\.\.\/wailsjs\/[^']*'/g;
  for (const m of src.matchAll(re)) {
    for (const raw of m[1].split(',')) {
      const name = raw
        .trim()
        .split(/\s+as\s+/)[0]
        .trim();
      if (name) names.push(name);
    }
  }
  return names;
}

/** Value names a harness module exports. */
function harnessExports(src: string): Set<string> {
  const out = new Set<string>();
  for (const m of src.matchAll(
    /^export\s+(?:async\s+)?(?:function|const|let|class)\s+([A-Za-z0-9_$]+)/gm,
  )) {
    out.add(m[1]);
  }
  // `export { a, b }` / `export { a as b }`
  for (const m of src.matchAll(/^export\s*\{([^}]*)\}/gm)) {
    for (const raw of m[1].split(',')) {
      const parts = raw.trim().split(/\s+as\s+/);
      const name = (parts[1] ?? parts[0]).trim();
      if (name) out.add(name);
    }
  }
  return out;
}

describe('Playwright harnesses mirror the bridge surface', () => {
  const names = bridgeNames();

  // Guards the guard: a parser that silently matched nothing would
  // make every case below vacuously pass.
  it('finds the re-exported names in bridge.ts', () => {
    expect(names.length).toBeGreaterThan(20);
    expect(names).toContain('RestartDaemon');
  });

  for (const [label, path, src] of [
    ['mock', 'test/e2e/wails-mock.ts', mockSrc],
    ['real', 'test/e2e-real/wails-bridge.ts', realSrc],
  ] as const) {
    it(`${label} harness defines every name bridge.ts re-exports`, () => {
      const has = harnessExports(src);
      const missing = names.filter((n) => !has.has(n));
      expect(
        missing,
        `${path} is missing ${missing.length} export(s). The app will not ` +
          'boot under this harness and every Playwright spec will time out.',
      ).toEqual([]);
    });
  }
});
