import { defineConfig } from '@playwright/test';

// Layer B: Playwright suite running against a REAL hived daemon via
// hived-ws-bridge. globalSetup.mjs spawns the daemon + bridge with
// fully isolated temp paths (HOME / HIVE_STATE_DIR / HIVE_SOCKET) and
// writes the bridge URL to process.env.WS_BRIDGE_URL. The Vite dev
// server boots with VITE_WAILS_REAL=1, which makes vite resolve the
// Wails App + runtime imports to test/e2e-real/wails-bridge.ts
// instead of the in-browser mock.
//
// Specs read process.env.WS_BRIDGE_URL inside a Playwright addInitScript
// to install window.__WS_BRIDGE_URL before main.ts loads.

export default defineConfig({
  testDir: './test/e2e-real',
  testMatch: '**/*.spec.{js,ts}',
  fullyParallel: false,
  // Real-daemon tests are slower than the mock — give them more room.
  timeout: 90000,
  workers: 1,
  // One retry on CI, but `failOnFlakyTests` means a retry buys diagnostics
  // (both attempts' traces), NOT a green check. Spec 245's re-gate criterion
  // is first-attempt green; letting a retry paper over a failure is what let
  // three specs sit quarantined for weeks while they were deterministically
  // stale rather than flaky.
  retries: process.env.CI ? 1 : 0,
  failOnFlakyTests: !!process.env.CI,
  globalSetup: './test/e2e-real/globalSetup.mjs',
  globalTeardown: './test/e2e-real/globalTeardown.mjs',
  use: {
    baseURL: 'http://localhost:5175',
    actionTimeout: 10000,
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'VITE_WAILS_REAL=1 VITE_PORT=5175 ./node_modules/.bin/vite',
    url: 'http://localhost:5175',
    reuseExistingServer: !process.env.CI,
    timeout: 30000,
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
});
