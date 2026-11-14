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
/** A trailing run this short is held back at a timed flush, in case it is the start of a token. */
const HOLD_CHARS = 256;

const utf8Bytes = (s: string): number => new TextEncoder().encode(s).length;

export interface Timers {
  set(fn: () => void, ms: number): unknown;
  clear(t: unknown): void;
}

/**
 * Cuts a stream into `check.output` pieces: at most 8 KiB of UTF-8, flushed
 * 250 ms after the first byte arrives. Redaction runs on everything in hand
 * before it is cut, and a trailing unbroken run of up to 256 characters (a
 * token that may still be arriving) is held back, by the timer and by the size
 * cut alike, until more output arrives or the stream ends. It is never flushed
 * on a timer alone: a token split across two timer ticks would match no pattern
 * in either half.
 */
export class Coalescer {
  #buf = '';
  #timer: unknown;
  readonly #emit: (text: string) => void;
  readonly #timers: Timers;

  constructor(emit: (text: string) => void, timers: Timers = { set: (fn, ms) => setTimeout(fn, ms), clear: (t) => clearTimeout(t as ReturnType<typeof setTimeout>) }) {
    this.#emit = (text) => emit(redactString(text).text);
    this.#timers = timers;
  }

  /**
   * Where the held-back tail begins: everything before it is safe to emit. The
   * tail is the trailing unbroken run, plus a credential keyword just before it
   * (`Bearer `, `Basic `, `Authorization: `, `"token": `), because those
   * patterns need the keyword and the value together.
   */
  static #safe(s: string, upTo: number): number {
    const ws = Math.max(s.lastIndexOf('\n', upTo - 1), s.lastIndexOf(' ', upTo - 1), s.lastIndexOf('\t', upTo - 1));
    let start = ws + 1;
    const key = /(?:\b(?:Bearer|Basic)|Authorization["']?\s*[:=]|["']?\b(?:auth_?token|access_?token|api_?key|secret|token)["']?\s*[:=])[ \t]*$/i.exec(s.slice(Math.max(0, start - 40), start));
    if (key) start = Math.max(0, start - 40) + key.index;
    return upTo - start <= HOLD_CHARS ? start : upTo;
  }

  write(text: string): void {
    this.#buf += text;
    while (utf8Bytes(this.#buf) >= FLUSH_BYTES) {
      const red = redactString(this.#buf).text;
      let cut = prefixWithin(red, FLUSH_BYTES);
      const safe = Coalescer.#safe(red, cut);
      if (safe > 0) cut = safe; // with no earlier break the run is cut as it is: it is longer than a token
      this.#emit(red.slice(0, cut));
      this.#buf = red.slice(cut);
    }
    if (this.#buf !== '' && this.#timer === undefined) this.#timer = this.#timers.set(() => this.tick(), FLUSH_MS);
  }

  /** The timer fired: emit what is safe, keep a trailing run that may be a token's front half. */
  tick(): void {
    this.#timer = undefined;
    const safe = Coalescer.#safe(this.#buf, this.#buf.length);
    if (safe > 0) {
      this.#emit(this.#buf.slice(0, safe));
      this.#buf = this.#buf.slice(safe);
    }
  }

  /** The stream ended: nothing more can complete a token, so emit it all. */
  end(): void {
    if (this.#timer !== undefined) this.#timers.clear(this.#timer);
    this.#timer = undefined;
    if (this.#buf !== '') this.#emit(this.#buf);
    this.#buf = '';
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
