// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { describe, it, expect } from 'vitest';
import { parsePrincipal } from '../../src/lib/principal.js';

// The cases mirror TestParsePrincipal in internal/wire/request_test.go:
// the Go parser and this one must agree on every input.
describe('parsePrincipal', () => {
  const cases: [string, string, string][] = [
    ['', '', ''],
    ['user', '', ''],
    ['plugin:engine', 'engine', ''],
    ['plugin:engine/workflow:r1', 'engine', 'r1'],
    ['plugin:engine/workflow:run-2-a', 'engine', 'run-2-a'],
    ['plugin:', '', ''],
    ['plugin:/workflow:r1', '', ''],
    ['plugin:engine/workflow:', '', ''],
    ['plugin:engine/workflow:R1', '', ''],
    ['plugin:x/../plugin:q', '', ''],
    ['plugin:a:b', '', ''],
    ['session:abc', '', ''],
    ['workflow:r1', '', ''],
  ];
  for (const [input, plugin, run] of cases) {
    it(`${JSON.stringify(input)}`, () => {
      expect(parsePrincipal(input)).toEqual({ plugin, run });
    });
  }
});
