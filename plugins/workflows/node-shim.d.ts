// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Minimal ambient declarations for the few Node APIs this plugin uses, so
// `tsc` can check it without an @types/node dependency (spec 495). Only what
// is imported here is declared; widen it when a new import needs it.

declare const process: {
  argv: string[];
  execPath: string;
  exitCode: number | undefined;
  platform: string;
  pid: number;
  env: Record<string, string | undefined>;
  stdout: { write(s: string): boolean };
  stderr: { write(s: string): boolean };
  on(event: string, fn: (...args: never[]) => void): void;
  exit(code?: number): never;
  kill(pid: number, signal?: string | number): true;
  cwd(): string;
};

declare const console: {
  log(...args: unknown[]): void;
  error(...args: unknown[]): void;
};

interface ImportMeta {
  url: string;
}

declare function setTimeout(fn: (...args: never[]) => void, ms?: number): { unref(): void; [Symbol.toPrimitive](): number };
declare function setImmediate(fn: (...args: never[]) => void): unknown;
declare function clearTimeout(t: ReturnType<typeof setTimeout> | undefined): void;
declare function setInterval(fn: (...args: never[]) => void, ms?: number): { unref?(): void };
declare function clearInterval(t: ReturnType<typeof setInterval> | undefined): void;

declare class URL {
  constructor(url: string, base?: string);
  href: string;
}

declare class TextEncoder {
  encode(s: string): Uint8Array;
}

declare class TextDecoder {
  decode(b?: Uint8Array): string;
}

declare module 'node:test' {
  export function test(name: string, fn: () => void | Promise<void>): void;
  export function test(name: string, opts: { skip?: boolean | string; timeout?: number }, fn: () => void | Promise<void>): void;
}

declare module 'node:assert/strict' {
  const assert: {
    (value: unknown, message?: string): asserts value;
    ok(value: unknown, message?: string): asserts value;
    equal(actual: unknown, expected: unknown, message?: string): void;
    notEqual(actual: unknown, expected: unknown, message?: string): void;
    deepEqual(actual: unknown, expected: unknown, message?: string): void;
    notDeepEqual(actual: unknown, expected: unknown, message?: string): void;
    match(value: string, re: RegExp, message?: string): void;
    doesNotMatch(value: string, re: RegExp, message?: string): void;
    throws(fn: () => unknown, expected?: RegExp, message?: string): void;
    rejects(p: Promise<unknown> | (() => Promise<unknown>), expected?: RegExp, message?: string): Promise<void>;
    fail(message?: string): never;
  };
  export default assert;
}

declare module 'node:fs' {
  export function readFileSync(path: string, encoding: 'utf8'): string;
  export function readdirSync(path: string): string[];
  export function writeFileSync(path: string, data: string, opts?: { mode?: number }): void;
  export function mkdtempSync(prefix: string): string;
  export function mkdirSync(path: string, opts?: { recursive?: boolean; mode?: number }): void;
  export function rmSync(path: string, opts: { recursive: boolean; force: boolean }): void;
  export function openSync(path: string, flags: string, mode?: number): number;
  export function closeSync(fd: number): void;
  export function chmodSync(path: string, mode: number): void;
  export function statSync(path: string): { mode: number };
  export function existsSync(path: string): boolean;
  export function realpathSync(path: string): string;
}

declare module 'node:os' {
  export function tmpdir(): string;
}

declare module 'node:path' {
  export function join(...parts: string[]): string;
  export function dirname(path: string): string;
}

declare module 'node:url' {
  export function fileURLToPath(url: string | { href: string }): string;
  export function pathToFileURL(path: string): { href: string };
}

declare module 'node:crypto' {
  export function randomBytes(n: number): Uint8Array & { toString(enc: 'hex'): string };
}

declare module 'node:string_decoder' {
  export class StringDecoder {
    constructor(enc: string);
    write(b: Uint8Array): string;
    end(): string;
  }
}

declare module 'node:child_process' {
  interface Readable {
    on(event: 'data', fn: (chunk: Uint8Array) => void): void;
  }
  export interface ChildProcess {
    pid: number | undefined;
    stdout: Readable | null;
    stderr: Readable | null;
    on(event: 'error', fn: (err: Error) => void): void;
    on(event: 'close', fn: (code: number | null, signal: string | null) => void): void;
    on(event: string, fn: (...args: never[]) => void): void;
    kill(signal?: string): boolean;
  }
  export interface SpawnOptions {
    cwd?: string;
    env?: Record<string, string | undefined>;
    stdio?: string | string[];
    detached?: boolean;
    windowsHide?: boolean;
  }
  export function spawn(cmd: string, args: readonly string[], opts?: SpawnOptions): ChildProcess;
  export function spawnSync(
    cmd: string,
    args: readonly string[],
    opts: { encoding: 'utf8'; windowsHide?: boolean; env?: Record<string, string | undefined>; cwd?: string; timeout?: number },
  ): { status: number | null; stdout: string; stderr: string; signal: string | null };
}

declare module 'node:sqlite' {
  type Param = string | number | bigint | null | Uint8Array;
  export class StatementSync {
    run(...params: Param[]): { changes: number | bigint; lastInsertRowid: number | bigint };
    get(...params: Param[]): Record<string, Param> | undefined;
    all(...params: Param[]): Record<string, Param>[];
  }
  export class DatabaseSync {
    constructor(path: string);
    exec(sql: string): void;
    prepare(sql: string): StatementSync;
    close(): void;
  }
}

// The vendored SDK is plain JavaScript; the engine's tests import it only for
// what they pin against it.
declare module '*/hive-plugin.mjs' {
  export function encodeWorkflowKey(key: string): string;
}
