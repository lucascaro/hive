import { defineConfig } from 'vite';
import path from 'node:path';

// Wails bridge substitution for tests:
//   VITE_WAILS_MOCK=1  → resolve App/runtime to test/e2e/wails-mock.js
//                        (in-browser scripted state machine; fast).
//   VITE_WAILS_REAL=1  → resolve to test/e2e-real/wails-bridge.js,
//                        which round-trips every call through
//                        hived-ws-bridge to a real hived daemon
//                        (Layer B end-to-end coverage).
//
// In normal Wails builds neither var is set; wails dev/build writes
// the real bindings into ./wailsjs and the resolver is a no-op.
const useMock = process.env.VITE_WAILS_MOCK === '1';
const useReal = process.env.VITE_WAILS_REAL === '1';
const substitute = useReal
  ? path.resolve(__dirname, 'test/e2e-real/wails-bridge.js')
  : useMock
    ? path.resolve(__dirname, 'test/e2e/wails-mock.js')
    : null;

export default defineConfig({
  plugins: [
    {
      name: 'hive-wails-substitute',
      enforce: 'pre',
      resolveId(id, _importer) {
        if (!substitute) return null;
        if (id === '../wailsjs/go/main/App' || id === '../wailsjs/runtime/runtime') {
          return substitute;
        }
        return null;
      },
    },
  ],
  server: {
    port: Number(process.env.VITE_PORT || 5173),
    strictPort: true,
  },
});
