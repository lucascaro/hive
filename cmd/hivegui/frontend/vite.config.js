import { defineConfig } from 'vite';
import fs from 'node:fs';
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

// Mock mode only: serve UI plugin files at /plugins/<id>/…, which is
// what the app's asset server Handler does from the state dir (spec
// 471). Test fixtures (test/fixtures/plugins) first, then the repo's own
// plugins/. Read-only, and a path must stay inside its plugin dir.
const pluginRoots = [
  path.resolve(__dirname, 'test/fixtures/plugins'),
  path.resolve(__dirname, '../../../plugins'),
];
const pluginTypes = {
  '.js': 'text/javascript',
  '.mjs': 'text/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
};
function mockPluginAssets() {
  return {
    name: 'hive-mock-plugin-assets',
    configureServer(server) {
      server.middlewares.use('/plugins/', (req, res, next) => {
        const rel = decodeURIComponent((req.url || '').split('?')[0]).replace(
          /^\/+/,
          '',
        );
        const [id, ...rest] = rel.split('/');
        if (!/^[a-z0-9][a-z0-9-]{0,62}$/.test(id) || rest.length === 0)
          return next();
        for (const root of pluginRoots) {
          const dir = path.join(root, id);
          const file = path.resolve(dir, rest.join('/'));
          if (!file.startsWith(dir + path.sep)) return next();
          if (fs.existsSync(file) && fs.statSync(file).isFile()) {
            res.setHeader(
              'Content-Type',
              pluginTypes[path.extname(file)] || 'application/octet-stream',
            );
            res.setHeader('Cache-Control', 'no-store');
            fs.createReadStream(file).pipe(res);
            return;
          }
        }
        next();
      });
    },
  };
}

export default defineConfig({
  plugins: [
    ...(useMock ? [mockPluginAssets()] : []),
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
    //
    // `site/` and this root only, NOT the repo root: the dev server would
    // otherwise serve .git and every dotfile above us to anything that can
    // reach the port.
    fs: {
      allow: [__dirname, path.resolve(__dirname, '../../../site')],
    },
  },
});
