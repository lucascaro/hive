// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Run: node --test 'plugins/sdk/*.test.mjs'
// A fake daemon on a temp unix socket stands in for hived.

import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

import { connect, encodeWorkflowKey, Frame, REQUEST_VERBS } from './hive-plugin.mjs';

const unixOnly = { skip: process.platform === 'win32' ? 'unix sockets' : false };

function frame(type, payload) {
  const body = Buffer.from(JSON.stringify(payload));
  const head = Buffer.alloc(5);
  head[0] = type;
  head.writeUInt32BE(body.length, 1);
  return Buffer.concat([head, body]);
}

/**
 * A fake hived. `onFrame(type, payload, reply)` sees every frame after
 * HELLO; `reply(type, payload)` writes one back.
 */
async function fakeDaemon(onFrame) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'hive-sdk-'));
  const sock = path.join(dir, 's');
  const conns = new Set();
  const server = net.createServer((c) => {
    conns.add(c);
    let buf = Buffer.alloc(0);
    const reply = (type, payload) => c.write(frame(type, payload));
    c.on('data', (chunk) => {
      buf = Buffer.concat([buf, chunk]);
      while (buf.length >= 5) {
        const len = buf.readUInt32BE(1);
        if (buf.length < 5 + len) break;
        const type = buf[0];
        const payload = JSON.parse(buf.subarray(5, 5 + len).toString('utf8') || '{}');
        buf = buf.subarray(5 + len);
        if (type === Frame.HELLO) reply(Frame.WELCOME, { version: 1, mode: 'control' });
        else onFrame(type, payload, reply, c);
      }
    });
    c.on('error', () => {});
  });
  await new Promise((r) => server.listen(sock, r));
  const hive = await connect({ socket: sock, exitOnClose: false });
  return {
    hive,
    dropClients: () => conns.forEach((c) => c.destroy()),
    close: async () => {
      hive.close();
      conns.forEach((c) => c.destroy());
      await new Promise((r) => server.close(r));
      fs.rmSync(dir, { recursive: true, force: true });
    },
  };
}

test('request resolves on ACK with req_id', unixOnly, async () => {
  const seen = [];
  const d = await fakeDaemon((type, p, reply) => {
    seen.push([type, p]);
    reply(Frame.ACK, { req_id: p.req_id, session_id: 's-1' });
  });
  try {
    const ack = await d.hive.request('CREATE_SESSION', { kind: 'acp', agent: 'claude' });
    assert.equal(ack.session_id, 's-1');
    assert.equal(seen[0][0], Frame.CREATE_SESSION);
    assert.equal(seen[0][1].kind, 'acp');
    assert.equal(typeof seen[0][1].req_id, 'number');
    // Ids are per connection and increase.
    await d.hive.request('KILL_SESSION', { session_id: 's-1' });
    assert.ok(seen[1][1].req_id > seen[0][1].req_id);
  } finally {
    await d.close();
  }
});

test('request resolves on the frame a request has of its own', unixOnly, async () => {
  const d = await fakeDaemon((type, p, reply) => {
    if (type === Frame.LIST_ACP_AGENTS) reply(Frame.ACP_AGENTS, { req_id: p.req_id, agents: [{ id: 'claude' }] });
    if (type === Frame.SEARCH_TRANSCRIPT) reply(Frame.TRANSCRIPT_MATCHES, { req_id: p.req_id, matches: [] });
  });
  try {
    assert.deepEqual((await d.hive.request('LIST_ACP_AGENTS')).agents, [{ id: 'claude' }]);
    assert.deepEqual((await d.hive.request('SEARCH_TRANSCRIPT', { session_id: 'x', query: 'q' })).matches, []);
  } finally {
    await d.close();
  }
});

test('request rejects on ERROR with req_id', unixOnly, async () => {
  const d = await fakeDaemon((_type, p, reply) => reply(Frame.ERROR, { req_id: p.req_id, code: 'no_such_session', message: 'gone', session_id: 'x' }));
  try {
    await assert.rejects(d.hive.request('PROMPT_ACP', { session_id: 'x', text: 't' }), (err) => {
      assert.equal(err.hive.code, 'no_such_session');
      assert.equal(err.hive.req_id > 0, true);
      assert.match(err.message, /no_such_session/);
      return true;
    });
  } finally {
    await d.close();
  }
});

test('interleaved replies resolve the right promises', unixOnly, async () => {
  const held = [];
  const d = await fakeDaemon((_type, p, reply) => {
    held.push([p, reply]);
    if (held.length === 3) {
      // Answer out of order: 3, 1, 2.
      for (const i of [2, 0, 1]) held[i][1](Frame.ACK, { req_id: held[i][0].req_id, session_id: held[i][0].session_id });
    }
  });
  try {
    const rs = await Promise.all(['a', 'b', 'c'].map((s) => d.hive.request('KILL_SESSION', { session_id: s })));
    assert.deepEqual(rs.map((r) => r.session_id), ['a', 'b', 'c']);
  } finally {
    await d.close();
  }
});

test('a req_id chosen by the caller on send() cannot settle a request()', unixOnly, async () => {
  let release;
  const d = await fakeDaemon((type, p, reply) => {
    if (type === Frame.GET_TRANSCRIPT_LINES) {
      reply(Frame.TRANSCRIPT_LINES, { req_id: p.req_id, lines: [] }); // answers the caller's own id
    } else {
      release = () => reply(Frame.ACK, { req_id: p.req_id });
    }
  });
  try {
    let settled = false;
    const pending = d.hive.request('KILL_SESSION', { session_id: 'x' }).then((r) => {
      settled = true;
      return r;
    });
    const seen = new Promise((r) => d.hive.once('TRANSCRIPT_LINES', r));
    d.hive.send('GET_TRANSCRIPT_LINES', { session_id: 'x', req_id: 1 });
    await seen;
    await new Promise((r) => setImmediate(r));
    assert.equal(settled, false, "a caller's req_id settled a request()");
    release();
    await pending;
  } finally {
    await d.close();
  }
});

test('request ignores broadcasts carrying the same req_id', unixOnly, async () => {
  let release;
  const d = await fakeDaemon((_type, p, reply) => {
    // Broadcasts that happen to hold the same number must not settle it.
    reply(Frame.SESSION_EVENT, { req_id: p.req_id, kind: 'added', session: { id: 'x' } });
    reply(Frame.PLUGIN_EVENT, { req_id: p.req_id, kind: 'updated' });
    reply(Frame.ACP_TRANSCRIPT, { req_id: p.req_id, session_id: 'x', epoch: 1 });
    // An ERROR for a different request does not settle it either.
    reply(Frame.ERROR, { req_id: p.req_id + 1000, code: 'other', message: 'm' });
    release = () => reply(Frame.ACK, { req_id: p.req_id });
  });
  try {
    let settled = false;
    // The ERROR is the last frame the fake sends before holding back its
    // ACK; frames are handled in order and settling is synchronous, so
    // once the client has seen it every broadcast before it was seen too.
    const lastBroadcastSeen = new Promise((r) => d.hive.once('ERROR', r));
    const pending = d.hive.request('KILL_SESSION', { session_id: 'x' }).then((r) => {
      settled = true;
      return r;
    });
    await lastBroadcastSeen;
    await new Promise((r) => setImmediate(r)); // let any wrongly settled promise run its handler
    assert.equal(settled, false, 'a broadcast settled the request');
    release();
    assert.ok((await pending).req_id > 0);
  } finally {
    await d.close();
  }
});

test('timeout rejects', unixOnly, async () => {
  const d = await fakeDaemon(() => {});
  try {
    await assert.rejects(d.hive.request('KILL_SESSION', {}, { timeoutMs: 50 }), (err) => {
      assert.equal(err.hive.code, 'timeout');
      return true;
    });
    // A late reply to a timed-out request is ignored, not an error.
    assert.equal(d.hive._pending.size, 0);
  } finally {
    await d.close();
  }
});

test('close rejects every pending request', unixOnly, async () => {
  let n = 0;
  let bothArrived;
  const arrived = new Promise((r) => {
    bothArrived = r;
  });
  const d = await fakeDaemon(() => {
    if (++n === 2) bothArrived();
  });
  try {
    const a = d.hive.request('KILL_SESSION', {});
    const b = d.hive.request('KILL_SESSION', {});
    const settled = Promise.allSettled([a, b]);
    await arrived;
    d.dropClients();
    const [ra, rb] = await settled;
    assert.equal(ra.status, 'rejected');
    assert.equal(rb.status, 'rejected');
    assert.match(String(ra.reason.message), /closed/);
    assert.equal(d.hive._pending.size, 0);
  } finally {
    await d.close();
  }
});

test('send is unchanged: no req_id, no promise', unixOnly, async () => {
  let got;
  const arrived = new Promise((r) => {
    got = r;
  });
  const d = await fakeDaemon((type, p) => got([type, p]));
  try {
    d.hive.send('LIST_SESSIONS');
    assert.deepEqual(await arrived, [Frame.LIST_SESSIONS, {}]);
  } finally {
    await d.close();
  }
});

test('request refuses a verb that has no reply carrying a req_id', unixOnly, async () => {
  const seen = [];
  let marker;
  const markerSeen = new Promise((r) => {
    marker = r;
  });
  const d = await fakeDaemon((type, p) => {
    seen.push(type);
    if (p.marker) marker();
  });
  try {
    for (const name of ['LIST_SESSIONS', 'UPDATE_SESSION', 'REMOVE_WORKTREE', 'SET_PLUGIN_ENABLED', 'WAIT_SESSION', 'ACK']) {
      await assert.rejects(d.hive.request(name, {}), (err) => {
        assert.equal(err.hive.code, 'unsupported_request');
        assert.match(err.message, /use send\(\)/);
        return true;
      });
    }
    // Refused up front: nothing was sent, nothing is pending.
    assert.equal(d.hive._pending.size, 0);
    // Frames arrive in order: once a later marker frame has, anything the
    // refused requests might have sent has too.
    d.hive.send('LIST_SESSIONS', { marker: true });
    await markerSeen;
    assert.deepEqual(seen, [Frame.LIST_SESSIONS]);
  } finally {
    await d.close();
  }
});

test('every request verb is a real frame and none is a reply frame', () => {
  for (const v of REQUEST_VERBS) assert.ok(Frame[v] !== undefined, v);
  for (const v of ['ACK', 'ACP_AGENTS', 'ERROR']) assert.ok(!REQUEST_VERBS.includes(v), v);
});

test('request on an unknown frame name rejects', unixOnly, async () => {
  const d = await fakeDaemon(() => {});
  try {
    await assert.rejects(d.hive.request('NOPE'), /unknown frame/);
  } finally {
    await d.close();
  }
});

// ---- spec 528 PR 2: plugin messages, worktree frames, branch keys ----

test('postMessage sends PLUGIN_MESSAGE with topic and data, and no req_id', unixOnly, async () => {
  const seen = [];
  const d = await fakeDaemon((type, p) => seen.push([type, p]));
  try {
    d.hive.postMessage('run.state', { n: 1, items: [true, null] });
    d.hive.postMessage('ping');
    await new Promise((r) => setTimeout(r, 50));
    assert.deepEqual(seen, [
      [Frame.PLUGIN_MESSAGE, { topic: 'run.state', data: { n: 1, items: [true, null] } }],
      [Frame.PLUGIN_MESSAGE, { topic: 'ping' }],
    ]);
  } finally {
    await d.close();
  }
});

test('a PLUGIN_MESSAGE from the GUI is an event, and never settles a request', unixOnly, async () => {
  const d = await fakeDaemon((type, p, reply) => {
    // The GUI's message carries the same number as the pending request.
    if (type === Frame.PREPARE_WORKTREE) reply(Frame.PLUGIN_MESSAGE, { plugin_id: 'wf', topic: 't', data: { req_id: p.req_id } });
  });
  try {
    const got = new Promise((r) => d.hive.once('PLUGIN_MESSAGE', r));
    const pending = d.hive.request('PREPARE_WORKTREE', {}, { timeoutMs: 150 });
    const msg = await got;
    assert.equal(msg.plugin_id, 'wf');
    assert.equal(msg.topic, 't');
    await assert.rejects(pending, /timed out/);
  } finally {
    await d.close();
  }
});

test('request resolves PREPARE_WORKTREE on PREPARED_WORKTREE and COMMIT_WORKTREE on ACK', unixOnly, async () => {
  const d = await fakeDaemon((type, p, reply) => {
    if (type === Frame.PREPARE_WORKTREE) {
      reply(Frame.PREPARED_WORKTREE, {
        req_id: p.req_id, project_id: p.project_id, path: '/w', branch: p.branch, reattached: false,
        base_sha: 'a'.repeat(40), head_sha: 'b'.repeat(40), merged: [],
      });
    }
    if (type === Frame.COMMIT_WORKTREE) reply(Frame.ACK, { req_id: p.req_id, committed: true, head: 'c'.repeat(40) });
  });
  try {
    const prep = await d.hive.request('PREPARE_WORKTREE', {
      project_id: 'p1', workflow_run_id: 'r1', branch: 'hive/wf/r1/a', base: 'main',
    });
    assert.equal(prep.path, '/w');
    assert.equal(prep.branch, 'hive/wf/r1/a');
    const ack = await d.hive.request('COMMIT_WORKTREE', {
      project_id: 'p1', workflow_run_id: 'r1', branch: 'hive/wf/r1/a', message: 'm',
    });
    assert.deepEqual([ack.committed, ack.head], [true, 'c'.repeat(40)]);
  } finally {
    await d.close();
  }
});

test('PREPARE_WORKTREE rejects on the daemon refusal', unixOnly, async () => {
  const d = await fakeDaemon((type, p, reply) => {
    reply(Frame.ERROR, { req_id: p.req_id, code: 'invalid_base', message: 'nope' });
  });
  try {
    await assert.rejects(d.hive.request('PREPARE_WORKTREE', { base: '--x' }), (e) => e.hive.code === 'invalid_base');
  } finally {
    await d.close();
  }
});

test('encodeWorkflowKey maps [N] to _N and keeps /', () => {
  assert.equal(encodeWorkflowKey('fix[2]/work'), 'fix_2/work');
  assert.equal(encodeWorkflowKey('work/each[3]/do'), 'work/each_3/do');
  assert.equal(encodeWorkflowKey('outer[1]/inner[22]/do'), 'outer_1/inner_22/do');
  assert.equal(encodeWorkflowKey('plan'), 'plan');
  assert.equal(encodeWorkflowKey('a-b/c-d2'), 'a-b/c-d2');
});

test('encodeWorkflowKey rejects keys outside the grammar', () => {
  for (const bad of ['', 'A', '1a', 'a b', 'a//b', '/a', 'a/', 'a[]', 'a[x]', 'a[1', 'a[1][2]', 'a_1', 'a.b', 'a-', '-a', 'a--b', '../x', 'a\n', null, undefined, 5]) {
    assert.throws(() => encodeWorkflowKey(bad), /not a workflow node key/, JSON.stringify(bad));
  }
});

test('encodeWorkflowKey is injective and its output is a legal ref name', () => {
  const segs = ['a', 'ab', 'a-b', 'a1', 'fix', 'work'];
  const keys = new Set();
  for (const s of segs) {
    keys.add(s);
    for (const n of [0, 1, 2, 10, 11]) keys.add(`${s}[${n}]`);
  }
  const single = [...keys];
  for (const a of single) for (const b of single) keys.add(`${a}/${b}`);
  const seen = new Map();
  for (const k of keys) {
    const enc = encodeWorkflowKey(k);
    assert.ok(!seen.has(enc), `${k} and ${seen.get(enc)} both encode to ${enc}`);
    seen.set(enc, k);
  }
  assert.ok(seen.size > 100);
  if (spawnSync('git', ['--version']).status !== 0) return;
  for (const enc of [...seen.keys()].filter((_, i) => i % 7 === 0)) {
    const r = spawnSync('git', ['check-ref-format', '--branch', `hive/wf/r1/${enc}`]);
    assert.equal(r.status, 0, `${enc}: ${r.stderr}`);
  }
});
