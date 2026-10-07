// Minimal ambient declarations for the few Node APIs this prototype uses, so
// `tsc` can check it without an @types/node dependency (spec 495). Only what
// is imported here is declared; widen it when a new import needs it.

declare const process: {
  argv: string[];
  execPath: string;
  exitCode: number | undefined;
  stdout: { write(s: string): boolean };
  stderr: { write(s: string): boolean };
};

interface ImportMeta {
  url: string;
}

declare module 'node:test' {
  export function test(name: string, fn: () => void | Promise<void>): void;
}

declare module 'node:assert/strict' {
  const assert: {
    (value: unknown, message?: string): asserts value;
    ok(value: unknown, message?: string): asserts value;
    equal(actual: unknown, expected: unknown, message?: string): void;
    notEqual(actual: unknown, expected: unknown, message?: string): void;
    deepEqual(actual: unknown, expected: unknown, message?: string): void;
    match(value: string, re: RegExp, message?: string): void;
    throws(fn: () => unknown, expected?: RegExp, message?: string): void;
  };
  export default assert;
}

declare module 'node:fs' {
  export function readFileSync(path: string, encoding: 'utf8'): string;
  export function writeFileSync(path: string, data: string): void;
  export function mkdtempSync(prefix: string): string;
  export function rmSync(path: string, opts: { recursive: boolean; force: boolean }): void;
}

declare module 'node:os' {
  export function tmpdir(): string;
}

declare module 'node:path' {
  export function join(...parts: string[]): string;
  export function dirname(path: string): string;
}

declare module 'node:url' {
  export function fileURLToPath(url: string): string;
  export function pathToFileURL(path: string): { href: string };
}

declare module 'node:child_process' {
  export function spawnSync(
    cmd: string,
    args: string[],
    opts: { encoding: 'utf8' },
  ): { status: number | null; stdout: string; stderr: string };
}
