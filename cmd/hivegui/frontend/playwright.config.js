import { defineConfig } from '@playwright/test';

// Boots `vite dev` with VITE_WAILS_MOCK=1, so the frontend loads
// against the in-browser fake of the Wails bridge defined in
// test/e2e/wails-mock.ts. Tests drive the UI and can inject daemon
// events through window.__hive.
export default defineConfig({
  testDir: './test/e2e',
  testMatch: '**/*.spec.{js,ts}',
  // EXPERIMENT (see PR #345) — revert this hunk if CI does not improve.
  //
  // With `fullyParallel: false` each FILE is a serial chain pinned to one
  // worker, so wall clock floors at the longest single file. This suite is
  // badly unbalanced: theme.spec.ts is 59 tests, focus-traps.spec.ts 41,
  // worktrees.spec.ts 40 — 140 of 303 in three files. That is why simply
  // raising the worker count bought exactly nothing (Linux: 4.9m on 2
  // workers, 4.9m on 4) and cost Windows 2.3m: there was no second unit of
  // work for the extra workers to pick up, only more contention.
  //
  // Parallelising WITHIN files is the only thing that lowers that floor.
  // Verified green locally at 4 workers (272 passed, 3 runs), but local has
  // far more cores than a 4-vCPU runner, so the local timing says nothing
  // about the CI gain. This PR's own CI run is the measurement.
  fullyParallel: true,
  workers: process.env.CI ? 4 : undefined,
  timeout: 30000,
  // One retry on CI, but `failOnFlakyTests` means a retry buys diagnostics
  // (both attempts' traces), NOT a green check. Spec 245's re-gate criterion
  // is first-attempt green; letting a retry paper over a failure is what let
  // three specs sit quarantined for weeks while they were deterministically
  // stale rather than flaky.
  retries: process.env.CI ? 1 : 0,
  failOnFlakyTests: !!process.env.CI,
  use: {
    baseURL: 'http://localhost:5174',
    actionTimeout: 5000,
    trace: 'retain-on-failure',
  },
  webServer: {
    // env vars via `env` (not shell prefix) so this also starts on Windows.
    command: 'npx vite',
    env: { VITE_WAILS_MOCK: '1', VITE_PORT: '5174' },
    url: 'http://localhost:5174',
    reuseExistingServer: !process.env.CI,
    timeout: 30000,
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
});
