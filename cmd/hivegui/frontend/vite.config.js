import { defineConfig } from 'vite';
import path from 'node:path';

// When VITE_WAILS_MOCK=1 (set by the E2E harness), the imports
// `../wailsjs/go/main/App` and `../wailsjs/runtime/runtime`
// resolve to test/e2e/wails-mock.js instead of the (uncommitted,
// generated-at-wails-build-time) Wails bindings. This lets us run
// vite dev + Playwright without a native Wails build.
//
// In normal Wails builds the env var is unset; wails dev/build
// writes the real bindings into ./wailsjs and the resolver below
// is a no-op.
const useMock = process.env.VITE_WAILS_MOCK === '1';

export default defineConfig({
  plugins: [
    {
      name: 'hive-wails-mock',
      enforce: 'pre',
      resolveId(id, importer) {
        if (!useMock) return null;
        if (id === '../wailsjs/go/main/App' || id === '../wailsjs/runtime/runtime') {
          return path.resolve(__dirname, 'test/e2e/wails-mock.js');
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
