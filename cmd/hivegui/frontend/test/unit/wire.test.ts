// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { describe, it, expect } from 'vitest';
import { readProjectId } from '../../src/lib/wire.js';

describe('readProjectId', () => {
  it('reads either case', () => {
    expect(readProjectId({ project_id: 'snake' })).toBe('snake');
    expect(readProjectId({ projectId: 'camel' })).toBe('camel');
    expect(readProjectId({})).toBe('');
  });
});
