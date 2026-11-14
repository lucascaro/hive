// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// A check node (spec 529, Check and human nodes): a command from the IR run in
// a worktree, its verdict the exit code. Never a shell: argv is passed as
// written, and nothing an earlier node produced reaches it.

import { spawn } from 'node:child_process';
import { StringDecoder } from 'node:string_decoder';
import { type KillDeps, killTree, procStart, type StopResult } from './killtree.ts';
import { redactString } from './redact.ts';
import { encodedSize, truncateEncoded } from './size.ts';

/** What a check's output keeps of each stream: the first HEAD_BYTES and the last TAIL_BYTES (whole lines). */
export const HEAD_BYTES = 16 << 10;
export const TAIL_BYTES = 48 << 10;
/** An emitted `check.output` event carries at most this much text. */
export const PIECE_BYTES = 8 << 10;
/** And at most this much once Go has re-encoded it (`<` is six bytes on the wire). */
const PIECE_ENCODED = 14 << 10;

const utf8Bytes = (s: string): number => new TextEncoder().encode(s).length;
const KEY_MARKER = /-----(BEGIN|END) [A-Z ]*PRIVATE KEY-----/g;

interface Entry {
  text: string;
  bytes: number;
  /** Whether a private-key block was open when the line began, and when it ended, in stream order. */
  startsOpen: boolean;
  endsOpen: boolean;
  /** An overlong line that was dropped; `text` is its stand-in. */
  marker: boolean;
}

export interface Captured {
  /** Redacted, with a `[... N bytes omitted ...]` marker where the middle was dropped. */
  text: string;
  truncated: boolean;
  /** Every byte the stream produced, kept or not. */
  total_bytes: number;
}

/**
 * A bounded capture of one output stream (spec 529, B-20). Nothing is
 * redacted or emitted while the check runs. At the end the head and the tail
 * are each redacted ONCE, as whole text, so the result does not depend on how
 * the output arrived in chunks, and what survives of a stream longer than
 * HEAD_BYTES + TAIL_BYTES is its start and its end:
 *
 * - the head is cut back to its last newline before redacting (a token cut in
 *   half can fall under a pattern's minimum length and survive); a head with no
 *   newline at all is replaced by a marker;
 * - the tail is a ring of WHOLE lines, so it starts at a line start; a line
 *   longer than TAIL_BYTES is replaced by a marker (fail closed, memory bounded);
 * - lines that fall out of the ring are scanned only for private-key BEGIN and
 *   END lines. If a block is open where the tail starts, the tail's lines up to
 *   and including its END line are dropped (or the whole tail, if it has no
 *   END), because the BEGIN that would make redaction recognise them is gone.
 *
 * Redaction is best-effort (B-5); this removes the boundary-dependent ways it
 * could fail, not the patterns' own gaps. `prepareEvent` redacts each emitted
 * piece again, which can only over-redact.
 */
export class Capture {
  #head = '';
  #headBytes = 0;
  /** The head filled: later text is the tail's. */
  #spilled = false;
  #headText = '';
  #headLost = false;
  #ring: Entry[] = [];
  #ringBytes = 0;
  #cur = '';
  #curBytes = 0;
  #overlong = false;
  #carry = '';
  /** Whether a private-key block is open after everything scanned so far. Every line is scanned exactly once, in stream order. */
  #state = false;
  /** The state when the current overlong line began. */
  #overStart = false;
  #dropped = 0;
  #lost = false;
  #total = 0;

  /** Bytes held in memory, for tests of the bound. */
  get held(): number {
    return this.#headBytes + this.#ringBytes + this.#curBytes + this.#carry.length;
  }

  write(text: string): void {
    if (text === '') return;
    this.#total += utf8Bytes(text);
    if (this.#spilled) return this.#tail(text);
    const room = HEAD_BYTES - this.#headBytes;
    const bytes = utf8Bytes(text);
    if (bytes <= room) {
      this.#head += text;
      this.#headBytes += bytes;
      return;
    }
    const fit = prefixWithin(text, room);
    this.#head += text.slice(0, fit);
    const nl = this.#head.lastIndexOf('\n');
    this.#spilled = true;
    if (nl < 0) {
      this.#headLost = true; // no line start to cut back to: the head is not shown
      this.#lost = true;
      this.#tail(this.#head + text.slice(fit));
    } else {
      this.#headText = this.#head.slice(0, nl + 1);
      this.#scan(this.#headText);
      this.#tail(this.#head.slice(nl + 1) + text.slice(fit));
    }
    this.#head = '';
    this.#headBytes = utf8Bytes(this.#headText);
  }

  /** The stream ended: what is kept, redacted whole. */
  finish(): Captured {
    if (!this.#spilled) return { text: redactString(this.#head).text, truncated: false, total_bytes: this.#total };
    if (this.#overlong) this.#pushMarker();
    else if (this.#curBytes > 0) this.#pushLine(this.#cur, this.#curBytes);
    this.#cur = '';
    this.#curBytes = 0;
    const contiguous = this.#dropped === 0 && !this.#lost && !this.#headLost;
    if (contiguous) {
      // Nothing was dropped between the head and the tail: it is the whole output.
      return { text: redactString(this.#headText + this.#ring.map((l) => l.text).join('')).text, truncated: false, total_bytes: this.#total };
    }
    // A key block whose BEGIN is not in the kept text (dropped off the front, or inside an omitted overlong line)
    // must not show its body: drop entries while such a block is open, in stream order, using the state each
    // entry was scanned with.
    let hidden = this.#ring[0]?.startsOpen ?? false;
    const lines: string[] = [];
    for (const e of this.#ring) {
      if (hidden) {
        hidden = e.endsOpen;
        continue;
      }
      lines.push(e.text);
      if (e.marker && e.endsOpen) hidden = true;
    }
    const tailRaw = lines.join('');
    const shown = (this.#headLost ? 0 : utf8Bytes(this.#headText)) + utf8Bytes(tailRaw);
    const head = this.#headLost ? '' : redactString(this.#headText).text;
    const text = `${head}[... ${Math.max(0, this.#total - shown)} bytes omitted ...]\n${redactString(tailRaw).text}`;
    return { text, truncated: true, total_bytes: this.#total };
  }

  /** Track whether a private-key block is open after `text`, by its last BEGIN or END marker. */
  #scan(text: string): void {
    KEY_MARKER.lastIndex = 0;
    for (let m = KEY_MARKER.exec(text); m; m = KEY_MARKER.exec(text)) this.#state = m[1] === 'BEGIN';
  }

  #tail(text: string): void {
    while (text !== '') {
      const nl = text.indexOf('\n');
      this.#appendCur(nl < 0 ? text : text.slice(0, nl + 1));
      if (nl < 0) return;
      this.#completeLine();
      text = text.slice(nl + 1);
    }
  }

  #appendCur(piece: string): void {
    const bytes = utf8Bytes(piece);
    if (this.#overlong) {
      this.#dropped += bytes;
      this.#scanChunk(piece);
      return;
    }
    this.#cur += piece;
    this.#curBytes += bytes;
    if (this.#curBytes > TAIL_BYTES) {
      // One line longer than the whole tail: fail closed. Its text is dropped; only its key markers are noted.
      this.#dropped += this.#curBytes;
      this.#overStart = this.#state;
      this.#scanChunk(this.#cur);
      this.#cur = '';
      this.#curBytes = 0;
      this.#overlong = true;
    }
  }

  /** Scan a dropped chunk for key markers, keeping a short carry so a marker split between chunks is seen. */
  #scanChunk(text: string): void {
    const both = this.#carry + text;
    this.#scan(both);
    this.#carry = both.slice(-64);
  }

  #completeLine(): void {
    if (this.#overlong) this.#pushMarker();
    else this.#pushLine(this.#cur, this.#curBytes);
    this.#cur = '';
    this.#curBytes = 0;
  }

  #pushMarker(): void {
    this.#overlong = false;
    this.#lost = true;
    this.#carry = '';
    // The omitted line's text was scanned as it streamed past: the marker inherits the state before and after it.
    this.#add({ text: '[... a line longer than the capture window omitted ...]\n', bytes: 64, startsOpen: this.#overStart, endsOpen: this.#state, marker: true });
  }

  #pushLine(text: string, bytes: number): void {
    const startsOpen = this.#state;
    this.#scan(text);
    this.#add({ text, bytes, startsOpen, endsOpen: this.#state, marker: false });
  }

  #add(e: Entry): void {
    this.#ring.push(e);
    this.#ringBytes += e.bytes;
    while (this.#ringBytes > TAIL_BYTES && this.#ring.length > 1) {
      const old = this.#ring.shift() as Entry;
      this.#ringBytes -= old.bytes;
      this.#dropped += old.bytes;
    }
  }
}

/** Cuts text into `check.output` pieces: at most PIECE_BYTES of UTF-8 and PIECE_ENCODED as the daemon re-encodes it. */
export function pieces(text: string): string[] {
  const out: string[] = [];
  while (text !== '') {
    let cut = prefixWithin(text, PIECE_BYTES);
    if (encodedSize(text.slice(0, cut)) > PIECE_ENCODED) cut = truncateEncoded(text.slice(0, cut), PIECE_ENCODED).text.length;
    out.push(text.slice(0, cut));
    text = text.slice(cut);
  }
  return out;
}

/** The longest prefix of `s`, in UTF-16 units, that is at most `max` UTF-8 bytes and does not split a surrogate pair. */
function prefixWithin(s: string, max: number): number {
  let lo = 0;
  let hi = s.length;
  const aligned = (n: number): number => {
    const c = s.charCodeAt(n - 1);
    return n > 0 && c >= 0xd800 && c <= 0xdbff ? n - 1 : n;
  };
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (utf8Bytes(s.slice(0, aligned(mid))) <= max) lo = mid;
    else hi = mid - 1;
  }
  return aligned(lo);
}

export type CheckResult =
  | { kind: 'exit'; ok: boolean; exit_code: number }
  | { kind: 'timeout' }
  | { kind: 'stopped' }
  | { kind: 'error'; message: string };

export interface CheckSpec {
  argv: readonly string[];
  cwd: string;
  timeout_s: number;
}

export interface CheckHandle {
  /** Undefined when the command could not be started; `result` then says why. */
  pid: number | undefined;
  /** The process group on POSIX. */
  pgid: number | undefined;
  proc_start: string | undefined;
  result: Promise<CheckResult>;
  /** Stops the tree, politely and then not. `result` settles as `stopped`. */
  stop(): Promise<'kept' | void>;
}

export interface CheckDeps extends KillDeps {
  env?: Record<string, string | undefined>;
}

/** The environment a check gets: the login PATH plus LANG, TZ and HOME. Builds and tests need HOME; checks are argv the user reviewed. */
export function checkEnv(from: Record<string, string | undefined> = process.env): Record<string, string> {
  const env: Record<string, string> = {};
  for (const k of ['PATH', 'LANG', 'TZ', 'HOME']) if (from[k] !== undefined) env[k] = from[k] as string;
  return env;
}

export interface OutputMeta {
  truncated?: true;
  total_bytes: number;
}

/**
 * Starts the command in its own process group. Its output is captured (head
 * and tail, see Capture) and reported ONCE when it ends, redacted whole, in
 * pieces of at most 8 KiB; nothing is reported while it runs, and nothing at
 * all for a check that was cancelled (B-21).
 */
export function launch(spec: CheckSpec, onOutput: (stream: 'stdout' | 'stderr', text: string, meta: OutputMeta) => void, deps: CheckDeps = {}): CheckHandle {
  const win = (deps.platform ?? process.platform) === 'win32';
  const child = spawn(spec.argv[0], spec.argv.slice(1), {
    cwd: spec.cwd,
    env: checkEnv(deps.env),
    windowsHide: true,
    detached: !win,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let stopping: 'timeout' | 'stopped' | undefined;
  let tree: Promise<StopResult> | undefined;
  let settle: (r: CheckResult) => void = () => {};
  const result = new Promise<CheckResult>((r) => {
    settle = r;
  });

  const pipes = (['stdout', 'stderr'] as const).map((stream) => {
    const dec = new StringDecoder('utf8');
    const cap = new Capture();
    child[stream]?.on('data', (chunk: Uint8Array) => cap.write(dec.write(chunk)));
    return () => {
      cap.write(dec.end());
      if (stopping === 'stopped') return; // cancelled: no output is recorded for it
      const c = cap.finish();
      for (const text of pieces(c.text)) onOutput(stream, text, { ...(c.truncated ? { truncated: true as const } : {}), total_bytes: c.total_bytes });
    };
  });

  const pid = child.pid;
  const handle: CheckHandle = {
    pid,
    pgid: pid !== undefined && !win ? pid : undefined,
    proc_start: pid !== undefined ? procStart(pid, deps) : undefined,
    result,
    stop: async () => {
      if (pid === undefined) return;
      stopping ??= 'stopped';
      tree ??= killTree({ pid, pgid: handle.pgid }, deps);
      const r = await tree;
      return r === 'kept' || r === 'refused' ? 'kept' : undefined;
    },
  };

  const timeout = setTimeout(() => {
    if (stopping) return; // a stop is already under way: a cancel stays a cancel
    stopping = 'timeout';
    // A stop the OS refuses (EPERM) leaves the process alive: the check settles as an error saying so.
    handle.stop().then(
      (r) => r === 'kept' && settle({ kind: 'error', message: `could not stop ${spec.argv[0]}: the operating system refused the signal (EPERM); it may still be running` }),
      (err: Error) => settle({ kind: 'error', message: `could not stop ${spec.argv[0]}: ${err.message}` }),
    );
  }, spec.timeout_s * 1000);
  child.on('error', (err: Error) => {
    clearTimeout(timeout);
    settle({ kind: 'error', message: `could not start ${spec.argv[0]}: ${err.message}` });
  });
  child.on('close', async (code: number | null) => {
    clearTimeout(timeout);
    for (const end of pipes) end();
    // The leader can exit on TERM while a grandchild with redirected stdio lives on: settle once the whole tree is stopped.
    if (tree) await tree.catch(() => {});
    if (stopping === 'timeout') settle({ kind: 'timeout' });
    else if (stopping === 'stopped') settle({ kind: 'stopped' });
    else settle({ kind: 'exit', ok: code === 0, exit_code: code ?? -1 });
  });
  return handle;
}
