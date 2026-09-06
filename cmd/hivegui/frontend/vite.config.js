import { defineConfig } from 'vite';
import path from 'node:path';

// Wails bridge substitution for tests:
//   VITE_WAILS_MOCK=1  → resolve App/runtime to test/e2e/wails-mock.ts
//                        (in-browser scripted state machine; fast).
//   VITE_WAILS_REAL=1  → resolve to test/e2e-real/wails-bridge.ts,
//                        which round-trips every call through
//                        hived-ws-bridge to a real hived daemon
//                        (Layer B end-to-end coverage).
//
// In normal Wails builds neither var is set; wails dev/build writes
// the real bindings into ./wailsjs and the resolver is a no-op.
const useMock = process.env.VITE_WAILS_MOCK === '1';
const useReal = process.env.VITE_WAILS_REAL === '1';
const substitute = useReal
  ? path.resolve(__dirname, 'test/e2e-real/wails-bridge.ts')
  : useMock
    ? path.resolve(__dirname, 'test/e2e/wails-mock.ts')
    : null;

export default defineConfig({
  plugins: [
    {
      name: 'hive-wails-substitute',
      enforce: 'pre',
      resolveId(id, _importer) {
        if (!substitute) return null;
        if (
          id === '../wailsjs/go/main/App' ||
          id === '../wailsjs/runtime/runtime'
        ) {
          return substitute;
        }
        return null;
      },
    },
  ],
  server: {
    port: Number(process.env.VITE_PORT || 5173),
    strictPort: true,
    // lib/whats-new.ts imports site/features.json — the single user-facing
    // feature list, shared with the website build — which lives above this
    // root. Vite's default fs.allow is the inferred workspace root, and a
    // miss here is invisible in `vite build` (the JSON is inlined) but 403s
    // in `vite dev`, which is what every Playwright spec runs against. So
    // every e2e spec fails, not just the What's New ones.
    fs: { allow: [path.resolve(__dirname, '../../..')] },
  },
});
