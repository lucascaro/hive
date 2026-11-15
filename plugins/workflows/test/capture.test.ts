// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Check output (spec 529, B-20, B-21): captured while the check runs, redacted
// ONCE as whole text when it ends, head plus tail, nothing for a cancel.

import assert from 'node:assert/strict';
import { mkdtempSync, realpathSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { Capture, HEAD_BYTES, PIECE_BYTES, pieces, TAIL_BYTES } from '../engine/check.ts';
import { checkExecutor } from '../engine/nodes.ts';
import { redactString } from '../engine/redact.ts';
import { encodedSize } from '../engine/size.ts';
import { workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { FakeHive, harness, rowOf, runState, waitFor } from './harness.ts';

const enc = new TextEncoder();
const bytes = (t: string): number => enc.encode(t).length;

function capture(chunks: string[]): ReturnType<Capture['finish']> & { peak: number } {
  const c = new Capture();
  let peak = 0;
  for (const ch of chunks) {
    c.write(ch);
    peak = Math.max(peak, c.held);
  }
  return { ...c.finish(), peak };
}

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

const split = (text: string, r: () => number, k: number): string[] => {
  const cuts = [...new Set(Array.from({ length: k }, () => Math.floor(r() * text.length)))].sort((a, b) => a - b);
  const out: string[] = [];
  let prev = 0;
  for (const c of [...cuts, text.length]) {
    out.push(text.slice(prev, c));
    prev = c;
  }
  return out;
};

const line = (i: number): string => `line ${String(i).padStart(6, '0')} ${'.'.repeat(60)}\n`;
const lines = (n: number, from = 0): string => Array.from({ length: n }, (_, i) => line(from + i)).join('');
const TOKEN = `ghp_${'A'.repeat(36)}`;

// ----------------------------------------------- redaction does not depend on chunking

test('a secret spanning arbitrary chunk boundaries is redacted exactly as whole-text redaction would', () => {
  const r = prng(20_261_114);
  for (let t = 0; t < 300; t++) {
    const body = lines(1 + Math.floor(r() * 8));
    const text = `${body}key ${TOKEN} and Authorization: Bearer abcdef0123456789abcdef\n${lines(2, 100)}-----BEGIN PRIVATE KEY-----\nSECRETBODY\n-----END PRIVATE KEY-----\ntail\n`;
    const got = capture(split(text, r, 1 + Math.floor(r() * 12)));
    assert.equal(got.text, redactString(text).text);
    assert.equal(got.truncated, false);
    assert.equal(got.total_bytes, bytes(text));
    assert.ok(!got.text.includes('AAAAAAAAAA') && !got.text.includes('SECRETBODY'));
  }
});

test('the result of a long output is the same however it is chunked, and no secret survives', () => {
  const r = prng(77);
  const text = `${lines(400)}token ${TOKEN}\n${lines(3000, 1000)}Authorization: token ${'0123456789abcdef'.repeat(2)}\n${lines(5)}`;
  const base = capture([text]);
  for (let t = 0; t < 40; t++) {
    const again = capture(split(text, r, 2 + Math.floor(r() * 30)));
    assert.deepEqual({ ...again, peak: 0 }, { ...base, peak: 0 }, `chunking ${t}`);
  }
  assert.ok(!base.text.includes('0123456789abcdef') && !base.text.includes('AAAAAAAAAA'));
  assert.equal(base.truncated, true);
});

// ------------------------------------------------------------------------- the head

test('the head is cut back to a line start before redacting, so a half token cannot be shown', () => {
  // The head limit falls inside a token on a line of its own.
  const filler = lines(Math.floor((HEAD_BYTES - 200) / bytes(line(0))));
  const pad = 'p'.repeat(HEAD_BYTES - bytes(filler) - 20);
  const text = `${filler}${pad} ${TOKEN}\n${lines(5000, 5000)}`;
  const got = capture([text]);
  const head = got.text.slice(0, got.text.indexOf('[... '));
  assert.ok(head.endsWith('\n'), 'the head ends at a line end');
  assert.ok(!head.includes('ghp_') && !head.includes('AAAA'), 'no part of the half-cut token is shown');
  assert.equal(got.truncated, true);
});

test('a head with no newline is replaced by a marker, never shown; its line is whole in the tail or a marker', () => {
  const fits = capture([`${'x'.repeat(HEAD_BYTES + 100)}\n${lines(5)}`]);
  assert.match(fits.text, /^\[\.\.\. \d+ bytes omitted \.\.\.\]\n/, 'no head text, only the marker');
  assert.equal(fits.truncated, true);
  assert.ok(fits.text.includes('x'.repeat(HEAD_BYTES + 100)), 'a line that fits the tail is shown whole, from its start');
  const long = capture([`${'x'.repeat(HEAD_BYTES + TAIL_BYTES + 100)}\n${lines(5)}`]);
  assert.match(long.text, /^\[\.\.\. \d+ bytes omitted \.\.\.\]\n/);
  assert.ok(!long.text.includes('xxxx'), 'a line over the whole window is not shown at all');
});

test('a key block opened in the head is redacted to the end of the head', () => {
  const text = `${lines(3)}-----BEGIN PRIVATE KEY-----\n${'KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK\n'.repeat(3000)}-----END PRIVATE KEY-----\n${lines(5, 9000)}`;
  const got = capture([text]);
  assert.ok(!got.text.includes('KKKK'), 'no body in the head or the tail');
  assert.match(got.text, /line 009004/, 'the lines after the block are shown');
});

// ------------------------------------------------------------------------- the tail

test('the tail starts at a line start, and its lines are whole', () => {
  const got = capture([lines(5000)]);
  const tail = got.text.slice(got.text.indexOf('bytes omitted ...]\n') + 'bytes omitted ...]\n'.length);
  for (const l of tail.split('\n').filter(Boolean)) assert.match(l, /^line \d{6} \.{60}$/);
  assert.ok(tail.endsWith(line(4999)));
});

test('a key block opened in the dropped middle never shows its body in the tail: END inside the tail', () => {
  const body = 'BODYBODYBODYBODYBODYBODYBODYBODY\n'.repeat(3000); // far more than the tail holds
  const text = `${lines(300)}-----BEGIN RSA PRIVATE KEY-----\n${body}-----END RSA PRIVATE KEY-----\n${lines(5, 8000)}`;
  const got = capture([text]);
  assert.ok(!got.text.includes('BODYBODY'), 'the body is not shown');
  assert.ok(got.text.includes('line 008004'), 'what follows the block is');
});

test('a key block opened in the dropped middle with no END in the tail: the whole tail is dropped', () => {
  const text = `${lines(300)}-----BEGIN RSA PRIVATE KEY-----\n${'BODYBODYBODYBODYBODYBODYBODYBODY\n'.repeat(5000)}`;
  const got = capture([text]);
  assert.ok(!got.text.includes('BODYBODY'));
  assert.match(got.text, /bytes omitted/);
});

test('one line longer than the whole tail becomes a marker, and memory stays bounded', () => {
  const chunks = [lines(10), ...Array.from({ length: 200 }, () => 'z'.repeat(32_768)), `\n${lines(3, 500)}`];
  const got = capture(chunks);
  assert.match(got.text, /a line longer than the capture window omitted/);
  assert.ok(!got.text.includes('zzzz'));
  assert.ok(got.text.includes('line 000502'), 'the lines after it are kept');
  assert.ok(got.peak <= HEAD_BYTES + 2 * TAIL_BYTES + 4096, `held ${got.peak}`);
});

test('memory stays bounded however much is written', () => {
  const c = new Capture();
  let peak = 0;
  for (let i = 0; i < 400; i++) {
    c.write(lines(100, i * 100));
    peak = Math.max(peak, c.held);
  }
  assert.ok(peak <= HEAD_BYTES + 2 * TAIL_BYTES + 8192, `held ${peak}`);
  const r = c.finish();
  assert.equal(r.total_bytes, 400 * bytes(lines(100)));
  assert.ok(bytes(r.text) <= HEAD_BYTES + TAIL_BYTES + 128);
});

test('total_bytes counts everything, truncated says whether anything was dropped, and a small output is whole', () => {
  const small = capture(['hello\n', 'world\n']);
  assert.deepEqual({ text: small.text, truncated: small.truncated, total_bytes: small.total_bytes }, { text: 'hello\nworld\n', truncated: false, total_bytes: 12 });
  const exactlyHead = capture([lines(1).repeat(0) + 'a'.repeat(HEAD_BYTES)]);
  assert.equal(exactlyHead.truncated, false);
  const mid = capture([lines(500)]); // 41 KiB: head and tail are contiguous, so it is whole
  assert.equal(mid.truncated, false);
  assert.equal(mid.text, lines(500));
  const big = capture([lines(5000)]);
  assert.equal(big.truncated, true);
  assert.equal(big.total_bytes, bytes(lines(5000)));
  const m = /\[\.\.\. (\d+) bytes omitted \.\.\.\]/.exec(big.text);
  assert.ok(m);
  const shown = bytes(big.text) - bytes(m[0] + '\n');
  assert.equal(Number(m[1]) + shown, big.total_bytes, 'shown + omitted = total');
});

test('pieces are at most 8 KiB of UTF-8 and 14 KiB as the daemon re-encodes them, and never split a character', () => {
  for (const text of [lines(400), '<'.repeat(30_000), ('é😀'.repeat(50) + '\n').repeat(300)]) {
    const ps = pieces(text);
    assert.equal(ps.join(''), text);
    for (const p of ps) {
      assert.ok(bytes(p) <= PIECE_BYTES);
      assert.ok(encodedSize(p) <= 14 << 10);
      assert.ok(!/[\ud800-\udbff]$/.test(p));
    }
  }
});

// ----------------------------------------------- the engine: when output is recorded

const never = (): Promise<void> => new Promise(() => {});
const oneCheck = (script: string, timeoutSec = 60) => workflow('c', s.object({}), (g) => void g.check('c', { argv: [process.execPath, '-e', script], timeoutSec }));

function prepared(dir: string): FakeHive {
  const hive = new FakeHive();
  hive.replies.PREPARE_WORKTREE = (req) => ({ path: dir, branch: req.branch, base_sha: 'sha-1', head_sha: 'sha-1', reattached: false });
  return hive;
}

const outputs = (h: ReturnType<typeof harness>, id: string) => h.store.events(id, 0, 1000).filter((e) => e.type === 'check.output');

test('check.output is recorded when the check ends, before node.finished, carrying total_bytes', async () => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), 'wf-cap-')));
  const h = harness({ agent: never, check: checkExecutor() }, { hive: prepared(dir) });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck("console.log('hello'); console.error('warn')"), input: {} });
    await waitFor(() => runState(h, id) === 'done', 'the run');
    const evs = h.store.events(id, 0, 1000);
    const out = evs.filter((e) => e.type === 'check.output');
    assert.deepEqual(out.map((e) => [e.data.stream, e.data.text, e.data.total_bytes]).sort(), [['stderr', 'warn\n', 5], ['stdout', 'hello\n', 6]]);
    const finished = evs.find((e) => e.type === 'node.finished' && e.key === 'c');
    for (const o of out) assert.ok(o.seq < (finished?.seq ?? 0), 'output precedes node.finished');
  } finally {
    h.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a check that times out still records its output, before node.finished', { skip: process.platform === 'win32' ? 'POSIX signals' : false }, async () => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), 'wf-cap-')));
  const h = harness({ agent: never, check: checkExecutor() }, { hive: prepared(dir) });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck("console.log('before the timeout'); setInterval(()=>{},1000)", 0.4), input: {} });
    await waitFor(() => runState(h, id) === 'failed', 'the run to fail');
    const evs = h.store.events(id, 0, 1000);
    const out = outputs(h, id);
    assert.equal(out.map((e) => e.data.text).join(''), 'before the timeout\n');
    const finished = evs.find((e) => e.type === 'node.finished' && e.key === 'c');
    assert.equal(finished?.data.state, 'failed');
    assert.ok(out[0].seq < (finished?.seq ?? 0));
  } finally {
    h.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('a cancelled check records no output (B-21)', { skip: process.platform === 'win32' ? 'POSIX signals' : false }, async () => {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), 'wf-cap-')));
  const h = harness({ agent: never, check: checkExecutor() }, { hive: prepared(dir) });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'c.ts', ir: oneCheck("console.log('about to be cancelled'); setInterval(()=>{},1000)"), input: {} });
    await waitFor(() => rowOf(h, id, 'c')?.state === 'running', 'the check to run');
    await new Promise((r) => setTimeout(r, 300)); // it has printed
    h.engine.cancelRun(id);
    await waitFor(() => runState(h, id) === 'cancelled', 'the cancel');
    await new Promise((r) => setTimeout(r, 300));
    h.engine.flush();
    assert.deepEqual(outputs(h, id), []);
  } finally {
    h.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

// ----------------------------------- redact.ts: the whole-text patterns, not line-local

test('the patterns are whole-text again: whitespace may include a newline', () => {
  for (const t of [
    'Bearer\nabcdefghij0123456789',
    '"token":\n  "abcdefghij12345"',
    'token\n= "abcdefghij12345"',
    'Authorization\n: abcdefghijklmnop',
    'Bearer abcdefghij0123456789',
    'secret = "abcdefghij12345"',
    'token\r=\r"abcdefghij12345"',
    'Bearer\r\nabcdefghij0123456789',
  ]) {
    const r = redactString(t);
    assert.ok(r.count >= 1 && !/abcdefghij/.test(r.text), `${JSON.stringify(t)} -> ${JSON.stringify(r.text)}`);
  }
  assert.ok(redactString('https://user:hunter2@example.com/x').count >= 1, 'URL credentials stay covered');
});

// ----------------------------------------------- key-block state is tracked in stream order (round 6)

const BEGIN = '-----BEGIN RSA PRIVATE KEY-----';
const END = '-----END RSA PRIVATE KEY-----';
const BODY = 'MIIEpAIBAAKCAQEAsecretbodyZZZ';
const FILLER = `${'x'.repeat(79)}\n`.repeat(250); // 20 KB: spills the head

test('a block opened by an overlong line is hidden even after an earlier, closed block (leak A)', () => {
  const got = capture([
    FILLER,
    `${BEGIN}\nold\n${END}\n`,
    `${'y'.repeat(60_000)}${BEGIN}\n`,
    `${BODY}\n${END}\nbye\n`,
  ]);
  assert.ok(!got.text.includes(BODY));
  assert.ok(got.text.includes('bye'));
});

test('evicting an earlier block does not reopen the tail after an overlong BEGIN line (leak B)', () => {
  const got = capture([
    FILLER,
    `${BEGIN}\n${END}\n`,
    `${'y'.repeat(60_000)}${BEGIN}\n`,
    `${BODY}\n`,
    `${'z'.repeat(79)}\n`.repeat(10),
  ]);
  assert.ok(!got.text.includes(BODY));
});

test('no body line of an unclosed or straddling key block is shown without its BEGIN (seeded, any chunking)', () => {
  const r = prng(61_114);
  for (let t = 0; t < 400; t++) {
    let text = '';
    let n = 0;
    const parts = 3 + Math.floor(r() * 10);
    for (let p = 0; p < parts; p++) {
      const k = r();
      if (k < 0.3) text += lines(Math.floor(r() * 400));
      else if (k < 0.6) {
        const body = Array.from({ length: 1 + Math.floor(r() * 3) }, () => `SECRETBODY${n++}\n`).join('');
        text += `${BEGIN}\n${body}${r() < 0.5 ? `${END}\n` : ''}`;
      } else if (k < 0.85) {
        const opens = r() < 0.5;
        const tail = opens ? `${BEGIN}\n` : r() < 0.5 ? `${END}\n` : '\n';
        // An overlong line that opens a block: its body follows, and may or may not be closed.
        const rest = opens ? `SECRETBODY${n++}\n${r() < 0.5 ? `${END}\n` : ''}` : '';
        text += `${'y'.repeat(TAIL_BYTES + 1 + Math.floor(r() * 4000))}${tail}${rest}`;
      } else text += lines(3);
    }
    const got = capture(split(text, r, Math.floor(r() * 20))).text;
    // Complete blocks are redacted whole. A body token that survives must sit under a BEGIN the reader can see.
    for (const m of got.matchAll(/SECRETBODY\d+/g)) {
      const before = got.slice(0, m.index);
      const lastBegin = before.lastIndexOf(BEGIN);
      // The omission stand-in hides a BEGIN: it is a barrier, not a visible opener.
      const barrier = Math.max(before.lastIndexOf(END), before.lastIndexOf('a line longer than the capture window'));
      assert.ok(lastBegin >= 0 && barrier < lastBegin, `${m[0]} shown without its BEGIN (trial ${t})`);
    }
  }
});

// ----------------------------------------------- split and long markers, and a stream-order oracle (round 7)

const longBegin = (n: number): string => `-----BEGIN ${'A'.repeat(n)} PRIVATE KEY-----`;

// A label of 38 makes a 66-char marker, 39 a 67-char one: both longer than the old 64-char carry.
for (const [label, cut, n] of [
  ['66-char marker', 65, 38],
  ['67-char marker', 66, 39],
  ['marker split inside its dashes', 3, 40],
] as const) {
  test(`a BEGIN marker split by a write inside an overlong line hides its body (${label})`, () => {
    const b = longBegin(n);
    const got = capture([
      FILLER,
      `${'y'.repeat(60_000)}${b.slice(0, cut)}`,
      `${b.slice(cut)}\n`,
      `${BODY}\n${END}\nbye\n`,
    ]);
    assert.ok(!got.text.includes(BODY));
    assert.ok(got.text.includes('bye'));
  });
}

test('a 300-char BEGIN label split by a write fails closed: open until a later END', () => {
  const b = longBegin(300);
  const got = capture([FILLER, `${'y'.repeat(60_000)}${b.slice(0, 120)}`, `${b.slice(120)}\n`, `${BODY}\n${END}\nbye\n`]);
  assert.ok(!got.text.includes(BODY));
  assert.ok(got.text.includes('bye'));
});

test('the same long marker, unsplit, hides its body (control)', () => {
  for (const n of [38, 39, 40, 300]) {
    const got = capture([FILLER, `${'y'.repeat(60_000)}${longBegin(n)}\n`, `${BODY}\n${END}\nbye\n`]);
    assert.ok(!got.text.includes(BODY), `label ${n}`);
  }
});

test('stream-order oracle: no key body is ever shown, for any filler, label, line length or chunking', () => {
  const r = prng(77_114);
  const ri = (n: number): number => Math.floor(r() * n);
  const types = ['RSA ', 'EC ', 'OPENSSH ', '', 'ENCRYPTED ', `${'L'.repeat(40)} `, `${'M'.repeat(300)} `];
  const eol = (): string => ['\n', '\r\n', '\n'][ri(3)] as string;
  for (let t = 0; t < 60; t++) {
    let text = '';
    let open = false;
    let n = 0;
    const bodies: string[] = [];
    const target = HEAD_BYTES + TAIL_BYTES + ri(80_000) - 30_000;
    while (text.length < target) {
      const k = ri(20);
      const ty = types[ri(types.length)] as string;
      const B = `-----BEGIN ${ty}PRIVATE KEY-----`;
      const E = `-----END ${ty}PRIVATE KEY-----`;
      if (k < 2) {
        text += B + eol();
        open = true;
      } else if (k < 4) {
        text += E + eol();
        open = false;
      } else if (k < 5) {
        let L = 'y'.repeat(ri(30_000) + 10_000);
        const m = ri(5);
        if (m === 0) {
          L += B;
          open = true;
        } else if (m === 1) {
          L += E;
          open = false;
        } else if (m === 2) {
          L += B + 'q'.repeat(100) + E;
          open = false;
        } else {
          L += E + 'w'.repeat(50) + B;
          open = true;
        }
        text += `${L}${'y'.repeat(ri(30_000) + 10_000)}${eol()}`;
      } else if (k < 6) {
        text += `${B}${eol()}zz${eol()}${E}${eol()}`;
        open = false;
      } else if (open) {
        // Tens of lines per body, so that eviction lands mid-block.
        for (let i = 0, c = 5 + ri(40); i < c; i++) {
          const b = `SECRET${n++}X`;
          bodies.push(b);
          text += b + 'a'.repeat(ri(60)) + eol();
        }
      } else text += 'f'.repeat(ri(200)) + eol();
    }
    const chunks: string[] = [];
    for (let i = 0; i < text.length; ) {
      const k = 1 + ri(r() < 0.3 ? 50 : 70_000);
      chunks.push(text.slice(i, i + k));
      i += k;
    }
    const got = capture(chunks).text;
    for (const b of bodies) assert.ok(!got.includes(b), `${b} shown (trial ${t})`);
  }
});
