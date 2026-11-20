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
  const hold = setTimeout(() => {}, 1000); // a referenced timer: the listing's own is unref'd, and the loop must outlive it
  assert.equal(await feed.listSessions(send, 10), undefined); // never answered in time
  clearTimeout(hold);
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

test('a listing past the owed-answer limit is declined and sent nowhere, and no owed slot is dropped', async () => {
  const { hive, feed, send, sent } = rig();
  const first = feed.listSessions(send, 5000);
  for (let i = 1; i < 64; i++) void feed.listSessions(send, 5000);
  assert.equal(sent.length, 64);
  assert.equal(await feed.listSessions(send, 5000), undefined, 'the 65th is declined');
  assert.equal(sent.length, 64, 'and not sent');
  hive.emit('SESSIONS', frame('one'));
  assert.deepEqual(ids(await first), ['one'], 'the first request still gets its own answer');
  feed.pokeAll();
});

test('a session a full listing no longer has is told to its subscribers', async () => {
  const { hive, feed } = rig();
  hive.emit('SESSIONS', frame('keep', 'drop'));
  let told = 0;
  const unsub = feed.subscribe('drop', () => told++);
  hive.emit('SESSIONS', frame('keep'));
  assert.equal(told, 1);
  assert.equal(feed.session('drop'), undefined);
  unsub();
});

test('a transcript read is answered by its own refusal, and an unrelated error on the session does not end it', async () => {
  const { hive, feed } = rig();
  const sends: string[] = [];
  const read = feed.fetchTranscript((n) => void sends.push(n), 's1', 1000);
  hive.emit('ERROR', { code: 'acp_busy', message: 'a turn is running', session_id: 's1' }); // someone else's refusal
  hive.emit('ACP_TRANSCRIPT', { session_id: 's1', epoch: 1, prompt_id: 3, reset: true });
  assert.equal((await read)?.prompt_id, 3, 'the snapshot still arrived');
  const refused = feed.fetchTranscript(() => {}, 's2', 1000);
  hive.emit('ERROR', { code: 'no_such_session', message: 'that session is not open', session_id: 's2' });
  assert.equal(await refused, undefined);
});
