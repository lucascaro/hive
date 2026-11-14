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
/** An open or still-resolving `-----BEGIN ... PRIVATE KEY-----` block is held up to this many characters. */
export const MAX_HELD_BLOCK = 16 << 10;

const PEM_BEGIN = /-----BEGIN [A-Z ]*PRIVATE KEY-----/g;
const PEM_END = /-----END [A-Z ]*PRIVATE KEY-----/;
/** What discarding keeps between chunks so an END marker split across two of them is still found. */
const END_TAIL = 64;

interface Span {
  start: number;
  /** One past the END marker, or Infinity for a block that has not closed yet. */
  end: number;
}

/** The private-key blocks in `s`, as redactString's PEM pattern would match them: each from a BEGIN to the first END after it, the next one searched after that. */
function blocks(s: string): Span[] {
  const out: Span[] = [];
  let from = 0;
  for (;;) {
    PEM_BEGIN.lastIndex = from;
    const m = PEM_BEGIN.exec(s);
    if (!m) return out;
    const at = m.index + m[0].length;
    const e = PEM_END.exec(s.slice(at));
    if (!e) {
      out.push({ start: m.index, end: Infinity });
      return out;
    }
    from = at + e.index + e[0].length;
    out.push({ start: m.index, end: from });
  }
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
 * than MAX_HELD_BLOCK, is replaced by a `[redacted: ...]` marker, and the rest
 * of that unit is DISCARDED to its real end (the newline, or the END line), or
 * the end of the stream. There is no budget after which output resumes: that
 * would resume mid-unit and emit a raw suffix. Discarding holds nothing, so
 * memory stays bounded however long the unit is; what is lost is the rest of one
 * oversize line or key block, and nothing after it. The stream end, an exit, a
 * cancel and a timeout all end the stream: the held tail is then redacted whole
 * and emitted.
 */
export class Coalescer {
  #raw = '';
  #out = '';
  #timer: unknown;
  /** Discarding to the end of an oversize line (a newline) or key block (its END line). */
  #skip: 'line' | 'block' | undefined;
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
        // Dropping the rest of an oversize unit, looking for where it really ends.
        const re = this.#skip === 'line' ? /\n/ : /-----END [A-Z ]*PRIVATE KEY-----[^\n]*\n?/;
        const m = re.exec(this.#raw);
        if (m) {
          this.#raw = this.#raw.slice(m.index + m[0].length);
          // An END marker with no newline after it yet: the rest of its line is still part of the unit.
          this.#skip = this.#skip === 'block' && !m[0].endsWith('\n') ? 'line' : undefined;
          continue;
        }
        // Keep a short tail so an END marker split across two chunks is still found; nothing else is held.
        this.#raw = this.#skip === 'block' ? this.#raw.slice(-END_TAIL) : '';
        return;
      }
      if (this.#raw === '') return;
      const lines = final ? this.#raw.length : this.#raw.lastIndexOf('\n') + 1;
      // Only whole units go out: complete lines, and whole private-key blocks. A block that is open, or closes in
      // the unfinished last line, holds from the start of the line it begins on; and that line may itself begin
      // inside an earlier block (`END ... BEGIN` on one line), so the cut keeps moving back until no block spans it.
      let cut = lines;
      const spans = final ? [] : blocks(this.#raw);
      for (let moved = true; moved && cut > 0; ) {
        moved = false;
        for (const sp of spans) {
          if (sp.start < cut && sp.end > cut) {
            cut = this.#raw.lastIndexOf('\n', sp.start - 1) + 1;
            moved = true;
          }
        }
      }
      if (cut > 0) {
        this.#out += redactString(this.#raw.slice(0, cut)).text;
        this.#raw = this.#raw.slice(cut);
        continue;
      }
      // Nothing can go out: what is held is an unfinished line, or a key block still being resolved.
      const block = lines > 0 || blocks(this.#raw).length > 0;
      if (this.#raw.length > (block ? MAX_HELD_BLOCK : MAX_HELD_LINE)) {
        this.#out += block ? '[redacted: private key block too long]\n' : '[redacted: line too long]\n';
        this.#skip = block ? 'block' : 'line';
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
