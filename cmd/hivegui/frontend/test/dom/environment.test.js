// Guardrail: the vitest config's `dom` project must provide a jsdom
// environment to every test/dom/ file BY DIRECTORY — without a
// per-file `// @vitest-environment jsdom` magic comment.
//
// This file deliberately omits that comment. If a future vitest
// upgrade or config edit breaks the directory→environment routing
// (as vitest 4 silently did when it removed `environmentMatchGlobs`),
// `document` will be undefined here and this test fails — catching the
// regression instead of letting other dom tests pass for the wrong
// reason or silently degrade to the node environment.
import { test, expect } from 'vitest';

test('test/dom/ files run under jsdom by directory routing, no magic comment required', () => {
  expect(typeof document, 'document should exist (jsdom env)').toBe('object');
  expect(typeof window, 'window should exist (jsdom env)').toBe('object');
});
