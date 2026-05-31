// Layer B globalSetup: build hived + hived-ws-bridge, spawn both
// against fully-isolated temp dirs, write the bridge URL to a temp
// file so individual specs (and the Vite server) can pick it up.
// Teardown runs in globalTeardown.js.
//
// Isolation invariants (mirroring the Layer A Go side):
//   HOME, HIVE_STATE_DIR, HIVE_SOCKET → fresh mktemp paths each run.
//   The bridge refuses to start if either HIVE env var is missing
//   or escapes a recognised temp prefix.

import { spawn, spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const REPO_ROOT = path.resolve(__dirname, '../../../../..');

function makeTempDir(prefix) {
  return fs.mkdtempSync(path.join('/tmp', prefix));
}

function buildBinary(pkg, outPath) {
  const r = spawnSync('go', ['build', '-o', outPath, pkg], {
    cwd: REPO_ROOT,
    stdio: ['ignore', 'inherit', 'inherit'],
  });
  if (r.status !== 0) {
    throw new Error(`go build ${pkg} failed (status ${r.status})`);
  }
}

function waitForFile(p, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  return new Promise((resolve, reject) => {
    const tick = () => {
      if (fs.existsSync(p)) return resolve();
      if (Date.now() > deadline) return reject(new Error(`timeout waiting for ${p}`));
      setTimeout(tick, 25);
    };
    tick();
  });
}

async function readBridgeUrl(stdout, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  return new Promise((resolve, reject) => {
    let buf = '';
    const onData = (chunk) => {
      buf += chunk.toString();
      const m = buf.match(/(ws:\/\/[^\s]+)/);
      if (m) {
        stdout.off('data', onData);
        resolve(m[1]);
      }
    };
    stdout.on('data', onData);
    setTimeout(() => {
      stdout.off('data', onData);
      reject(new Error(`bridge URL not printed within ${timeoutMs}ms; got: ${buf.slice(0, 200)}`));
    }, timeoutMs);
  });
}

export default async function globalSetup() {
  // Temp roots — every run is fresh; teardown removes them.
  const tmp = makeTempDir('hive-e2e-real-');
  const stateDir = path.join(tmp, 'state');
  const sock = path.join(tmp, 'hived.sock');
  const home = path.join(tmp, 'home');
  fs.mkdirSync(home, { recursive: true });

  const env = {
    ...process.env,
    HOME: home,
    HIVE_STATE_DIR: stateDir,
    HIVE_SOCKET: sock,
    TERM: 'dumb',
  };

  // Build both binaries to a per-run path so concurrent CI workspaces
  // don't clobber each other.
  const hivedBin = path.join(tmp, 'hived');
  const bridgeBin = path.join(tmp, 'hived-ws-bridge');
  buildBinary('./cmd/hived', hivedBin);
  buildBinary('./cmd/hived-ws-bridge', bridgeBin);

  // Spawn hived. The wire-protocol log tee lands under stateDir.
  const hived = spawn(hivedBin, ['--socket', sock, '--shell', '/bin/bash', '--cols', '80', '--rows', '24'], {
    env,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  hived.stderr.on('data', (b) => process.stderr.write(`[hived] ${b}`));
  await waitForFile(sock, 5000);

  // Spawn the bridge; it prints its bound URL to stdout. We pin the
  // port to 0 (OS-picked) so parallel CI runs don't collide.
  const bridge = spawn(bridgeBin, ['--addr', '127.0.0.1:0'], {
    env,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  bridge.stderr.on('data', (b) => process.stderr.write(`[ws-bridge] ${b}`));
  const wsUrl = await readBridgeUrl(bridge.stdout, 5000);

  // Pass state to the spec process(es) and to the Vite dev server. The
  // tests use process.env.WS_BRIDGE_URL inside addInitScript to install
  // window.__WS_BRIDGE_URL before main.js loads.
  process.env.WS_BRIDGE_URL = wsUrl.trim();
  process.env.HIVE_E2E_REAL_TMP = tmp;
  process.env.HIVE_E2E_REAL_PIDS = JSON.stringify({
    hived: hived.pid,
    bridge: bridge.pid,
  });
}
