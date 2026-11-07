// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_IR_BYTES, MAX_NESTING, shape } from '../ir/shape.ts';
import { examples } from '../tools/examples.ts';

// biome-ignore lint/suspicious/noExplicitAny: tests mutate a parsed IR freely
type Any = any;
const fresh = (name = 'reviewLoop'): Any => JSON.parse(JSON.stringify(examples[name]()));
const findKind = (nodes: Any[], kind: string): Any => {
  for (const n of nodes) {
    if (n.kind === kind) return n;
    const inner = n.nodes && findKind(n.nodes, kind);
    if (inner) return inner;
  }
};
const agentOf = (ir: Any): Any => findKind(ir.nodes, 'agent');
const text = (ir: unknown) => shape(ir).join('\n');

test('accepts the three library IRs', () => {
  for (const name of Object.keys(examples)) assert.deepEqual(shape(fresh(name)), [], name);
});

test('rejects what is not an object', () => {
  for (const v of [null, 'x', 3, [], undefined]) assert.deepEqual(shape(v), ['ir: must be an object']);
});

test('rejects unknown kind', () => {
  const ir = fresh();
  ir.nodes[0].kind = 'agnt';
  assert.match(text(ir), /kind: must be agent, check, human, group, loop or map/);
});

test('rejects extra key', () => {
  const top = fresh();
  top.extra = 1;
  assert.match(text(top), /ir: unexpected key "extra"/);
  const inner = fresh();
  agentOf(inner).sneaky = true;
  assert.match(text(inner), /unexpected key "sneaky"/);
  const edge = fresh();
  findKind(edge.nodes, 'loop').edges[0].why = 1;
  assert.match(text(edge), /unexpected key "why"/);
});

test('rejects a missing key', () => {
  const ir = fresh();
  delete agentOf(ir).prompt;
  assert.match(text(ir), /missing key "prompt"/);
});

test('rejects bad scalar types', () => {
  const ir = fresh();
  const a = agentOf(ir);
  a.timeout_s = 0;
  a.agent = '';
  a.mode = 3;
  a.worktree = 'shared';
  const out = text(ir);
  assert.match(out, /timeout_s: must be a number > 0/);
  assert.match(out, /agent: must be a non-empty string/);
  assert.match(out, /mode: must be a string/);
  assert.match(out, /worktree: must be "own" or \{of\}/);
});

test('rejects bad ids, loop and map bounds', () => {
  const ir = fresh();
  const loop = findKind(ir.nodes, 'loop');
  ir.nodes[0].id = 'Fix';
  loop.max_iters = 21;
  assert.match(text(ir), /id: must be an id/);
  assert.match(text(ir), /max_iters: must be an integer in 1\.\.20/);
  const fan = fresh('fanOutThenVerify');
  findKind(fan.nodes, 'map').concurrency = 17;
  assert.match(text(fan), /concurrency: must be an integer in 1\.\.16/);
});

test('rejects a malformed condition', () => {
  const ir = fresh();
  const loop = findKind(ir.nodes, 'loop');
  loop.until = { ref: 'x.y', op: 'matches' };
  assert.match(text(ir), /until\.op: must be one of eq\|ne\|in\|truthy\|lt\|gt/);
  loop.until = { all: 'x' };
  assert.match(text(ir), /until\.all: must be an array/);
  loop.until = { not: { ref: 'a.b', op: 'eq', value: { deep: [Number.POSITIVE_INFINITY] } } };
  assert.match(text(ir), /not a finite number/);
});

test('rejects an output schema deeper than 8', () => {
  const ir = fresh();
  let s: Any = { type: 'object', properties: { a: { type: 'string' } }, required: ['a'], additionalProperties: false };
  for (let i = 0; i < 8; i++) s = { type: 'object', properties: { a: s }, required: ['a'], additionalProperties: false };
  agentOf(ir).output = s;
  assert.match(text(ir), /output.*nested deeper than 8 levels/);
});

test('rejects an output schema over 8 KiB', () => {
  const ir = fresh();
  const props: Record<string, unknown> = {};
  for (let i = 0; i < 400; i++) props[`field${i}`] = { type: 'string' };
  agentOf(ir).output = { type: 'object', properties: props, required: Object.keys(props), additionalProperties: false };
  assert.match(text(ir), /output: larger than 8192 bytes/);
});

test('rejects partial required', () => {
  const ir = fresh();
  const a = agentOf(ir);
  a.output.required = a.output.required.slice(1);
  assert.match(text(ir), /output.required: must list every property/);
});

test('rejects additionalProperties true', () => {
  const ir = fresh();
  agentOf(ir).output.additionalProperties = true;
  assert.match(text(ir), /output: additionalProperties must be false/);
});

test('applies the same limits to human outputs and the run input', () => {
  const ir = fresh('planImplementVerify');
  ir.input.additionalProperties = true;
  assert.match(text(ir), /ir\.input: additionalProperties must be false/);
});

test('bounds nesting of nodes and conditions', () => {
  const group = (inner: Any) => ({ id: 'g', kind: 'group', nodes: [inner], edges: [] });
  let n: Any = { id: 'c', kind: 'check', argv: ['true'], timeout_s: 1 };
  for (let i = 0; i < MAX_NESTING + 2; i++) n = group(n);
  const ir = { ir: 1, name: 'deep', input: fresh().input, nodes: [n], edges: [] };
  assert.match(text(ir), /nested deeper than 32 levels/);

  let c: Any = { ref: 'a.b', op: 'truthy' };
  for (let i = 0; i < MAX_NESTING + 2; i++) c = { not: c };
  const ir2 = fresh();
  findKind(ir2.nodes, 'loop').until = c;
  assert.match(text(ir2), /condition nested deeper than 32 levels/);
});

test('caps the whole IR at 32 KiB of JSON', () => {
  const at = (n: number) => {
    const ir = fresh();
    agentOf(ir).prompt = '';
    const room = n - new TextEncoder().encode(JSON.stringify(ir)).length;
    agentOf(ir).prompt = 'é'.repeat(Math.floor(room / 2)) + (room % 2 ? 'x' : '');
    return ir;
  };
  const exact = at(MAX_IR_BYTES);
  assert.equal(new TextEncoder().encode(JSON.stringify(exact)).length, MAX_IR_BYTES);
  assert.deepEqual(shape(exact), [], 'exactly at the limit passes');
  agentOf(exact).prompt += 'x';
  assert.deepEqual(shape(exact), [`ir: too large (${MAX_IR_BYTES + 1} bytes of JSON, the limit is ${MAX_IR_BYTES})`]);
});
