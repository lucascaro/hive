// Minimal hand-written declarations for the node APIs the Layer B
// worktree spec needs.
//
// Same reasoning as the `process` shim in test/e2e/hive-global.d.ts:
// tsconfig's `types` is ["vite/client"] on purpose, and that array is
// program-global — pulling in @types/node to reach `fs` here would
// also swap setTimeout's return type under src/ (NodeJS.Timeout vs
// number) and light up files this change never touched. A dozen lines
// instead, narrowed to exactly what is used.

declare module 'node:child_process' {
  export function execFileSync(
    file: string,
    args: readonly string[],
    options: { encoding: 'utf8'; stdio?: 'ignore' | 'pipe' },
  ): string;
}

declare module 'node:fs' {
  export function existsSync(path: string): boolean;
  export function writeFileSync(path: string, data: string): void;
  export function rmSync(
    path: string,
    options?: { recursive?: boolean; force?: boolean },
  ): void;
}

declare module 'node:path' {
  export function join(...parts: string[]): string;
}
