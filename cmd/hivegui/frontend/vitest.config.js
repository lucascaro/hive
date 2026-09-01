import { defineConfig } from 'vitest/config';

// Split unit vs DOM suites by directory:
//   test/unit/   → pure modules, node env, fast
//   test/dom/    → jsdom + xterm/wails mocks
//
// vitest 4 removed `environmentMatchGlobs`, so the directory→env
// routing is expressed as two projects instead. This keeps the
// guardrail automatic: a new test/dom/ file gets jsdom even if its
// author forgets the `// @vitest-environment jsdom` magic comment
// (the comment still works and overrides, but is no longer required).
// `npm test` (`vitest run`) runs both projects and reports a combined
// total. Playwright specs (test/e2e*, *.spec.*) are excluded by the
// `*.test.*` include and run via their own runner.
//
// The includes match .js, .ts and .tsx: the tree is mid-migration to
// TypeScript (docs/exec-plans/active/typescript-migration.md), the React
// rewrite adds .tsx suites on top of that, and a
// converted test that stops matching its glob vanishes *silently* — the
// suite just gets smaller. Do not narrow these back to `.js`.
export default defineConfig({
  test: {
    globals: false,
    reporters: 'default',
    projects: [
      {
        extends: true,
        test: {
          name: 'unit',
          include: ['test/unit/**/*.test.{js,ts,tsx}'],
          environment: 'node',
        },
      },
      {
        extends: true,
        test: {
          name: 'dom',
          include: ['test/dom/**/*.test.{js,ts,tsx}'],
          environment: 'jsdom',
          // Registers @testing-library/jest-dom's matchers (toHaveClass,
          // toBeVisible, …) for the React Testing Library suites the UI
          // rewrite adds. Harmless for the existing imperative tests.
          setupFiles: ['./test/dom/setup-rtl.ts'],
        },
      },
    ],
  },
});
