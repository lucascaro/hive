// Debug/e2e-only globals. None of these exist in a normal production
// build: `__hive_state` is gated on the Vite mock/real env vars
// (app/state.ts) and the scrolltrace pair is gated on localStorage
// `hive.debug` (app/trace.ts).
//
// `__hive` used to be declared here as `unknown`. It moved to
// test/e2e/hive-global.d.ts in wave 7b, where the harnesses that inject it
// live and where it could be given a real type — nothing under src/ reads it.

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
    // Injected by playwright.real.config's addInitScript before page.goto,
    // read by test/e2e-real/wails-bridge.ts.
    __WS_BRIDGE_URL?: string;
  }
}

declare module '*.html?raw' {
  const content: string;
  export default content;
}

declare module '*.svg?raw' {
  const content: string;
  export default content;
}
