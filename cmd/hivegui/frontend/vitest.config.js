import { defineConfig } from 'vitest/config';

// Split unit vs DOM suites by directory:
//   test/unit/   → pure modules, node env, fast
//   test/dom/    → jsdom + xterm/wails mocks
//
// The default `npm test` runs both; tweak with --dir.
export default defineConfig({
  test: {
    environmentMatchGlobs: [
      ['test/dom/**', 'jsdom'],
      ['test/unit/**', 'node'],
    ],
    include: ['test/**/*.test.js'],
    globals: false,
    reporters: 'default',
  },
});
