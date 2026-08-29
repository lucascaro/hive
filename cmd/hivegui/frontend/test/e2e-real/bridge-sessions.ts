// Shared bridge helpers for the e2e-real specs.
//
// The suite runs ONE daemon for every spec file (globalSetup spawns it
// once), so a spec that creates sessions must remove them or the next
// file inherits them — session-phases.spec.ts asserts on the session
// COUNT and goes red when it does. Both of the specs that create
// sessions need the same socket boilerplate, and a second copy is how
// the two would drift, so it lives here.
import { test } from '@playwright/test';

const WS_URL = process.env.WS_BRIDGE_URL;

// bridgeCalls opens one control connection to the ws-bridge and runs a
// sequence of JSON-RPC calls over it, in order, failing on the first
// error. Both addSecondSession and the cleanup below need this, and a
// second copy of the socket boilerplate is how the two would drift.
export async function bridgeCalls(calls: Array<[string, object]>) {
  if (!WS_URL)
    throw new Error('WS_BRIDGE_URL not set — globalSetup did not run');
  // Node < 22 has no global WebSocket; fall back to the ws package, typed by
  // the hand-written ws-shim.d.ts (see there for why not @types/ws).
  const WS = globalThis.WebSocket ?? (await import('ws')).WebSocket;
  const ws = new WS(WS_URL);
  await new Promise((res, rej) => {
    ws.onopen = res;
    ws.onerror = rej;
  });
  const send = (id: number, method: string, params: object = {}) =>
    ws.send(JSON.stringify({ id, method, params }));
  const waitFor = (id: number) =>
    new Promise<{ id: number; error?: string }>((res) => {
      ws.addEventListener('message', function h(ev) {
        const m = JSON.parse(ev.data);
        if (m.id === id) {
          ws.removeEventListener('message', h);
          res(m);
        }
      });
    });
  try {
    send(1, 'ConnectControl');
    await waitFor(1);
    let id = 1;
    for (const [method, params] of calls) {
      id += 1;
      send(id, method, params);
      const resp = await waitFor(id);
      if (resp.error) throw new Error(`${method} via bridge: ${resp.error}`);
    }
  } finally {
    ws.close();
  }
}

// killAndAwaitRemoval sends KILL_SESSION for each id and resolves only
// once the daemon has broadcast `removed` for all of them, so teardown
// is actually complete when it returns.
async function killAndAwaitRemoval(ids: string[]) {
  if (!WS_URL)
    throw new Error('WS_BRIDGE_URL not set — globalSetup did not run');
  const WS = globalThis.WebSocket ?? (await import('ws')).WebSocket;
  const ws = new WS(WS_URL);
  await new Promise((res, rej) => {
    ws.onopen = res;
    ws.onerror = rej;
  });
  const pending = new Set(ids);
  const done = new Promise<void>((resolve) => {
    ws.addEventListener('message', (ev) => {
      const m = JSON.parse(ev.data);
      // The bridge relays daemon fanout as notifications; session:list
      // (sent on connect) and session:event both settle this.
      if (m.event === 'session:event') {
        const parsed = JSON.parse(m.args?.[0] ?? '{}');
        if (parsed.kind === 'removed' && parsed.session?.id) {
          pending.delete(parsed.session.id);
        }
      } else if (m.event === 'session:list') {
        const parsed = JSON.parse(m.args?.[0] ?? '{}');
        const live = new Set(
          (parsed.sessions ?? []).map((x: { id: string }) => x.id),
        );
        for (const id of [...pending]) if (!live.has(id)) pending.delete(id);
      }
      if (pending.size === 0) resolve();
    });
  });
  const send = (id: number, method: string, params: object = {}) =>
    ws.send(JSON.stringify({ id, method, params }));
  const reply = (id: number) =>
    new Promise<{ id: number; error?: string }>((res) => {
      ws.addEventListener('message', function h(ev) {
        const m = JSON.parse(ev.data);
        if (m.id === id) {
          ws.removeEventListener('message', h);
          res(m);
        }
      });
    });
  try {
    // AWAIT the handshake before killing anything. The bridge answers
    // KillSession with "no control connection" if it arrives before
    // ConnectControl has dialled — which is silently fatal here, since
    // the kill never reaches the daemon and teardown then waits out its
    // timeout for a removal that was never requested.
    send(1, 'ConnectControl');
    const hello = await reply(1);
    if (hello.error) throw new Error(`ConnectControl: ${hello.error}`);
    let n = 1;
    for (const id of ids) {
      n += 1;
      send(n, 'KillSession', { session_id: id, force: true });
    }
    await Promise.race([
      done,
      new Promise<void>((_, rej) =>
        setTimeout(
          () => rej(new Error('teardown: sessions never removed')),
          15000,
        ),
      ),
    ]);
  } finally {
    ws.close();
  }
}

// registerSessionCleanup wires an afterAll that removes every session
// the caller recorded. afterALL, not afterEach: killing a session
// between tests reflows the grid and rebaselines the replay column
// baseline, which destabilised scroll-codex measurably (2 failures in
// 6 runs against 0 without it).
export function registerSessionCleanup(ids: Set<string>) {
  test.afterAll(async () => {
    if (ids.size === 0) return;
    try {
      await killAndAwaitRemoval([...ids]);
    } catch {
      // Best effort — a teardown error must not mask a real failure.
    }
    ids.clear();
  });
}
