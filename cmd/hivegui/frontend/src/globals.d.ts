// Debug/e2e-only globals. None of these exist in a normal production
// build: `__hive_state` is gated on the Vite mock/real env vars
// (app/state.ts), the scrolltrace pair is gated on localStorage
// `hive.debug` (app/trace.ts), and `__hive` is injected by the
// Playwright bridges (test/e2e/wails-mock.ts, test/e2e-real/wails-bridge.ts).

import type { AppState } from './app/state.js';
import type { ScrollTraceEntry } from './lib/scroll-debug.js';

declare global {
  interface Window {
    __hive_state?: AppState;
    __hive_scrolltrace?: ScrollTraceEntry[];
    __hive_dumpscroll?: () => {
      enabled: boolean;
      ring: ScrollTraceEntry[];
      lastJump: unknown;
      counters: Record<string, number>;
      maxStallMs: number;
    };
    __hive?: unknown;
    // Injected by playwright.real.config's addInitScript before page.goto,
    // read by test/e2e-real/wails-bridge.ts.
    __WS_BRIDGE_URL?: string;
  }
}
