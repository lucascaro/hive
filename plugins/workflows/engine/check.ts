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

const FLUSH_MS = 250;
const FLUSH_BYTES = 8 << 10;

const utf8Bytes = (s: string): number => new TextEncoder().encode(s).length;

export interface Timers {
  set(fn: () => void, ms: number): unknown;
  clear(t: unknown): void;
}

/** An unfinished line is held (raw) up to this many characters; past it the line is dropped, fail closed. */
export const MAX_HELD_LINE = 8 << 10;
/** An open `-----BEGIN ... PRIVATE KEY-----` block is held up to this many characters. */
export const MAX_HELD_BLOCK = 16 << 10;
/** After a cap, at most this many more characters are discarded looking for the end of the line or block. */
export const MAX_DISCARD = 64 << 10;

const PEM_BEGIN = /-----BEGIN [A-Z ]*PRIVATE KEY-----/g;
const PEM_END = /-----END [A-Z ]*PRIVATE KEY-----/;

/** Where the first private-key block with no END after it begins, or -1. */
function openBlock(s: string): number {
  PEM_BEGIN.lastIndex = 0;
  for (let m = PEM_BEGIN.exec(s); m; m = PEM_BEGIN.exec(s)) {
    if (!PEM_END.test(s.slice(m.index + m[0].length))) return m.index;
  }
  return -1;
}

/**
 * Cuts a stream into `check.output` pieces (at most 8 KiB of UTF-8, flushed 250
 * ms after output is ready) and redacts it on the way.
 *
 * The redaction patterns are line and block oriented, so this layer is too:
 * it redacts only COMPLETE units, a whole line or a whole private-key block,
 * and keeps the unfinished tail RAW until its newline, its END line, the end
 * of the stream, or a cap arrives. It never stores a redacted remainder (a
 * partly redacted line has lost the context the patterns need) and the timer
 * never emits a partial line. So whatever the chunk boundaries and ticks, the
 * output equals redacting the whole text at once.
 *
 * Caps fail closed. A line longer than MAX_HELD_LINE, or a key block longer
 * than MAX_HELD_BLOCK, is replaced by a `[redacted: ...]` marker and the rest of
 * that line or block is discarded, up to MAX_DISCARD further characters, so a
 * raw prefix or suffix of something that may be a secret is never emitted and
 * memory stays bounded. The stream end, an exit, a cancel and a timeout all
 * end the stream: the held tail is then redacted whole and emitted.
 */
export class Coalescer {
  #raw = '';
  #out = '';
  #timer: unknown;
  /** Discarding until a newline (line) or an END line (block), with this many characters of budget left. */
  #skip: { kind: 'line' | 'block'; left: number } | undefined;
  readonly #emit: (text: string) => void;
  readonly #timers: Timers;

  constructor(emit: (text: string) => void, timers: Timers = { set: (fn, ms) => setTimeout(fn, ms), clear: (t) => clearTimeout(t as ReturnType<typeof setTimeout>) }) {
    this.#emit = emit;
    this.#timers = timers;
  }

  /** Characters held raw, for tests that bound memory. */
  get held(): number {
    return this.#raw.length + this.#out.length;
  }

  write(text: string): void {
    this.#raw += text;
    this.#drain(false);
    this.#ship(false);
  }

  /** The timer fired: ship what is ready. Held (unfinished) text is never shipped here. */
  tick(): void {
    this.#timer = undefined;
    this.#ship(true);
  }

  /** The stream ended: nothing more can complete a unit, so redact the held tail whole and emit everything. */
  end(): void {
    if (this.#timer !== undefined) this.#timers.clear(this.#timer);
    this.#timer = undefined;
    this.#drain(true);
    this.#ship(true);
  }

  #drain(final: boolean): void {
    for (;;) {
      if (this.#skip) {
        // Dropping the rest of an oversize line or block, looking for where it ends.
        const re = this.#skip.kind === 'line' ? /\n/ : /-----END [A-Z ]*PRIVATE KEY-----[^\n]*\n?/;
        const m = re.exec(this.#raw);
        if (m) {
          this.#raw = this.#raw.slice(m.index + m[0].length);
          this.#skip = undefined;
          continue;
        }
        this.#skip.left -= this.#raw.length;
        this.#raw = '';
        if (this.#skip.left <= 0) this.#skip = undefined; // the discard budget is spent: resume with whatever comes next
        return;
      }
      if (this.#raw === '') return;
      let cut = final ? this.#raw.length : this.#raw.lastIndexOf('\n') + 1;
      if (cut > 0) {
        const open = final ? -1 : openBlock(this.#raw.slice(0, cut));
        // Hold from the start of the line the block opens on: the patterns see that line whole (`Authorization: x -----BEGIN ...`).
        if (open >= 0) cut = this.#raw.lastIndexOf('\n', open - 1) + 1;
      }
      if (cut > 0) {
        this.#out += redactString(this.#raw.slice(0, cut)).text;
        this.#raw = this.#raw.slice(cut);
        continue;
      }
      // Nothing complete: what is held is an unfinished line, or an open private-key block.
      const open = openBlock(this.#raw);
      const block = open >= 0;
      if (this.#raw.length > (block ? MAX_HELD_BLOCK : MAX_HELD_LINE)) {
        this.#out += block ? '[redacted: private key block too long]\n' : '[redacted: line too long]\n';
        this.#skip = { kind: block ? 'block' : 'line', left: MAX_DISCARD };
        continue;
      }
      return;
    }
  }

  /** Emit ready text in pieces of at most 8 KiB, now when asked and otherwise once 8 KiB has built up. */
  #ship(all: boolean): void {
    while (this.#out !== '' && (all || utf8Bytes(this.#out) >= FLUSH_BYTES)) {
      const cut = prefixWithin(this.#out, FLUSH_BYTES);
      this.#emit(this.#out.slice(0, cut));
      this.#out = this.#out.slice(cut);
    }
    if (this.#out !== '' && this.#timer === undefined) this.#timer = this.#timers.set(() => this.tick(), FLUSH_MS);
  }
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

/**
 * Starts the command in its own process group and reports its output in
 * `check.output`-sized pieces: per stream, flushed every 250 ms or 8 KiB.
 */
export function launch(spec: CheckSpec, onOutput: (stream: 'stdout' | 'stderr', text: string) => void, deps: CheckDeps = {}): CheckHandle {
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
    const co = new Coalescer((text) => onOutput(stream, text));
    child[stream]?.on('data', (chunk: Uint8Array) => co.write(dec.write(chunk)));
    return () => {
      co.write(dec.end());
      co.end();
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
