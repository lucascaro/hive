// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The check-output stream layer (spec 529, B-5). The oracle is redactString
// over the whole text: however the output is chunked and whatever the ticks,
// what is emitted must equal it, so nothing leaks that whole-text redaction
// would have removed and nothing is lost.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Coalescer, MAX_DISCARD, MAX_HELD_BLOCK, MAX_HELD_LINE } from '../engine/check.ts';
import { redactString } from '../engine/redact.ts';

const manual = () => {
  const fns: (() => void)[] = [];
  return { timers: { set: (fn: () => void) => (fns.push(fn), fns.length), clear: () => {} }, fire: () => fns.splice(0).forEach((f) => f()) };
};

/** Feeds chunks, optionally ticking after each, then ends the stream. */
function run(chunks: string[], ticks: boolean[] = []): { got: string; peak: number; pieces: string[] } {
  const pieces: string[] = [];
  const m = manual();
  const co = new Coalescer((t) => pieces.push(t), m.timers);
  let peak = 0;
  chunks.forEach((c, i) => {
    co.write(c);
    peak = Math.max(peak, co.held);
    if (ticks[i]) m.fire();
  });
  co.end();
  return { got: pieces.join(''), peak, pieces };
}

// ----------------------------------------------------------------- the oracle

function prng(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const ALNUM = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
const rs = (r: () => number, n: number, set = ALNUM): string => Array.from({ length: n }, () => set[Math.floor(r() * set.length)]).join('');

const PEM_BODY = (r: () => number): string => `-----BEGIN RSA PRIVATE KEY-----\n${Array.from({ length: 3 + Math.floor(r() * 4) }, () => rs(r, 64, `${ALNUM}+/`)).join('\n')}\n-----END RSA PRIVATE KEY-----`;

/** [secret text, a substring of it that must not survive]. */
const SECRETS: ((r: () => number) => [string, string])[] = [
  (r) => { const s = `ghp_${rs(r, 36)}`; return [s, s.slice(-14)]; },
  (r) => { const s = `sk-${rs(r, 30)}`; return [s, s.slice(-14)]; },
  (r) => { const s = `xoxb-${rs(r, 10, '0123456789')}-${rs(r, 12)}`; return [s, s.slice(-10)]; },
  (r) => { const s = `AKIA${rs(r, 16, 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567')}`; return [s, s.slice(-10)]; },
  (r) => { const s = `github_pat_${rs(r, 30)}`; return [s, s.slice(-14)]; },
  (r) => { const t = rs(r, 24); return [`Bearer ${t}`, t.slice(-10)]; },
  (r) => { const t = rs(r, 6); return [`Authorization: Bearer ${t}`, t]; },
  (r) => { const t = rs(r, 40, '0123456789abcdef'); return [`Authorization: token ${t}`, t.slice(-14)]; },
  (r) => { const t = rs(r, 14); return [`"token": "${t}"`, t.slice(-8)]; },
  (r) => { const t = rs(r, 12); return [`https://user:${t}@example.com/path`, t.slice(-8)]; },
  (r) => { const body = PEM_BODY(r); return [body, body.split('\n')[2].slice(-20)]; },
];

const WORDS = ['build', 'ok', 'warning:', 'done', '12', 'test', 'PASS', '', 'at', 'line'];

function makeText(r: () => number, items: number): { text: string; cores: string[] } {
  const parts: string[] = [];
  const cores: string[] = [];
  for (let i = 0; i < items; i++) {
    if (r() < 0.45) {
      const [s, core] = SECRETS[Math.floor(r() * SECRETS.length)](r);
      parts.push(s);
      cores.push(core);
    } else parts.push(WORDS[Math.floor(r() * WORDS.length)]);
    parts.push(r() < 0.5 ? '\n' : r() < 0.7 ? ' ' : '');
  }
  if (r() < 0.5) parts.push('\n');
  return { text: parts.join(''), cores };
}

function check(text: string, cores: string[], chunks: string[], ticks: boolean[], what: string): void {
  const want = redactString(text).text;
  const { got, peak } = run(chunks, ticks);
  assert.equal(got, want, `${what}: output differs from whole-text redaction\nchunks ${JSON.stringify(chunks)}`);
  for (const core of cores) if (!want.includes(core)) assert.ok(!got.includes(core), `${what}: ${core} leaked`);
  assert.ok(peak <= MAX_HELD_BLOCK + MAX_HELD_LINE + text.length * 0 + 16_384, `${what}: held ${peak}`);
}

test('oracle: every offset of every short text, with ticks, equals whole-text redaction', () => {
  const r = prng(20_260_513);
  let n = 0;
  for (let t = 0; t < 120; t++) {
    const { text, cores } = makeText(r, 1 + Math.floor(r() * 2));
    if (text.length > 420) continue;
    for (let at = 1; at < text.length; at++) {
      check(text, cores, [text.slice(0, at), text.slice(at)], [r() < 0.5, r() < 0.5], `text ${t} split at ${at}`);
      n++;
    }
  }
  assert.ok(n > 3000, `${n} (text, split) pairs checked`);
});

test('oracle: random multi-way splits of longer texts, with ticks interleaved', () => {
  const r = prng(7_777_001);
  for (let t = 0; t < 250; t++) {
    const { text, cores } = makeText(r, 4 + Math.floor(r() * 14));
    const k = 2 + Math.floor(r() * 7);
    const cuts = [...new Set(Array.from({ length: k - 1 }, () => Math.floor(r() * text.length)))].sort((a, b) => a - b);
    const chunks: string[] = [];
    let prev = 0;
    for (const c of [...cuts, text.length]) {
      chunks.push(text.slice(prev, c));
      prev = c;
    }
    check(text, cores, chunks, chunks.map(() => r() < 0.6), `text ${t}`);
  }
});

test('oracle: byte-at-a-time with a tick after every byte', () => {
  const r = prng(99);
  for (let t = 0; t < 40; t++) {
    const { text, cores } = makeText(r, 3 + Math.floor(r() * 5));
    const chunks = [...text];
    check(text, cores, chunks, chunks.map(() => true), `text ${t}`);
  }
});

// ------------------------------------------------- the reviewer's named probes

test('probe: a partial last line in a buffer of 9 KB of lines is not redacted to a remainder and then continued raw', () => {
  const lines = `${'filler line of ordinary output\n'.repeat(310)}`;
  const first = `${lines}Authorization: Bearer ghp_Z0123456`;
  const second = '789abcdefghijklmnopqrstuvwxy\n';
  assert.ok(first.length > 9000);
  const { got } = run([first, second]);
  assert.equal(got, redactString(first + second).text);
  assert.ok(!got.includes('3456789abc'), got.slice(-80));
});

test('probe: a private key split across two writes with a buffer over 8 KiB does not leak its body', () => {
  const body = Array.from({ length: 5 }, (_, i) => `${'ABCDEFGH'.repeat(8)}${i}`);
  const pem = `-----BEGIN RSA PRIVATE KEY-----\n${body.join('\n')}\n-----END RSA PRIVATE KEY-----\n`;
  const text = `${'filler line of ordinary output\n'.repeat(300)}${pem}after\n`;
  for (const at of [text.indexOf('-----BEGIN') + 40, text.indexOf('-----BEGIN') + 120, text.indexOf('-----END') - 5]) {
    const { got } = run([text.slice(0, at), text.slice(at)], [false, false]);
    assert.equal(got, redactString(text).text);
    assert.ok(!got.includes('ABCDEFGHABCDEFGH'));
  }
});

test('probe: Authorization: token X with no trailing newline is redacted at the end of the stream', () => {
  const t = `Authorization: token ${'0123456789abcdef'.repeat(2)}0123abcd`;
  const { got } = run([t]);
  assert.ok(!got.includes('0123456789abcdef'), got);
  assert.equal(got, '[redacted]');
});

test('probe: a short Authorization: Bearer value split by a tick is redacted whole', () => {
  const { got, pieces } = run(['Authorization: Bearer ab', 'cd1234\nnext\n'], [true, true]);
  assert.equal(got, '[redacted]\nnext\n');
  assert.ok(pieces.every((p) => !p.includes('Bearer')), 'no piece carries the front half');
});

test('probe: a private key split by a tick leaks nothing, and an unbroken run with a partial ghp_ at a tick is not emitted', () => {
  const m = manual();
  const out: string[] = [];
  const co = new Coalescer((t) => out.push(t), m.timers);
  co.write('-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEF\n');
  m.fire();
  assert.deepEqual(out, [], 'an open key block is held, not shipped');
  co.write('AASCBKcwggSjAgEAAoIBAQC\n-----END PRIVATE KEY-----\nok\n');
  m.fire();
  co.end();
  assert.equal(out.join(''), '[redacted]\nok\n');
  const out2: string[] = [];
  const m2 = manual();
  const c2 = new Coalescer((t) => out2.push(t), m2.timers);
  c2.write(`${'z'.repeat(400)} ghp_${'A'.repeat(20)}`);
  m2.fire();
  assert.deepEqual(out2, [], 'a long unfinished line is held whole: no raw prefix');
  c2.write(`${'A'.repeat(16)}\n`);
  c2.end();
  assert.equal(out2.join(''), redactString(`${'z'.repeat(400)} ghp_${'A'.repeat(36)}\n`).text);
});

// -------------------------------------------------------- caps fail closed

test('a line over the cap becomes a marker, the rest of it is dropped, and memory stays bounded', () => {
  const huge = 'y'.repeat(MAX_HELD_LINE * 6);
  const chunks = Array.from({ length: 6 }, (_, i) => huge.slice(i * MAX_HELD_LINE, (i + 1) * MAX_HELD_LINE));
  const { got, peak } = run([...chunks, '\nnext line\n']);
  assert.equal(got, '[redacted: line too long]\nnext line\n');
  assert.ok(peak <= 2 * MAX_HELD_LINE + 64, `held ${peak}`);
});

test('the discard after a cap is itself bounded: a line longer than the budget resumes after it', () => {
  const total = MAX_HELD_LINE + MAX_DISCARD + 5000;
  const { got } = run([`${'y'.repeat(total)}\n`.slice(0, MAX_HELD_LINE + 10), 'y'.repeat(MAX_DISCARD + 100), 'tail\n']);
  assert.match(got, /^\[redacted: line too long\]\n/);
  assert.ok(got.length < 200, 'what is lost is the capped line and one budget, not the stream');
});

test('a private-key block over the cap is replaced by a marker and dropped through its END line', () => {
  const body = ('A'.repeat(64) + '\n').repeat(Math.ceil(MAX_HELD_BLOCK / 65) + 20);
  const { got, peak } = run(['before\n-----BEGIN PRIVATE KEY-----\n', body.slice(0, 9000), body.slice(9000), '-----END PRIVATE KEY-----\nafter\n']);
  assert.equal(got, 'before\n[redacted: private key block too long]\nafter\n');
  assert.ok(peak <= MAX_HELD_BLOCK + 2 * 9000 + 128, `held ${peak}`);
});

test('the end of the stream redacts an unterminated key block whole', () => {
  const { got } = run(['x\n-----BEGIN PRIVATE KEY-----\nMIIEvQIBADAN\n']);
  assert.equal(got, 'x\n[redacted]');
});

test('pieces are at most 8 KiB of UTF-8 and never split a character', () => {
  const lines = ('é😀'.repeat(40) + '\n').repeat(400);
  const { got, pieces } = run([lines]);
  assert.equal(got, lines);
  for (const p of pieces) {
    assert.ok(new TextEncoder().encode(p).length <= 8192);
    assert.ok(!/[\ud800-\udbff]$/.test(p));
  }
});
