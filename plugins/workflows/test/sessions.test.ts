// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The session feed's reads of the daemon (spec 529, B3c): LIST_SESSIONS has no
// req_id, so each SESSIONS frame must answer the request it belongs to.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { SessionFeed } from '../engine/sessions.ts';
import { FakeHive } from './harness.ts';

const frame = (...ids: string[]) => ({ sessions: ids.map((id) => ({ id, alive: true, kind: 'acp' })) });
const ids = (r: { id: string }[] | undefined): string[] | undefined => r?.map((s) => s.id);
const tick = (): Promise<void> => new Promise((r) => setImmediate(r));

function rig() {
  const hive = new FakeHive();
  const feed = new SessionFeed();
  feed.attach(hive);
  const sent: string[] = [];
  return { hive, feed, send: (n: string) => void sent.push(n), sent };
}

test('two listings in flight: each SESSIONS frame answers the oldest, so the second never reads the first\'s answer', async () => {
  const { hive, feed, send, sent } = rig();
  const first = feed.listSessions(send, 1000);
  const second = feed.listSessions(send, 1000);
  assert.deepEqual(sent, ['LIST_SESSIONS', 'LIST_SESSIONS']);
  hive.emit('SESSIONS', frame('old'));
  assert.deepEqual(ids(await first), ['old']);
  let secondDone = false;
  void second.then(() => (secondDone = true));
  await tick();
  assert.equal(secondDone, false, 'the first answer is not the second request\'s');
  hive.emit('SESSIONS', frame('new'));
  assert.deepEqual(ids(await second), ['new']);
});

test('a listing that gave up still owns its answer: the late frame does not answer the next request', async () => {
  const { hive, feed, send } = rig();
  assert.equal(await feed.listSessions(send, 10), undefined); // never answered in time
  const next = feed.listSessions(send, 1000);
  hive.emit('SESSIONS', frame('late answer to the first'));
  let done = false;
  void next.then(() => (done = true));
  await tick();
  assert.equal(done, false);
  hive.emit('SESSIONS', frame('its own'));
  assert.deepEqual(ids(await next), ['its own']);
});

test('a listing whose send throws leaves no answer owed', async () => {
  const { hive, feed } = rig();
  assert.equal(
    await feed.listSessions(() => {
      throw new Error('write after end');
    }, 1000),
    undefined,
  );
  const ok = feed.listSessions(() => {}, 1000);
  hive.emit('SESSIONS', frame('x'));
  assert.deepEqual(ids(await ok), ['x']);
});

test('a lost connection answers every waiting listing with nothing', async () => {
  const { feed, send } = rig();
  const a = feed.listSessions(send, 5000);
  const b = feed.listSessions(send, 5000);
  feed.pokeAll();
  assert.deepEqual(await Promise.all([a, b]), [undefined, undefined]);
});
