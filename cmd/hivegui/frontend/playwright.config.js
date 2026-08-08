import { defineConfig } from '@playwright/test';

// Boots `vite dev` with VITE_WAILS_MOCK=1, so the frontend loads
// against the in-browser fake of the Wails bridge defined in
// test/e2e/wails-mock.ts. Tests drive the UI and can inject daemon
// events through window.__hive.
export default defineConfig({
  testDir: './test/e2e',
  testMatch: '**/*.spec.{js,ts}',
  fullyParallel: false,
  timeout: 30000,
  // One retry on CI so a one-off flake doesn't fail the required leg;
  // first-attempt artifacts (trace attachment) are kept.
  retries: process.env.CI ? 1 : 0,
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
