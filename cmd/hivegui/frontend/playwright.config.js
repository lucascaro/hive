import { defineConfig } from '@playwright/test';

// Boots `vite dev` with VITE_WAILS_MOCK=1, so the frontend loads
// against the in-browser fake of the Wails bridge defined in
// test/e2e/wails-mock.js. Tests drive the UI and can inject daemon
// events through window.__hive.
export default defineConfig({
  testDir: './test/e2e',
  testMatch: '**/*.spec.js',
  fullyParallel: false,
  timeout: 30000,
  use: {
    baseURL: 'http://localhost:5174',
    actionTimeout: 5000,
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'VITE_WAILS_MOCK=1 VITE_PORT=5174 ./node_modules/.bin/vite',
    url: 'http://localhost:5174',
    reuseExistingServer: !process.env.CI,
    timeout: 30000,
  },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
  ],
});
