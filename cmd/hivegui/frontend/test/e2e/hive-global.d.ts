// Globals that exist only under Playwright. None of these are declared in
// src/globals.d.ts on purpose: nothing under src/ reads them, and the types
// belong beside the harnesses that inject them.
//
// Location is documentation, not enforcement. This file is in the program
// (tsconfig includes test/e2e/**/*), so its `declare global` block is visible
// to src/** too — `window.__hive` is non-optional and `process` exists
// everywhere, not just here. Nothing under src/ touches either today, so it
// is inert; do not mistake the directory for a scope.
//
// `__hive` is injected by BOTH bridges, and their surfaces differ:
// test/e2e/wails-mock.ts assigns all thirteen members, test/e2e-real/
// wails-bridge.ts only the five that don't need a scripted state machine
// (the daemon IS the state machine there). So the five are required and the
// mock-only eight are optional. One interface, no casts, and the optionality
// is a true statement about the runtime rather than a lie that makes the
// spec sites read more nicely — the cost is `?.` at the mock-only sites.
import type {
  MockBranch,
  MockIdea,
  MockProject,
  MockSession,
  MockWorktree,
} from './wails-mock.js';
import type { ReflowApi } from './fixtures/xterm-reflow.js';

type Handler = (...args: unknown[]) => void;
type StdinEntry = { id: string; b64: string; text: string };

interface HiveTestApi {
  // --- Assigned by both harnesses ---
  stdinLog: StdinEntry[];
  stdinText(id?: string): string;
  resetStdin(): void;
  listeners: Map<string, Handler[]>;
  emit(name: string, ...args: unknown[]): void;

  // --- Mock only (test/e2e/wails-mock.ts). Absent under VITE_WAILS_REAL. ---
  state?: {
    projects: MockProject[];
    sessions: MockSession[];
    worktrees: MockWorktree[];
    orphanBranches: MockBranch[];
    // Branch names the GUI asked to delete on the remote.
    deletedRemotes: string[];
    ideas: MockIdea[];
  };
  addSession?(
    name: string,
    insertAfter?: string,
    projectId?: string,
  ): Promise<string>;
  killSession?(id: string, force?: boolean): Promise<string>;
  setSessionState?(id: string, next: string, source?: string): void;
  ringBell?(id: string): void;
  createSessionWithWorktree?(name: string, branch?: string): Promise<string>;
  seedWorktrees?(worktrees: MockWorktree[], branches?: MockBranch[]): void;
  seedIdeas?(ideas: MockIdea[]): void;
  replayLog?: { id: string; t: number }[];
  replayCount?(id?: string): number;
  resetReplay?(): void;
  failNext?(method: string, message?: string): void;
  delayNext?(method: string, ms?: number): void;
  phaseHold?(ms?: number): void;
  // Background colour the first open terminal is actually painted with.
  termThemeBg?(): string;
  // The sixteen ANSI slots as the first open terminal holds them.
  termAnsi?(): string[];
}

declare global {
  interface Window {
    // Not optional: whichever bridge Vite substituted has already run by the
    // time any spec evaluates, so an outer `?.` would be noise on 109 sites.
    __hive: HiveTestApi;
    // Assigned by test/e2e/fixtures/xterm-reflow.ts, read by its spec through
    // the fixture page only — hence optional, like the debug globals in
    // src/globals.d.ts.
    // Set by test/e2e/ordering.spec.ts to record whether anything
    // consumed a ⌘-arrow keydown. Optional for the same reason as
    // __reflow: only that one spec assigns it.
    __arrowPrevented?: boolean | null;
    __reflow?: ReflowApi;
    __reflowReady?: boolean;
  }

  // Playwright specs run in node and read process.platform (and process.env
  // once wave 7c converts test/e2e-real), but tsconfig's `types` is
  // ["vite/client"] — deliberately no @types/node. That array is
  // program-global, so pulling node types in to reach `process` would also
  // swap setTimeout's return type under src/ (NodeJS.Timeout vs number) and
  // put errors in files this wave never touched. A few lines instead.
  const process: {
    // A union, not `string`, and that is the whole point of hand-writing it:
    // `process.platform === 'darwin'` is the modifier-key switch in 16 specs,
    // and against `string` a typo ('Darwin', 'macos') compiles clean, sends
    // Control instead of Meta on every mac run, and still passes on Linux CI
    // — so the mac-only path silently stops being exercised. Narrowed, that
    // typo is TS2367. Listed values are this repo's CI matrix.
    platform: 'darwin' | 'linux' | 'win32';
    env: Record<string, string | undefined>;
  };
}

export type { HiveTestApi };
