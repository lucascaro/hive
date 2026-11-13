// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// A check node (spec 529, Check and human nodes): a command from the IR run in
// a worktree, its verdict the exit code. Never a shell: argv is passed as
// written, and nothing an earlier node produced reaches it.

import { spawn } from 'node:child_process';
import { StringDecoder } from 'node:string_decoder';
import { type KillDeps, killTree, procStart } from './killtree.ts';
import { redactString } from './redact.ts';

const FLUSH_MS = 250;
const FLUSH_BYTES = 8 << 10;
/** A trailing run this short is held back at a timed flush, in case it is the start of a token. */
const HOLD_CHARS = 256;

const utf8Bytes = (s: string): number => new TextEncoder().encode(s).length;

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
  stop(): Promise<void>;
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
  let tree: Promise<void> | undefined;
  let settle: (r: CheckResult) => void = () => {};
  const result = new Promise<CheckResult>((r) => {
    settle = r;
  });

  const pipes = (['stdout', 'stderr'] as const).map((stream) => {
    const dec = new StringDecoder('utf8');
    let buf = '';
    let timer: ReturnType<typeof setTimeout> | undefined;
    // Redaction runs on everything in hand before it is cut, so a secret that
    // straddles a cut is not stored in two harmless-looking halves.
    const emit = (text: string): void => onOutput(stream, redactString(text).text);
    const flush = (all: boolean): void => {
      if (timer) clearTimeout(timer);
      timer = undefined;
      let out = buf;
      if (!all) {
        // Hold back a trailing unbroken run (a token still arriving) until more output or the end.
        const ws = Math.max(buf.lastIndexOf('\n'), buf.lastIndexOf(' '), buf.lastIndexOf('\t'));
        if (ws >= 0 && buf.length - ws <= HOLD_CHARS) out = buf.slice(0, ws + 1);
      }
      buf = buf.slice(out.length);
      if (out !== '') emit(out);
      if (buf !== '') timer = setTimeout(() => flush(true), FLUSH_MS);
    };
    child[stream]?.on('data', (chunk: Uint8Array) => {
      buf += dec.write(chunk);
      while (utf8Bytes(buf) >= FLUSH_BYTES) {
        const red = redactString(buf).text;
        let cut = prefixWithin(red, FLUSH_BYTES);
        // Do not cut through what may be the start of a token: back up to the whitespace before it, when that is near.
        const ws = Math.max(red.lastIndexOf('\n', cut - 1), red.lastIndexOf(' ', cut - 1), red.lastIndexOf('\t', cut - 1));
        if (ws > 0 && cut - ws <= HOLD_CHARS && cut < red.length + 1 && !/\s/.test(red[cut - 1])) cut = ws + 1;
        onOutput(stream, red.slice(0, cut));
        buf = red.slice(cut);
      }
      if (buf !== '' && !timer) timer = setTimeout(() => flush(false), FLUSH_MS);
    });
    return () => {
      buf += dec.end();
      flush(true);
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
      await tree;
    },
  };

  const timeout = setTimeout(() => {
    if (stopping) return; // a stop is already under way: a cancel stays a cancel
    stopping = 'timeout';
    void handle.stop();
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
