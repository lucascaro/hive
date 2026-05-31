import { defineConfig } from '@playwright/test';

// Layer B: Playwright suite running against a REAL hived daemon via
// hived-ws-bridge. globalSetup.js spawns the daemon + bridge with
// fully isolated temp paths (HOME / HIVE_STATE_DIR / HIVE_SOCKET) and
// writes the bridge URL to process.env.WS_BRIDGE_URL. The Vite dev
// server boots with VITE_WAILS_REAL=1, which makes vite resolve the
// Wails App + runtime imports to test/e2e-real/wails-bridge.js
// instead of the in-browser mock.
//
// Specs read process.env.WS_BRIDGE_URL inside a Playwright addInitScript
// to install window.__WS_BRIDGE_URL before main.js loads.

export default defineConfig({
  testDir: './test/e2e-real',
  testMatch: '**/*.spec.js',
  fullyParallel: false,
  // Real-daemon tests are slower than the mock — give them more room.
  timeout: 60000,
  workers: 1,
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
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
  ],
});
