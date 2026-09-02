import { afterEach } from 'vitest';
import { cleanup } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { cancelInlineRename } from '../../src/app/inline-rename.js';

// RTL registers its own afterEach(cleanup) ONLY when a global `afterEach`
// exists — and vitest.config.js sets `globals: false`, so it never does.
// Without this, every render() leaves a mounted root behind: React trees
// accumulate across a file's tests and, worse, module-level singletons
// keep pointing into them.
afterEach(() => {
  // app/inline-rename.ts holds the open editor in a module-level `active`.
  // A test that leaves a rename open leaks it into the next one, where the
  // next beginInlineRename mounts over a node the previous test detached —
  // so a regression in the second-and-later open would still go green.
  // Cancelled BEFORE cleanup, so the editor unmounts against a live tree.
  cancelInlineRename();
  cleanup();
});
