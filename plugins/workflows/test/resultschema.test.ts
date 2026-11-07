// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { checkSchema, checkValue } from '../ir/resultschema.ts';

const dir = join(dirname(fileURLToPath(import.meta.url)), '../../../internal/acp/testdata/resultschema');

test("matches A's fixtures", () => {
  const files = readdirSync(dir).filter((f) => f.endsWith('.json'));
  assert.ok(files.length >= 6, 'fixtures not found');
  for (const f of files) {
    const fx = JSON.parse(readFileSync(join(dir, f), 'utf8'));
    assert.equal(checkSchema(fx.schema), null, `${f}: schema refused`);
    const err = checkValue(fx.schema, fx.value);
    if (fx.ok) assert.equal(err, null, f);
    else assert.equal(err, fx.error, f);
  }
});

const obj = (properties: Record<string, unknown>) => ({
  type: 'object',
  properties,
  required: Object.keys(properties),
  additionalProperties: false,
});

test('rejects a schema the daemon would refuse at CREATE', () => {
  const bad: [string, unknown, RegExp][] = [
    ['array at the top', { type: 'array', items: { type: 'string' } }, /top level must be an object/],
    [
      'partial required',
      { ...obj({ a: { type: 'string' }, b: { type: 'string' } }), required: ['a'] },
      /must list every property \("b" is missing\)/,
    ],
    ['required names a ghost', { ...obj({ a: { type: 'string' } }), required: ['a', 'z'] }, /"z" is not a property/],
    ['additionalProperties true', { ...obj({ a: { type: 'string' } }), additionalProperties: true }, /additionalProperties must be false/],
    ['unsupported keyword', obj({ a: { type: 'string', pattern: 'x' } }), /"pattern" is not supported on a string schema/],
    ['empty enum', obj({ a: { type: 'string', enum: [] } }), /non-empty array of strings/],
    ['array without items', obj({ a: { type: 'array' } }), /needs items/],
    ['unknown type', obj({ a: { type: 'integer' } }), /type must be string/],
    ['non-object', 'x', /must be an object/],
  ];
  for (const [name, schema, re] of bad) assert.match(checkSchema(schema) ?? '', re, name);
});

test('depth: 8 levels pass, 9 fail (the leaf string counts)', () => {
  const nest = (n: number): unknown => (n === 1 ? obj({ a: { type: 'string' } }) : obj({ a: nest(n - 1) }));
  assert.equal(checkSchema(nest(7)), null);
  assert.match(checkSchema(nest(8)) ?? '', /nested deeper than 8 levels/);
});

test('size: over 8 KiB fails, counting UTF-8 bytes', () => {
  // "é" is two bytes in UTF-8 but one UTF-16 unit: one 2,500-char enum fits, two do not.
  const e = 'é'.repeat(2500);
  assert.equal(checkSchema(obj({ a: { type: 'string', enum: [e] } })), null);
  assert.match(checkSchema(obj({ a: { type: 'string', enum: [e, e] } })) ?? '', /larger than 8192 bytes/);
});

test('values: infinite number, __proto__ key and array path', () => {
  const s = obj({ n: { type: 'number' }, xs: { type: 'array', items: { type: 'boolean' } } });
  assert.match(checkValue(s, JSON.parse('{"n":1e400,"xs":[]}')) ?? '', /data\.n: Infinity is out of range/);
  assert.equal(checkValue(s, JSON.parse('{"n":1,"xs":[true,1]}')), 'data.xs[1]: expected boolean, got number');
  assert.equal(checkValue(s, JSON.parse('{"n":1,"xs":[],"__proto__":1}')), 'data.__proto__: unexpected field');
});
