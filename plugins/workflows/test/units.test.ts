// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The small pure pieces: encoded size, redaction, event caps, push batches and
// instance keys.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batchForPush, capData, MAX_EVENT_BYTES, prepareEvent, type RunEvent } from '../engine/events.ts';
import { buildIndex, encodeKey, keyOf, parseKey } from '../engine/keys.ts';
import { redactString, redactValue } from '../engine/redact.ts';
import { encodedSize, MAX_MESSAGE_BYTES, truncateEncoded } from '../engine/size.ts';
import { encodeWorkflowKey } from '../hive-plugin.mjs';
import { examples } from '../tools/examples.ts';

// ----------------------------------------------------------------------- size

test('encodedSize counts what Go sends: < > & and U+2028/2029 grow to six bytes', () => {
  assert.equal(encodedSize('abc'), 5);
  assert.equal(encodedSize('<'.repeat(16 * 1024)), 16 * 1024 * 6 + 2, '16 KiB of < is 96 KiB on the wire');
  assert.equal(encodedSize('a&b>c'), 2 + 3 + 12, 'two escapes cost six bytes each');
  assert.equal(encodedSize(' '), 6 + 2);
  assert.equal(encodedSize('é'), 2 + 2, 'UTF-8, not UTF-16');
  assert.equal(encodedSize(undefined), 0);
  assert.equal(encodedSize({ a: ['<'] }), '{"a":["\\u003c"]}'.length);
});

test('truncateEncoded cuts to the encoded budget without splitting a surrogate pair', () => {
  const r = truncateEncoded('<'.repeat(100), 62);
  assert.equal(r.truncated, true);
  assert.ok(encodedSize(r.text) <= 62);
  assert.equal(r.text.length, 10);
  const pair = truncateEncoded('😀😀😀', 2 + 4 + 1);
  assert.equal(pair.text, '😀', 'a lone high surrogate is dropped');
  assert.deepEqual(truncateEncoded('ok', 100), { text: 'ok', truncated: false });
});

// --------------------------------------------------------------------- redact

test('redaction: one case per pattern, and a count', () => {
  const cases: [string, string][] = [
    ['-----BEGIN RSA PRIVATE KEY-----\nMIIEow\nIBAAKC\n-----END RSA PRIVATE KEY-----', 'a PEM private-key block'],
    ['-----BEGIN PRIVATE KEY-----\nMIIEvQ', 'a PEM block cut off before its END'],
    ['-----BEGIN OPENSSH KEY-----', 'a lone BEGIN KEY header'],
    ['AKIAABCDEFGHIJKLMNOP', 'an AWS access key id'],
    [`ghp_${'a'.repeat(36)}`, 'a GitHub token'],
    [`gho_${'b'.repeat(40)}`, 'a GitHub OAuth token'],
    [`github_pat_${'c'.repeat(30)}`, 'a fine-grained PAT'],
    [`sk-${'d'.repeat(30)}`, 'an sk- key'],
    ['xoxb-1234567890-abcdefghij', 'a Slack token'],
    ['Authorization: Bearer abc.def.ghi', 'an Authorization header'],
  ];
  for (const [secret, what] of cases) {
    const r = redactString(`before ${secret} after`);
    assert.ok(!r.text.includes(secret.slice(-12)) || secret.startsWith('-----BEGIN OPENSSH'), `${what} is gone: ${r.text}`);
    assert.match(r.text, /\[redacted\]/, what);
    assert.ok(r.count >= 1, what);
  }
  assert.deepEqual(redactString('nothing to see'), { text: 'nothing to see', count: 0 });
  assert.equal(redactString('Authorization: Bearer x\nnext line stays').text, '[redacted]\nnext line stays', 'the header, not the line after it');
});

test('redactValue walks objects and arrays, leaves keys and non-strings, and counts', () => {
  const r = redactValue({ a: ['k AKIAABCDEFGHIJKLMNOP', 3, null], AKIAABCDEFGHIJKLMNOP: true, n: { s: 'ok' } });
  assert.deepEqual(r.value, { a: ['k [redacted]', 3, null], AKIAABCDEFGHIJKLMNOP: true, n: { s: 'ok' } });
  assert.equal(r.count, 1);
});

// ----------------------------------------------------------------------- events

const draft = (type: Parameters<typeof prepareEvent>[0]['type'], data: Record<string, unknown>) => ({ key: 'a', node_id: 'a', attempt: 1, type, data });

test('prepareEvent redacts first and notes the count, then caps', () => {
  const e = prepareEvent(draft('check.output', { stream: 'stdout', text: `${'<'.repeat(20_000)} AKIAABCDEFGHIJKLMNOP` }));
  assert.equal(e.data.redacted, 1);
  assert.equal(e.data.truncated, true);
  assert.ok(encodedSize(e.data) <= MAX_EVENT_BYTES);
  // A secret straddling the cut is still redacted, because redaction ran on the whole text.
  const cut = prepareEvent(draft('agent.message', { session_id: 's', item_id: 'i', text: `${'x'.repeat(16_300)} AKIAABCDEFGHIJKLMNOP${'<'.repeat(100)}` }));
  assert.ok(!String(cut.data.text).includes('AKIA'));
  assert.match(String(cut.data.text), /\[redacted\]/);
  assert.equal(cut.data.truncated, true);
});

test('capData: node.output keeps a prefix, agent.plan drops trailing entries, anything else halves its longest string', () => {
  const out = capData('node.output', { output: { text: '<'.repeat(10_000) } });
  assert.equal(out.truncated, true);
  assert.ok(encodedSize(out.output) <= 16 * 1024);
  const small = capData('node.output', { output: { ok: true } });
  assert.deepEqual(small, { output: { ok: true } });
  const plan = capData('agent.plan', { session_id: 's', entries: Array.from({ length: 500 }, () => ({ content: 'x'.repeat(100), status: 'pending' })) });
  assert.ok((plan.entries as unknown[]).length < 500 && (plan.entries_omitted as number) > 0);
  assert.ok(encodedSize(plan) <= MAX_EVENT_BYTES);
  const odd = capData('run.finished', { state: 'failed', error: '<'.repeat(10_000) });
  assert.ok(encodedSize(odd) <= MAX_EVENT_BYTES);
  assert.equal(odd.truncated, true);
});

test('batchForPush keeps order and every batch under 48 KiB as sent', () => {
  const evs: RunEvent[] = Array.from({ length: 40 }, (_, i) => ({ run_id: 'r1', seq: i + 1, ts: 't', key: 'a', node_id: 'a', attempt: 1, type: 'check.output', data: { stream: 'stdout', text: '<'.repeat(1500) } }));
  const batches = batchForPush('r1', evs);
  assert.ok(batches.length > 3);
  assert.deepEqual(batches.flat().map((e) => e.seq), evs.map((e) => e.seq));
  for (const b of batches) {
    assert.ok(b.length >= 1);
    assert.ok(encodedSize({ plugin_id: 'workflows', topic: 'run.event', data: { run_id: 'r1', events: b } }) <= MAX_MESSAGE_BYTES);
  }
  assert.deepEqual(batchForPush('r1', []), []);
});

// ------------------------------------------------------------------------ keys

test('keys put [i] after each loop (from 1) and map (from 0) on the path, and parse back', () => {
  const ix = buildIndex(examples.planImplementVerify());
  assert.equal(keyOf(ix, 'feature/build/work', [2]), 'feature/build[2]/work');
  assert.equal(keyOf(ix, 'feature/plan', []), 'feature/plan');
  assert.equal(keyOf(ix, 'feature/build', []), 'feature/build', 'a container carries no index of its own');
  assert.deepEqual(parseKey(ix, 'feature/build[2]/work'), { id: 'feature/build/work', ctx: [2] });
  assert.equal(parseKey(ix, 'feature/build[2]/nope'), null);
  assert.equal(parseKey(ix, 'feature/build/work'), null, 'a loop body node needs its iteration');
  const fan = buildIndex(examples.fanOutThenVerify());
  assert.equal(keyOf(fan, 'work/each/do', [3]), 'work/each[3]/do');
  assert.throws(() => keyOf(fan, 'work/each/do', []), /needs 1 indices/);
});

test('encodeKey is the SDK encoder: fix[2]/work becomes fix_2/work', () => {
  for (const k of ['fix[2]/work', 'work/each[3]/do', 'a', 'a[0]/b[12]/c']) assert.equal(encodeKey(k), encodeWorkflowKey(k), k);
  assert.equal(encodeKey('fix[2]/work'), 'fix_2/work');
});
