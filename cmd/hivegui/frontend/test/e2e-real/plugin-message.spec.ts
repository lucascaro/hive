// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { test, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

// PLUGIN_MESSAGE (spec 528) end to end: a real plugin process using the
// SDK, a real hived and hived-ws-bridge, in the isolated state dir. The
// spec is the "GUI": a bridge client, which hived treats as UI-capable.
// It checks both directions, that the daemon stamps the sender, and
// that a message to a plugin that is not running comes back as an
// error rather than vanishing.

const WS_URL = process.env.WS_BRIDGE_URL;
const TMP = process.env.HIVE_E2E_REAL_TMP ?? '';
const repoRoot = execFileSync('git', ['rev-parse', '--show-toplevel'], {
  encoding: 'utf8',
}).trim();

interface Pushed {
  event: string;
  args: string[];
}

test('a plugin and the GUI exchange messages through the daemon', async () => {
  expect(
    WS_URL,
    'WS_BRIDGE_URL not set — globalSetup did not run',
  ).toBeTruthy();
  expect(TMP).toBeTruthy();

  // A plugin that announces itself and echoes every message back with
  // the id the daemon stamped, so the spec sees what a plugin received.
  const dir = fs.mkdtempSync(path.join(TMP, 'echo-msg-'));
  fs.writeFileSync(
    path.join(dir, 'hive-plugin.json'),
    JSON.stringify({
      id: 'echo-msg',
      name: 'Echo messages',
      version: '0.1.0',
      api_version: '0.2',
      main: { command: ['node', 'main.mjs'] },
    }),
  );
  fs.copyFileSync(
    path.join(repoRoot, 'plugins', 'sdk', 'hive-plugin.mjs'),
    path.join(dir, 'hive-plugin.mjs'),
  );
  fs.writeFileSync(
    path.join(dir, 'main.mjs'),
    `import { connect } from './hive-plugin.mjs';
const hive = await connect();
hive.postMessage('ready', { pid: process.pid });
hive.on('PLUGIN_MESSAGE', (m) => {
  hive.postMessage(m.topic + '.echo', { got: m.data, plugin_id: m.plugin_id });
});
`,
  );

  const WS = globalThis.WebSocket ?? (await import('ws')).WebSocket;
  const ws = new WS(WS_URL as string);
  await new Promise((res, rej) => {
    ws.onopen = res;
    ws.onerror = rej;
  });
  const pushed: Pushed[] = [];
  const replies = new Map<number, { error?: string }>();
  ws.addEventListener('message', (ev) => {
    const m = JSON.parse(ev.data as string);
    if (m.event) pushed.push(m as Pushed);
    else if (typeof m.id === 'number') replies.set(m.id, m);
  });
  let nextId = 0;
  const call = async (method: string, params: object = {}) => {
    const id = ++nextId;
    ws.send(JSON.stringify({ id, method, params }));
    await expect
      .poll(() => replies.has(id), { message: `no reply to ${method}` })
      .toBe(true);
    const r = replies.get(id);
    if (r?.error) throw new Error(`${method}: ${r.error}`);
  };
  const awaitPush = async <T>(
    event: string,
    pred: (v: T) => boolean,
  ): Promise<T> => {
    let found: T | undefined;
    await expect
      .poll(
        () => {
          for (const p of pushed) {
            if (p.event !== event) continue;
            const v = JSON.parse(p.args[0]) as T;
            if (pred(v)) {
              found = v;
              return true;
            }
          }
          return false;
        },
        { message: `no ${event} matching`, timeout: 15000 },
      )
      .toBe(true);
    return found as T;
  };

  try {
    await call('ConnectControl');
    await call('InstallPlugin', { source: dir, nonce: 'n-1' });
    await call('SetPluginEnabled', { id: 'echo-msg', enabled: true });

    // Plugin -> GUI: the daemon stamps the sender.
    const ready = await awaitPush<{
      plugin_id: string;
      topic: string;
      data: { pid: number };
    }>('plugin:message', (m) => m.topic === 'ready');
    expect(ready.plugin_id).toBe('echo-msg');
    expect(typeof ready.data.pid).toBe('number');

    // GUI -> plugin and back.
    await call('SendPluginMessage', {
      plugin_id: 'echo-msg',
      topic: 'ping',
      data: { n: 1, nonce: 'abc' },
    });
    const echo = await awaitPush<{
      plugin_id: string;
      data: { got: { n: number; nonce: string }; plugin_id: string };
    }>(
      'plugin:message',
      (m) => (m as { topic?: string }).topic === 'ping.echo',
    );
    expect(echo.plugin_id).toBe('echo-msg');
    expect(echo.data.got).toEqual({ n: 1, nonce: 'abc' });
    // The plugin saw its own id as the target, as sent.
    expect(echo.data.plugin_id).toBe('echo-msg');

    // A plugin that is not running.
    await call('SendPluginMessage', { plugin_id: 'ghost', topic: 'ping' });
    await awaitPush<{ code: string }>(
      'control:error',
      (e) => e.code === 'plugin_not_running',
    );

    // A bad topic is refused, not relayed.
    await call('SendPluginMessage', {
      plugin_id: 'echo-msg',
      topic: 'Bad Topic',
    });
    await awaitPush<{ code: string }>(
      'control:error',
      (e) => e.code === 'invalid_plugin_message',
    );
  } finally {
    await call('RemovePlugin', { id: 'echo-msg' }).catch(() => {});
    ws.close();
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
