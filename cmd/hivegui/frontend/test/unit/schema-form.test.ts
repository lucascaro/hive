// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { describe, expect, it } from 'vitest';
import {
  describe as describeSchema,
  initialRaw,
  toValue,
} from '../../src/lib/schema-form.js';
import type { JsonSchema } from '../../src/lib/workflow-types.js';

const schema: JsonSchema = {
  type: 'object',
  additionalProperties: false,
  required: ['task', 'verdict'],
  properties: {
    task: { type: 'string' },
    verdict: { type: 'string', enum: ['approve', 'revise'] },
    max_files: { type: 'number' },
    strict: { type: 'boolean' },
    tags: { type: 'array', items: { type: 'string' } },
    sizes: { type: 'array', items: { type: 'number' } },
    steps: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['name'],
        properties: { name: { type: 'string' } },
      },
    },
    opts: {
      type: 'object',
      additionalProperties: false,
      required: [],
      properties: { depth: { type: 'number' } },
    },
  },
};

describe('describe', () => {
  it('describes string enum number boolean array object', () => {
    const f = Object.fromEntries(
      describeSchema(schema).map((x) => [x.name, x]),
    );
    expect(f.task).toMatchObject({ kind: 'string', required: true });
    expect(f.verdict).toMatchObject({
      kind: 'enum',
      options: ['approve', 'revise'],
    });
    expect(f.max_files).toMatchObject({ kind: 'number', required: false });
    expect(f.strict).toMatchObject({ kind: 'boolean' });
    expect(f.tags).toMatchObject({ kind: 'list', item: 'string' });
    expect(f.sizes).toMatchObject({ kind: 'list', item: 'number' });
    expect(f.steps).toMatchObject({ kind: 'json' });
    expect(f.opts.fields?.[0]).toMatchObject({ name: 'depth', kind: 'number' });
  });

  it('required follows the schema', () => {
    const req = describeSchema(schema)
      .filter((x) => x.required)
      .map((x) => x.name);
    expect(req).toEqual(['task', 'verdict']);
  });

  it('a non-object root becomes one JSON field', () => {
    expect(describeSchema({ type: 'number' })).toEqual([
      { name: 'value', kind: 'number', required: true },
    ]);
  });
});

describe('toValue', () => {
  it('coerces numbers and one-per-line arrays', () => {
    const raw = {
      ...initialRaw(describeSchema(schema)),
      task: 'fix it',
      verdict: 'approve',
      max_files: '10',
      strict: true,
      tags: 'a\n\nb\r\n',
      sizes: '1\n2.5',
    };
    expect(toValue(schema, raw)).toEqual({
      value: {
        task: 'fix it',
        verdict: 'approve',
        max_files: 10,
        strict: true,
        tags: ['a', 'b'],
        sizes: [1, 2.5],
      },
      errors: [],
    });
  });

  it('omits empty optional fields but keeps required ones', () => {
    const { value } = toValue(schema, initialRaw(describeSchema(schema)));
    expect(value).toEqual({ task: '', verdict: '', strict: false });
  });

  it('array of objects parses JSON and reports a parse error', () => {
    const base = initialRaw(describeSchema(schema));
    expect(
      toValue(schema, { ...base, steps: '[{"name":"a"}]' }).value,
    ).toMatchObject({
      steps: [{ name: 'a' }],
    });
    const bad = toValue(schema, { ...base, steps: '[{' });
    expect(bad.errors).toEqual(['steps: not valid JSON']);
  });

  it('reports a number that is not one', () => {
    const base = initialRaw(describeSchema(schema));
    expect(toValue(schema, { ...base, max_files: 'ten' }).errors).toEqual([
      'max_files: not a number',
    ]);
    expect(toValue(schema, { ...base, sizes: '1\nx' }).errors).toEqual([
      'sizes: every line must be a number',
    ]);
  });

  it('nests objects and drops an empty optional one', () => {
    const base = initialRaw(describeSchema(schema));
    expect(toValue(schema, base).value).not.toHaveProperty('opts');
    expect(
      toValue(schema, { ...base, opts: { depth: '3' } }).value,
    ).toMatchObject({ opts: { depth: 3 } });
  });
});
