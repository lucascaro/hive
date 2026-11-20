// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Agent nodes across a plugin crash, a daemon restart and a reconnect (spec
// 529, B3c). The engine is "restarted" by opening a new engine over the same
// runs.db and pointing the same scripted daemon at a new connection; the old
// engine is left inert, as a dead process would be. The real daemon and fake
// adapter are exercised by internal/workflowe2e.

import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { type AgentDeps, agentExecutor, INTERRUPTED_NOTE, REPROMPT } from '../engine/agent.ts';
import type { Row } from '../engine/model.ts';
import { workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { type Sim, type SimOptions, type SimSession, simulate } from './daemon-sim.ts';
import { FakeHive, type Harness, harness, rowOf, runState, tick, waitFor } from './harness.ts';

const out = s.object({ summary: s.string() });
const never = (): Promise<void> => new Promise(() => {});
const ok = (summary = 'done') => ({ status: 'ok', summary, data: { summary } });

const one = () =>
  workflow('w', s.object({}), (g) => {
    g.agent('a', { agent: 'claude', prompt: 'do it', output: out });
  });

const two = () =>
  workflow('w', s.object({}), (g) => {
    const a = g.agent('a', { agent: 'claude', prompt: 'do a', output: out });
    const b = g.agent('b', { agent: 'claude', prompt: 'do b', output: out });
    g.edge(a, b);
  });

interface World {
  dir: string;
  hive: FakeHive;
  sim: Sim;
  h: Harness;
  deps: AgentDeps;
  /** The run of the test. */
  id: string;
  cleanup(): void;
}

/** A daemon, and an engine connected to it. `quiet` leaves every turn open: the test ends it by hand. */
function world(ir: ReturnType<typeof one>, o: Partial<SimOptions> = {}, deps: AgentDeps = {}): World {
  const hive = new FakeHive();
  const dir = mkdtempSync(join(tmpdir(), 'wf-dur-'));
  const sim = simulate(hive, { onPrompt: () => {}, ...o });
  const all = { cancelGraceMs: 40, readyTimeoutMs: 400, settlePollMs: 5, ...deps };
  const h = harness({ agent: agentExecutor(all), check: never }, { dir, hive });
  const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'w.ts', ir, input: {} });
  return { dir, hive, sim, h, deps: all, id, cleanup: () => (h.close(), rmSync(dir, { recursive: true, force: true })) };
}

/**
 * The engine dies and a new one starts on the same runs.db and the same
 * daemon: `boot` is the daemon's boot id (the same one: only the plugin died; a
 * new one: `hived` restarted too). Returns after the new engine recovered.
 */
async function crash(w: World, boot = 'boot-1', deps: AgentDeps = {}): Promise<World> {
  w.h.engine.disconnect(); // what a dead process is: nothing of the old engine acts again
  w.h.close();
  const hive = new FakeHive();
  hive.welcome = { boot_id: boot };
  w.sim.attach(hive);
  const all = { ...w.deps, ...deps };
  const h = harness({ agent: agentExecutor(all), check: never }, { dir: w.dir, hive });
  await h.engine.recover();
  return { ...w, hive, h, deps: all, cleanup: () => (h.close(), rmSync(w.dir, { recursive: true, force: true })) };
}

const row = (w: World, key = 'a'): Row => rowOf(w.h, w.id, key) as Row;
const running = (w: World, key = 'a'): Promise<void> => waitFor(() => rowOf(w.h, w.id, key)?.state === 'running' && rowOf(w.h, w.id, key)?.prompt_id !== undefined, `${key} to be running with a turn`);
const done = (w: World): Promise<void> => waitFor(() => runState(w.h, w.id) === 'done', 'the run to finish');

/** Ends the turn of the session with a result, as the agent would. */
const finish = (sess: SimSession, summary = 'done'): void => {
  sess.submit(ok(summary));
  sess.end();
};

// ------------------------------------------------------------- plugin crash

test('plugin crash: a turn that ended while the plugin was down is recorded, and the attempt does not change', async () => {
  const w = await (async () => {
    const w0 = world(one());
    await running(w0);
    return w0;
  })();
  const sess = w.sim.sessions[0];
  w.h.engine.disconnect();
  finish(sess, 'finished while down'); // the daemon went on without us
  const w2 = await crash(w);
  try {
    await done(w2);
    const r = row(w2);
    assert.equal(r.attempt, 1, 'recorded, not retried');
    assert.deepEqual(r.output_raw, { summary: 'finished while down' });
    assert.equal(w2.sim.of('PROMPT_ACP').length, 1, 'the turn was not sent again');
    assert.equal(w2.sim.of('CREATE_SESSION').length, 1);
    assert.equal(w2.sim.of('COMMIT_WORKTREE').length, 1, 'committed once, by the engine that recorded the result');
    assert.equal(w2.sim.sessions.length, 1);
  } finally {
    w2.cleanup();
  }
});

test('plugin crash: a turn that is still running is watched, and ends the node when it ends', async () => {
  const w = world(one());
  await running(w);
  const w2 = await crash(w);
  try {
    await tick();
    assert.equal(row(w2).state, 'running');
    assert.equal(row(w2).attempt, 1);
    assert.equal(w2.sim.of('PROMPT_ACP').length, 1, 'no second turn on a session that is mid-turn');
    assert.equal(w2.sim.of('CREATE_SESSION').length, 1);
    finish(w2.sim.sessions[0]);
    await done(w2);
    assert.equal(row(w2).attempt, 1);
  } finally {
    w2.cleanup();
  }
});

test('plugin crash: a node waiting on a permission is still waiting, and resumes when it is answered', async () => {
  const w = world(one());
  await running(w);
  const sess = w.sim.sessions[0];
  sess.ask({ request_id: 'p1', title: 'Run it?', options: [{ option_id: 'allow', name: 'Allow', kind: 'allow_once' }] });
  await waitFor(() => row(w).state === 'waiting', 'the permission wait');
  const w2 = await crash(w);
  try {
    await tick();
    assert.equal(row(w2).state, 'waiting', 'a waiting agent is picked up, not left with no one watching');
    assert.equal(row(w2).waiting?.request_id, 'p1');
    sess.answer();
    await waitFor(() => row(w2).state === 'running', 'the node to resume');
    finish(sess);
    await done(w2);
    assert.equal(row(w2).attempt, 1);
  } finally {
    w2.cleanup();
  }
});

test('plugin crash: a session that died while the plugin was down interrupts the node, and its retry restarts that session', async () => {
  const w = world(one());
  await running(w);
  const sess = w.sim.sessions[0];
  w.h.engine.disconnect();
  sess.event('updated', { alive: false }); // the adapter exited while the plugin was down
  const w2 = await crash(w);
  try {
    await waitFor(() => w2.sim.of('RESTART_SESSION').length === 1, 'the restart');
    await waitFor(() => w2.sim.prompts.length === 2, 'the retry prompt');
    assert.equal(w2.sim.prompts[1], INTERRUPTED_NOTE + w2.sim.prompts[0]);
    assert.equal(row(w2).attempt, 2);
    finish(sess);
    await done(w2);
    assert.equal(w2.sim.sessions.length, 1, 'the same session, restarted');
  } finally {
    w2.cleanup();
  }
});

// ------------------------------------------------------------ the lost ack

test('a crash after the session was made and before its first turn: the turn is sent once', async () => {
  const w = world(one(), {}, { pause: (p) => (p === 'before-prompt' ? never() : Promise.resolve()) });
  await waitFor(() => rowOf(w.h, w.id, 'a')?.state === 'running', 'the node to be running');
  assert.equal(row(w).prompt_id, undefined, 'the crash lands before any turn was sent');
  assert.equal(w.sim.of('PROMPT_ACP').length, 0);
  const w2 = await crash(w);
  try {
    await waitFor(() => w2.sim.of('PROMPT_ACP').length === 1, 'the turn');
    await tick();
    assert.equal(w2.sim.of('PROMPT_ACP').length, 1, 'sent exactly once');
    assert.equal(w2.sim.of('CREATE_SESSION').length, 1);
    finish(w2.sim.sessions[0]);
    await done(w2);
    assert.equal(row(w2).attempt, 1);
  } finally {
    w2.cleanup();
  }
});

test('a lost PROMPT_ACP ack: the turn already in the transcript is adopted, not sent again', async () => {
  const w = world(one(), {}, { pause: (p) => (p === 'after-prompt' ? never() : Promise.resolve()) });
  await waitFor(() => w.sim.of('PROMPT_ACP').length === 1, 'the turn to be sent');
  await tick();
  assert.equal(row(w).prompt_id, undefined, 'the ack was lost: the row never recorded the turn');
  const w2 = await crash(w);
  try {
    await waitFor(() => row(w2).prompt_id !== undefined, 'the adopted turn to be recorded');
    assert.deepEqual([row(w2).prompt_id, row(w2).epoch, row(w2).attempt], [10, 1, 1]);
    finish(w2.sim.sessions[0]);
    await done(w2);
    assert.equal(w2.sim.of('PROMPT_ACP').length, 1, 'exactly one PROMPT_ACP was ever sent');
    assert.equal(row(w2).attempt, 1);
  } finally {
    w2.cleanup();
  }
});

test('a lost ack on the F3 re-prompt: the re-prompt in the transcript is adopted, and a second miss still fails the node', async () => {
  let n = 0;
  const w = world(one(), { onPrompt: (_t, _x, sess) => sess.end() }, { pause: (p) => (p === 'after-prompt' && ++n === 2 ? never() : Promise.resolve()) });
  await waitFor(() => w.sim.of('PROMPT_ACP').length === 2, 'the re-prompt to be sent');
  await tick();
  assert.equal(row(w).prompt_id, 10, 'only the first turn was recorded');
  const w2 = await crash(w);
  try {
    await waitFor(() => runState(w2.h, w2.id) === 'failed', 'the run to fail');
    assert.equal(w2.sim.of('PROMPT_ACP').length, 2, 'no third prompt');
    assert.match(row(w2).error ?? '', /twice without calling submit_result/, 'the adopted turn was known to be the re-prompt');
    assert.equal(w2.sim.sessions[0].users[1].text, REPROMPT);
  } finally {
    w2.cleanup();
  }
});

test('a turn another principal prompted is not the node\'s: the node is interrupted, not given that result', async () => {
  const w = world(one());
  await running(w);
  const sess = w.sim.sessions[0];
  w.h.engine.disconnect();
  sess.begin('user', 'something the user typed'); // the latest turn is not ours
  finish(sess, 'not ours');
  const w2 = await crash(w);
  try {
    await waitFor(() => row(w2).attempt === 2, 'the retry');
    assert.notEqual(row(w2).output_raw && (row(w2).output_raw as { summary: string }).summary, 'not ours');
  } finally {
    w2.cleanup();
  }
});

// ----------------------------------------------------------- hived restart

/** What `hived` does to a session at boot: a new adapter loaded the conversation, so a new epoch whose replayed turns are not ours. */
function revive(sess: SimSession): void {
  sess.reset();
  sess.users = [{ id: 5, origin: 'replayed', text: 'do it' }];
  sess.event('updated');
}

test('hived restart: the revived session is re-prompted as attempt 2, with the interrupted note', async () => {
  const w = world(one());
  await running(w);
  const sess = w.sim.sessions[0];
  w.h.engine.disconnect();
  revive(sess);
  const w2 = await crash(w, 'boot-2');
  try {
    await waitFor(() => w2.sim.prompts.length === 2, 'the retry prompt');
    assert.equal(w2.sim.prompts[1], INTERRUPTED_NOTE + w2.sim.prompts[0]);
    assert.equal(row(w2).attempt, 2);
    assert.equal(row(w2).boot_id, 'boot-2');
    assert.equal(w2.sim.of('CREATE_SESSION').length, 1, 'the session that came back is used');
    assert.equal(w2.sim.of('RESTART_SESSION').length, 0);
    finish(sess, 'second try');
    await done(w2);
    assert.deepEqual(row(w2).output_raw, { summary: 'second try' });
  } finally {
    w2.cleanup();
  }
});

test('hived restart: a session that did not come back is restarted, then re-prompted', async () => {
  const w = world(one());
  await running(w);
  const sess = w.sim.sessions[0];
  w.h.engine.disconnect();
  sess.event('updated', { alive: false, last_error: 'adapter exited' });
  const w2 = await crash(w, 'boot-2');
  try {
    await waitFor(() => w2.sim.prompts.length === 2, 'the retry prompt');
    assert.equal(w2.sim.of('RESTART_SESSION').length, 1);
    assert.equal(w2.sim.of('CREATE_SESSION').length, 1);
    assert.equal(row(w2).session_id, sess.id);
  } finally {
    w2.cleanup();
  }
});

test('hived restart: a transcript that happens to carry the row\'s epoch and prompt id is not its turn: ids restart with the daemon, only the boot id tells', async () => {
  const w = world(one());
  await running(w);
  const sess = w.sim.sessions[0];
  w.h.engine.disconnect();
  finish(sess, 'a different process\'s result'); // epoch 1, prompt 10, ended with a result: it looks exactly like the row's own turn
  const w2 = await crash(w, 'boot-2');
  try {
    await waitFor(() => row(w2).attempt === 2, 'the retry');
    assert.notEqual((row(w2).output_raw as { summary?: string } | undefined)?.summary, 'a different process\'s result');
    assert.equal(w2.sim.prompts.length, 2);
  } finally {
    w2.cleanup();
  }
});

test('plugin crash: a session restarted under the same daemon (a new epoch) is not the row\'s turn, even when the new epoch has a turn with the same id', async () => {
  const w = world(one());
  await running(w);
  const sess = w.sim.sessions[0];
  w.h.engine.disconnect();
  sess.reset(); // restarted: epoch 2
  sess.begin('user', 'typed after the restart'); // prompt id 10 again
  finish(sess, 'typed result');
  const w2 = await crash(w);
  try {
    await waitFor(() => row(w2).attempt === 2, 'the retry');
    assert.notEqual((row(w2).output_raw as { summary?: string } | undefined)?.summary, 'typed result');
  } finally {
    w2.cleanup();
  }
});

test('a lost ack on a retried attempt is adopted from the new epoch: the first attempt\'s ids were cleared when the second started', async () => {
  const w = world(one());
  await running(w);
  w.h.engine.disconnect();
  revive(w.sim.sessions[0]);
  const w2 = await crash(w, 'boot-2', { pause: (p) => (p === 'after-prompt' ? never() : Promise.resolve()) });
  await waitFor(() => w2.sim.prompts.length === 2, 'attempt 2\'s prompt');
  await tick();
  assert.equal(row(w2).attempt, 2);
  assert.equal(row(w2).prompt_id, undefined, 'its ack was lost, and the first attempt\'s turn is not its turn');
  const w3 = await crash(w2, 'boot-2');
  try {
    await waitFor(() => row(w3).prompt_id !== undefined, 'the adopted turn');
    assert.deepEqual([row(w3).attempt, row(w3).epoch, row(w3).state], [2, 2, 'running']);
    finish(w3.sim.sessions[0], 'second');
    await done(w3);
    assert.equal(w3.sim.prompts.length, 2, 'no third prompt');
  } finally {
    w3.cleanup();
  }
});

test('a turn of ours that was not recorded, followed by one that is not ours, is not followed: the node is interrupted', async () => {
  const w = world(one(), {}, { pause: (p) => (p === 'after-prompt' ? never() : Promise.resolve()) });
  await waitFor(() => w.sim.prompts.length === 1, 'the turn');
  await tick();
  const sess = w.sim.sessions[0];
  w.h.engine.disconnect();
  sess.begin('user', 'typed meanwhile');
  finish(sess, 'not ours');
  const w2 = await crash(w);
  try {
    await waitFor(() => row(w2).attempt === 2, 'the retry');
    assert.notEqual((row(w2).output_raw as { summary?: string } | undefined)?.summary, 'not ours');
  } finally {
    w2.cleanup();
  }
});

// The daemon trims the oldest transcript items of a long conversation, never the state fields (`prompt_id`, `stop_reason`, the result).

test('a trimmed transcript: a recorded turn still running is kept at attempt 1, with no second prompt and no kill', async () => {
  const w = world(one());
  await running(w);
  w.h.engine.disconnect();
  w.sim.sessions[0].trim = 1; // its user item is gone; the turn is not
  const w2 = await crash(w);
  try {
    await tick();
    await new Promise((r) => setTimeout(r, 30));
    assert.deepEqual([row(w2).state, row(w2).attempt], ['running', 1]);
    assert.equal(w2.sim.of('PROMPT_ACP').length, 1, 'a healthy long turn is not prompted again (the daemon would refuse it as busy)');
    assert.deepEqual(w2.sim.of('KILL_SESSION'), []);
    finish(w2.sim.sessions[0], 'long turn');
    await done(w2);
    assert.equal(row(w2).attempt, 1);
  } finally {
    w2.cleanup();
  }
});

test('a trimmed transcript: a recorded turn that finished with a result is accepted, not run again', async () => {
  const w = world(one());
  await running(w);
  w.h.engine.disconnect();
  finish(w.sim.sessions[0], 'a finished long turn');
  w.sim.sessions[0].trim = 1;
  const w2 = await crash(w);
  try {
    await done(w2);
    assert.deepEqual(row(w2).output_raw, { summary: 'a finished long turn' });
    assert.equal(w2.sim.of('PROMPT_ACP').length, 1);
    assert.equal(row(w2).attempt, 1);
  } finally {
    w2.cleanup();
  }
});

test('a trimmed transcript: the F3 re-prompt is known from the row, so its trimmed item allows no third prompt', async () => {
  const w = world(one(), { onPrompt: (turn, _t, sess) => (turn === 1 ? sess.end() : undefined) });
  await waitFor(() => w.sim.prompts.length === 2 && row(w).prompt_id === 20, 'the re-prompt to be running and recorded');
  assert.equal(row(w).reprompted, true);
  w.h.engine.disconnect();
  w.sim.sessions[0].trim = 2; // both user items trimmed
  w.sim.sessions[0].end(); // the re-prompt ended without a result
  const w2 = await crash(w);
  try {
    await waitFor(() => runState(w2.h, w2.id) === 'failed', 'the second miss to fail the node');
    assert.match(row(w2).error ?? '', /twice without calling submit_result/);
    assert.equal(w2.sim.prompts.length, 2, 'no third prompt');
    assert.deepEqual(w2.sim.of('KILL_SESSION'), []);
  } finally {
    w2.cleanup();
  }
});

test('a trimmed transcript, lost ack, turn still running: nothing is sent or killed; when the turn ends the node is interrupted and its retry succeeds', async () => {
  const w = world(one(), {}, { pause: (p) => (p === 'after-prompt' ? never() : Promise.resolve()) });
  await waitFor(() => w.sim.prompts.length === 1, 'the turn');
  await tick();
  w.h.engine.disconnect();
  w.sim.sessions[0].trim = 1; // the turn is running and its item is gone: the row cannot tell it from the user's
  const w2 = await crash(w);
  try {
    await new Promise((r) => setTimeout(r, 60));
    assert.deepEqual([row(w2).state, row(w2).attempt], ['running', 1], 'waiting for the session to go idle');
    assert.equal(w2.sim.of('PROMPT_ACP').length, 1);
    assert.deepEqual(w2.sim.of('KILL_SESSION'), []);
    w2.sim.sessions[0].end(); // the turn ends
    await waitFor(() => row(w2).attempt === 2 && w2.sim.prompts.length === 2, 'the retry to be prompted');
    assert.ok(w2.sim.prompts[1].startsWith(INTERRUPTED_NOTE));
    assert.notEqual(row(w2).state, 'failed', 'the retry was not refused as busy');
    assert.deepEqual(w2.sim.of('KILL_SESSION'), [], 'a healthy session is never killed for this');
  } finally {
    w2.cleanup();
  }
});

test('a recorded turn on a session that has no turn at all (same epoch, nothing prompted) is interrupted at once, not waited for', async () => {
  const w = world(one());
  await running(w);
  w.h.engine.disconnect();
  const sess = w.sim.sessions[0];
  sess.prompt_id = 0; // a session that came back with an empty transcript under the same epoch number
  sess.users = [];
  const w2 = await crash(w);
  try {
    await waitFor(() => row(w2).attempt === 2 && w2.sim.prompts.length === 2, 'the retry');
    assert.deepEqual(w2.sim.of('KILL_SESSION'), []);
  } finally {
    w2.cleanup();
  }
});

test('a trimmed transcript, lost ack, session idle: the node is interrupted at once', async () => {
  const w = world(one(), {}, { pause: (p) => (p === 'after-prompt' ? never() : Promise.resolve()) });
  await waitFor(() => w.sim.prompts.length === 1, 'the turn');
  await tick();
  w.h.engine.disconnect();
  finish(w.sim.sessions[0], 'unreadable'); // the turn ended meanwhile
  w.sim.sessions[0].trim = 1;
  const w2 = await crash(w);
  try {
    await waitFor(() => row(w2).attempt === 2 && w2.sim.prompts.length === 2, 'the retry');
    assert.deepEqual(w2.sim.of('KILL_SESSION'), []);
  } finally {
    w2.cleanup();
  }
});

test('a trimmed transcript, lost ack, a turn that outlives the node\'s timeout: it is cancelled, not killed, and the node fails naming the timeout', async () => {
  const slow = workflow('w', s.object({}), (g) => {
    g.agent('a', { agent: 'claude', prompt: 'do it', output: out, timeoutSec: 1 });
  });
  const w = world(slow, {}, { pause: (p) => (p === 'after-prompt' ? never() : Promise.resolve()) });
  await waitFor(() => w.sim.prompts.length === 1, 'the turn');
  await tick();
  w.h.engine.disconnect();
  w.sim.sessions[0].trim = 1;
  const w2 = await crash(w);
  try {
    await waitFor(() => runState(w2.h, w2.id) === 'failed', 'the node to time out');
    assert.match(row(w2).error ?? '', /timed out after 1s/);
    assert.equal(w2.sim.of('CANCEL_ACP').length, 1);
    assert.deepEqual(w2.sim.of('KILL_SESSION'), []);
    assert.equal(w2.sim.prompts.length, 1);
  } finally {
    w2.cleanup();
  }
});

test('hived restart: when the restart fails (no loadSession) the session is killed with its worktree kept and a fresh one is made in the same worktree', async () => {
  const w = world(one(), {
    onRestart: () => {
      throw Object.assign(new Error('this agent cannot load a session'), { hive: { code: 'restart_failed', message: 'this agent cannot load a session' } });
    },
  });
  await running(w);
  const first = w.sim.sessions[0];
  const path = row(w).worktree;
  w.h.engine.disconnect();
  first.event('updated', { alive: false, last_error: 'adapter exited' });
  const w2 = await crash(w, 'boot-2');
  try {
    await waitFor(() => w2.sim.sessions.length === 2 && w2.sim.prompts.length === 2, 'the fresh session and its prompt');
    const names = w2.sim.of('KILL_SESSION').concat(w2.sim.of('RESTART_SESSION')).length;
    assert.equal(names, 2);
    assert.deepEqual(w2.sim.of('KILL_SESSION')[0], { session_id: first.id, force: true });
    const create = w2.sim.of('CREATE_SESSION')[1];
    assert.equal(create.worktree_path, path, 'the fresh session works in the interrupted attempt\'s worktree');
    // The kill removes a worktree that holds nothing: the engine prepared it again before creating, so the path exists.
    const order = w2.hive.requests.map((r) => r.name).filter((x) => x === 'KILL_SESSION' || x === 'PREPARE_WORKTREE' || x === 'CREATE_SESSION');
    assert.deepEqual(order.slice(-3), ['KILL_SESSION', 'PREPARE_WORKTREE', 'CREATE_SESSION']);
    assert.equal(w2.sim.prompts[1], INTERRUPTED_NOTE + w2.sim.prompts[0]);
    assert.equal(row(w2).session_id, w2.sim.sessions[1].id);
    assert.equal(row(w2).attempt, 2);
    finish(w2.sim.sessions[1]);
    await done(w2);
  } finally {
    w2.cleanup();
  }
});

test('hived restart: a session still being revived is waited for, not restarted underneath the daemon', async () => {
  const w = world(one());
  await running(w);
  const sess = w.sim.sessions[0];
  w.h.engine.disconnect();
  sess.reset();
  sess.event('updated', { alive: false, phase: 'reviving' });
  const w2 = await crash(w, 'boot-2');
  try {
    await tick();
    await new Promise((r) => setTimeout(r, 40));
    assert.equal(w2.sim.of('RESTART_SESSION').length, 0, 'the daemon is still reviving it');
    assert.equal(w2.sim.prompts.length, 1);
    revive(sess);
    await waitFor(() => w2.sim.prompts.length === 2, 'the retry prompt');
    assert.equal(w2.sim.of('RESTART_SESSION').length, 0);
  } finally {
    w2.cleanup();
  }
});

test('hived restart: a node interrupted twice fails, naming the count', async () => {
  const w = world(one());
  await running(w);
  w.h.engine.disconnect();
  revive(w.sim.sessions[0]);
  const w2 = await crash(w, 'boot-2');
  await waitFor(() => w2.sim.prompts.length === 2 && row(w2).prompt_id !== undefined, 'attempt 2 to be running');
  w2.h.engine.disconnect();
  revive(w2.sim.sessions[0]);
  const w3 = await crash(w2, 'boot-3');
  try {
    await waitFor(() => runState(w3.h, w3.id) === 'failed', 'the run to fail');
    assert.match(row(w3).error ?? '', /interrupted 2 times/);
    assert.equal(w3.sim.prompts.length, 2, 'no third attempt');
  } finally {
    w3.cleanup();
  }
});

test('hived restart: a result submitted while the plugin was down is lost with the restart, by design: the node is interrupted', async () => {
  const w = world(one());
  await running(w);
  const sess = w.sim.sessions[0];
  w.h.engine.disconnect();
  finish(sess, 'lost'); // submitted while down; hived restarts before the plugin reads it
  revive(sess);
  const w2 = await crash(w, 'boot-2');
  try {
    await waitFor(() => w2.sim.prompts.length === 2, 'attempt 2');
    assert.equal(row(w2).attempt, 2);
    finish(sess, 'second');
    await done(w2);
    assert.deepEqual(row(w2).output_raw, { summary: 'second' });
  } finally {
    w2.cleanup();
  }
});

test('a finished node is never run again across a daemon restart and a plugin crash', async () => {
  const w = world(two(), { onPrompt: (turn, _t, sess) => (turn === 1 ? finish(sess, 'a done') : undefined) });
  await running(w, 'b');
  assert.equal(row(w, 'a').state, 'done');
  w.h.engine.disconnect();
  revive(w.sim.sessions[1]);
  const w2 = await crash(w, 'boot-2'); // hived restarted
  await waitFor(() => w2.sim.prompts.length === 3 && row(w2, 'b').prompt_id !== undefined, 'b attempt 2 to be running');
  const w3 = await crash(w2, 'boot-2'); // then the plugin crashed
  try {
    await tick();
    finish(w3.sim.sessions[1], 'b done');
    await done(w3);
    assert.deepEqual(w3.sim.of('CREATE_SESSION').map((c) => c.name), ['w · a', 'w · b'], 'a was created once; b was reused');
    assert.equal(w3.sim.prompts.filter((p) => p.includes('do a')).length, 1, 'a was prompted once');
    assert.equal(w3.sim.prompts.length, 3, 'b was prompted twice: once, and once after the daemon restart');
    assert.deepEqual([row(w3, 'a').attempt, row(w3, 'a').state, row(w3, 'b').attempt], [1, 'done', 2]);
  } finally {
    w3.cleanup();
  }
});

test('a daemon that does not list its sessions: running nodes are interrupted, nothing is assumed to have survived', async () => {
  const w = world(one());
  await running(w);
  w.h.engine.disconnect();
  w.h.close();
  const hive = new FakeHive();
  w.sim.attach(hive);
  hive.onSend.LIST_SESSIONS = () => {}; // never answers
  const h = harness({ agent: never, check: never }, { dir: w.dir, hive, daemonWaitMs: 30 });
  try {
    await h.engine.recover();
    const r = rowOf(h, w.id, 'a');
    assert.equal(r?.state, 'interrupted', 'no word from the daemon: the turn is not assumed to have survived');
    assert.equal(r?.attempt, 1);
    assert.equal(w.sim.of('PROMPT_ACP').length, 1, 'and nothing was sent');
  } finally {
    h.close();
    rmSync(w.dir, { recursive: true, force: true });
  }
});

test('a connection that is already closed when recovery reads the daemon: running nodes are interrupted, and recovery does not throw', async () => {
  const w = world(one());
  await running(w);
  w.h.engine.disconnect();
  w.h.close();
  const hive = new FakeHive();
  hive.send = () => {
    throw new Error('write after end');
  };
  const h = harness({ agent: never, check: never }, { dir: w.dir, hive });
  try {
    await h.engine.recover();
    assert.equal(rowOf(h, w.id, 'a')?.state, 'interrupted');
  } finally {
    h.close();
    rmSync(w.dir, { recursive: true, force: true });
  }
});

// ------------------------------------------------------------ orphans

/** A CREATE_SESSION the engine never saw the ack of: the daemon made the session, and the engine died waiting. */
function creating(extra: Partial<SimOptions> = {}): World {
  return world(one(), { holdCreate: never(), ...extra });
}

test('orphan adoption: a session made for a node the engine never recorded is taken over, not duplicated', async () => {
  const w = creating();
  await waitFor(() => w.sim.sessions.length === 1, 'the session to be made');
  assert.equal(row(w).state, 'pending', 'the row never saw the ack');
  const w2 = await crash(w);
  try {
    await waitFor(() => w2.sim.prompts.length === 1, 'the turn');
    assert.equal(w2.sim.of('CREATE_SESSION').length, 1, 'no second session in the same worktree');
    assert.equal(row(w2).session_id, w2.sim.sessions[0].id);
    assert.equal(row(w2).attempt, 1);
    assert.equal(row(w2).turns, 1, 'the launch the crash cut short sent nothing, so it charged nothing');
    finish(w2.sim.sessions[0]);
    await done(w2);
  } finally {
    w2.cleanup();
  }
});

test('orphan adoption: two candidates are both removed and the node starts a fresh session, still on its first attempt', async () => {
  const w = creating();
  await waitFor(() => w.sim.sessions.length === 1, 'the session to be made');
  // Another launch of the same node made a second one (a retry that overlapped).
  const extra = w.sim.sessions[0];
  const twin = Object.assign(Object.create(Object.getPrototypeOf(extra)), extra, { id: 'sx' });
  w.sim.sessions.push(twin);
  const w2 = await crash(w);
  try {
    await waitFor(() => w2.sim.prompts.length === 1, 'the turn');
    const killed = w2.sim.of('KILL_SESSION').map((k) => k.session_id).sort();
    assert.deepEqual(killed, [extra.id, 'sx'].sort());
    assert.equal(w2.sim.of('CREATE_SESSION').length, 2, 'a fresh session replaced them');
    assert.equal(row(w2).attempt, 1, 'nothing was ever prompted, so nothing was interrupted');
  } finally {
    w2.cleanup();
  }
});

test('orphan adoption: a session in another worktree is not taken over', async () => {
  const w = creating();
  await waitFor(() => w.sim.sessions.length === 1, 'the session to be made');
  w.sim.sessions[0].worktree_path = '/somewhere/else';
  const w2 = await crash(w);
  try {
    await waitFor(() => w2.sim.prompts.length === 1, 'the turn');
    assert.deepEqual(w2.sim.of('KILL_SESSION').map((k) => k.session_id), [w2.sim.sessions[0].id]);
    assert.equal(row(w2).session_id, w2.sim.sessions[1].id);
  } finally {
    w2.cleanup();
  }
});

test('orphan scan: only this run\'s unreferenced sessions are touched', async () => {
  const w = world(one());
  await running(w);
  const users = Object.assign(Object.create(Object.getPrototypeOf(w.sim.sessions[0])), w.sim.sessions[0], { id: 'user1', name: 'w · a', spawned_by: '' });
  const other = Object.assign(Object.create(Object.getPrototypeOf(w.sim.sessions[0])), w.sim.sessions[0], { id: 'other1', name: 'w · a', spawned_by: 'plugin:workflows/workflow:rzzzzzzzzzzzz' });
  w.sim.sessions.push(users, other);
  const w2 = await crash(w);
  try {
    await tick();
    assert.deepEqual(w2.sim.of('KILL_SESSION'), [], 'the node\'s own session, the user\'s and another run\'s are all left alone');
    assert.equal(row(w2).state, 'running');
  } finally {
    w2.cleanup();
  }
});

test('orphan scan: a stray session of a node that already has its own is removed, and the node keeps its own', async () => {
  const w = world(one());
  await running(w);
  const own = w.sim.sessions[0];
  const stray = Object.assign(Object.create(Object.getPrototypeOf(own)), own, { id: 'stray', name: 'w · a', spawned_by: `plugin:workflows/workflow:${w.id}` });
  w.sim.sessions.push(stray);
  const w2 = await crash(w);
  try {
    await tick();
    assert.deepEqual(w2.sim.of('KILL_SESSION').map((k) => k.session_id), ['stray'], 'the stray is removed: a node never has two sessions in its worktree');
    assert.equal(row(w2).session_id, own.id, 'the node keeps its own');
    assert.equal(row(w2).state, 'running');
    assert.equal(w2.sim.of('PROMPT_ACP').length, 1);
  } finally {
    w2.cleanup();
  }
});

test('orphan scan: a finished run is not active, so its leftover sessions are left to the run-end cleanup (B3d), not recovery', async () => {
  const w = world(one(), { onPrompt: (_t, _x, sess) => finish(sess) });
  await done(w);
  const stray = Object.assign(Object.create(Object.getPrototypeOf(w.sim.sessions[0])), w.sim.sessions[0], { id: 'stray', name: 'w · a', spawned_by: `plugin:workflows/workflow:${w.id}` });
  w.sim.sessions.push(stray);
  // A finished run is not active: its sessions are the run-end cleanup's (B3d), not recovery's.
  const w2 = await crash(w);
  try {
    assert.deepEqual(w2.sim.of('KILL_SESSION'), []);
  } finally {
    w2.cleanup();
  }
});

test('orphan adoption: a session that cannot be removed keeps its node from launching in the same worktree', async () => {
  const w = creating({ killFails: true });
  await waitFor(() => w.sim.sessions.length === 1, 'the session to be made');
  const twin = Object.assign(Object.create(Object.getPrototypeOf(w.sim.sessions[0])), w.sim.sessions[0], { id: 'sx' });
  w.sim.sessions.push(twin);
  const w2 = await crash(w);
  try {
    await waitFor(() => runState(w2.h, w2.id) === 'failed', 'the node to fail');
    assert.match(row(w2).error ?? '', /could not be removed/);
    assert.equal(w2.sim.of('CREATE_SESSION').length, 1, 'no second session beside the leftover');
  } finally {
    w2.cleanup();
  }
});

test('a recovered first turn of a retried attempt still carries the interrupted note', async () => {
  const w = world(one());
  await running(w);
  w.h.engine.disconnect();
  revive(w.sim.sessions[0]);
  // attempt 2 is running its session and has not sent its turn when the plugin crashes again
  const w2 = await crash(w, 'boot-2', { pause: (p) => (p === 'before-prompt' ? never() : Promise.resolve()) });
  await waitFor(() => row(w2).attempt === 2 && row(w2).state === 'running', 'attempt 2 to be running with no turn');
  assert.equal(row(w2).prompt_id, undefined);
  const w3 = await crash(w2, 'boot-2');
  try {
    await waitFor(() => w3.sim.prompts.length === 2, 'the turn');
    assert.ok(w3.sim.prompts[1].startsWith(INTERRUPTED_NOTE), 'sent once, with the note');
    assert.equal(row(w3).attempt, 2);
  } finally {
    w3.cleanup();
  }
});

test('a retried attempt does not adopt an earlier attempt\'s turn of ours that its reused session still holds', async () => {
  const w = world(one());
  await running(w);
  w.h.engine.disconnect();
  finish(w.sim.sessions[0], 'attempt 1\'s result'); // epoch 1, our item 10, ended with a result: the daemon "restarted" with the session unchanged
  const w2 = await crash(w, 'boot-2', { pause: (p) => (p === 'before-prompt' ? never() : Promise.resolve()) });
  await waitFor(() => row(w2).attempt === 2 && row(w2).state === 'running', 'attempt 2 to be running with no turn');
  const w3 = await crash(w2, 'boot-2');
  try {
    await waitFor(() => runState(w3.h, w3.id) === 'failed', 'the node to be interrupted again');
    assert.notEqual(row(w3).state, 'done', 'attempt 1\'s result was not taken as attempt 2\'s');
    assert.equal(w3.sim.prompts.length, 1);
  } finally {
    w3.cleanup();
  }
});

test('an idle waiter belongs to its attempt: a cancelled run is not cancelled or failed again when its timer fires', async () => {
  const slow = workflow('w', s.object({}), (g) => {
    g.agent('a', { agent: 'claude', prompt: 'do it', output: out, timeoutSec: 1 });
  });
  const w = world(slow, {}, { pause: (p) => (p === 'after-prompt' ? never() : Promise.resolve()) });
  await waitFor(() => w.sim.prompts.length === 1, 'the turn');
  await tick();
  w.h.engine.disconnect();
  w.sim.sessions[0].trim = 1;
  const w2 = await crash(w);
  try {
    await tick();
    w2.h.engine.cancelRun(w2.id);
    await new Promise((r) => setTimeout(r, 1300));
    assert.equal(runState(w2.h, w2.id), 'cancelled');
    assert.deepEqual(w2.sim.of('CANCEL_ACP'), [], 'the waiter did nothing for a node that had left its attempt');
  } finally {
    w2.cleanup();
  }
});

test('a launch queued behind a stale one does not start when the connection is lost meanwhile', async () => {
  let release: () => void = () => {};
  const w = world(one(), { holdCreate: new Promise<void>((r) => (release = r)) });
  await waitFor(() => w.sim.sessions.length === 1, 'the first launch to be creating');
  const asked = w.sim.of('LIST_ACP_AGENTS').length;
  w.h.engine.cancelRun(w.id);
  w.h.engine.retryRun(w.id); // the retry queues behind the first launch
  await tick();
  w.h.engine.disconnect(); // and the connection is lost while it waits
  release();
  await new Promise((r) => setTimeout(r, 60));
  try {
    assert.equal(w.sim.of('LIST_ACP_AGENTS').length, asked, 'the queued launch asked the daemon nothing: it belongs to a lost connection');
    assert.equal(row(w).turns, 0);
  } finally {
    w.cleanup();
  }
});

// --------------------------------------------------------------- reconnect

test('reconnect: nodes whose turn is running are picked up on the new connection, and nothing fails', async () => {
  const w = world(one());
  await running(w);
  w.h.engine.disconnect();
  const hive2 = new FakeHive();
  hive2.welcome = { boot_id: 'boot-1' };
  w.sim.attach(hive2);
  await w.h.engine.reconnect(hive2);
  try {
    assert.equal(row(w).state, 'running');
    assert.equal(w.sim.of('PROMPT_ACP').length, 1, 'no second turn');
    finish(w.sim.sessions[0]);
    await done(w);
    assert.equal(row(w).attempt, 1);
  } finally {
    w.cleanup();
  }
});

test('reconnect: a launch whose request was rejected by the lost connection does not fail its node; the session it made is adopted', async () => {
  let lose: (e: Error) => void = () => {};
  const w = world(one(), { holdCreate: new Promise<void>((_, rej) => (lose = rej)) });
  await waitFor(() => w.sim.sessions.length === 1, 'the session to be made');
  w.h.engine.disconnect();
  lose(new Error('hive: connection closed')); // what the SDK does to every pending request
  await tick();
  await tick();
  assert.equal(row(w).state, 'pending', 'the rejected CREATE_SESSION is not the node\'s failure');
  const hive2 = new FakeHive();
  hive2.welcome = { boot_id: 'boot-1' };
  w.sim.attach(hive2);
  await w.h.engine.reconnect(hive2);
  try {
    await waitFor(() => w.sim.prompts.length === 1, 'the turn on the new connection');
    assert.equal(w.sim.of('CREATE_SESSION').length, 1, 'the session the lost launch made was adopted');
    finish(w.sim.sessions[0]);
    await done(w);
  } finally {
    w.cleanup();
  }
});

test('reconnect to a restarted daemon: the node is interrupted and retried, like any restart', async () => {
  const w = world(one());
  await running(w);
  w.h.engine.disconnect();
  revive(w.sim.sessions[0]);
  const hive2 = new FakeHive();
  hive2.welcome = { boot_id: 'boot-9' };
  w.sim.attach(hive2);
  await w.h.engine.reconnect(hive2);
  try {
    await waitFor(() => w.sim.prompts.length === 2, 'the retry');
    assert.equal(row(w).attempt, 2);
    assert.equal(row(w).boot_id, 'boot-9');
  } finally {
    w.cleanup();
  }
});

test('while disconnected nothing new is launched, and a run.cancel still commits', async () => {
  const w = world(two());
  await running(w, 'a');
  w.h.engine.disconnect();
  // a finishes while the connection is down, which makes b due.
  w.h.engine.completeNode(w.id, 'a', { summary: 'x' });
  await tick();
  await tick();
  try {
    assert.equal(w.sim.of('CREATE_SESSION').length, 1, 'b is not launched until the connection is back and the nodes have been read');
    assert.equal(row(w, 'b').state, 'pending');
    w.h.engine.cancelRun(w.id);
    assert.equal(runState(w.h, w.id), 'cancelled');
    assert.equal(w.h.engine.disconnected, true);
  } finally {
    w.cleanup();
  }
});

test('a run.cancel while the connection is down reaches no stop of a launch the connection outlived', async () => {
  const w = world(one());
  await running(w);
  const asked = w.hive.requests.length;
  w.h.engine.disconnect();
  w.h.engine.cancelRun(w.id); // rt.stops still holds the stale launch's stop
  await tick();
  await tick();
  try {
    assert.equal(runState(w.h, w.id), 'cancelled');
    const sent = w.hive.requests.slice(asked).map((r) => r.name);
    assert.deepEqual(sent, [], 'a stale launch speaks for no one: nothing is cancelled or killed through the lost connection');
  } finally {
    w.cleanup();
  }
});

test('a launch that settles late does not release the claim of the launch that replaced it', async () => {
  const gates: (() => void)[] = [];
  const calls: string[] = [];
  const agent = async (ctx: { key: string }): Promise<void> => {
    calls.push(ctx.key);
    await new Promise<void>((r) => gates.push(r));
  };
  const hive = new FakeHive();
  const dir = mkdtempSync(join(tmpdir(), 'wf-dur-'));
  const h = harness({ agent: agent as never, check: never }, { dir, hive, daemonWaitMs: 30 });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'w.ts', ir: one(), input: {} });
    assert.deepEqual(calls, ['a']); // the first launch
    h.engine.disconnect();
    const hive2 = new FakeHive();
    hive2.welcome = { boot_id: 'boot-1' };
    await h.engine.reconnect(hive2); // its replacement is queued behind it
    gates[0](); // the first launch settles late
    await tick();
    await tick();
    assert.deepEqual(calls, ['a', 'a'], 'the replacement runs once the first has settled');
    h.engine.reserveTurn(id, 'a'); // anything that drives the run
    await tick();
    gates[1](); // the replacement settles; a launch the scheduler wrongly admitted beside it would run now
    await new Promise((r) => setTimeout(r, 20));
    assert.deepEqual(calls.slice(0, 2), ['a', 'a']);
    assert.equal(calls.length, 2, 'the node was not launched a third time');
  } finally {
    h.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

// ---------------------------------------------------- stale launch ordering

test('a stale launch\'s kill lands before the retried attempt re-attaches the worktree', async () => {
  let release: () => void = () => {};
  const w = world(one(), { holdCreate: new Promise<void>((r) => (release = r)) });
  await waitFor(() => w.sim.sessions.length === 1, 'the stale launch to be creating');
  w.h.engine.cancelRun(w.id);
  w.h.engine.retryRun(w.id);
  await new Promise((r) => setTimeout(r, 30));
  const preparedBefore = w.hive.requests.filter((r) => r.name === 'PREPARE_WORKTREE').length;
  assert.equal(preparedBefore, 1, 'the retry has not re-attached the worktree while the stale launch is in flight');
  release();
  try {
    await waitFor(() => w.sim.sessions.length === 2, 'the retried attempt to create its session');
    const names = w.hive.requests.map((r) => r.name);
    const kill = names.indexOf('KILL_SESSION');
    const prepares = names.map((n, i) => (n === 'PREPARE_WORKTREE' ? i : -1)).filter((i) => i >= 0);
    assert.ok(kill >= 0, 'the stale launch killed its session');
    assert.ok(prepares[1] > kill, `the retry's PREPARE (${prepares[1]}) must come after the stale kill (${kill}): a kill that lands after it disposes the re-attached worktree`);
  } finally {
    w.cleanup();
  }
});

// ------------------------------------------- a run cancelled and retried while it is read

const par = () =>
  workflow('w', s.object({}), (g) => {
    g.agent('a', { agent: 'claude', prompt: 'do a', output: out });
    g.agent('b', { agent: 'claude', prompt: 'do b', output: out });
  });

test('a run cancelled and retried while recovery reads a node is not failed by that read, and its old sessions are not adopted', async () => {
  const hive = new FakeHive();
  const dir = mkdtempSync(join(tmpdir(), 'wf-dur-'));
  const sim = simulate(hive, { onPrompt: () => {} });
  const deps = { cancelGraceMs: 40, readyTimeoutMs: 400, settlePollMs: 5 };
  const h = harness({ agent: agentExecutor(deps), check: never }, { dir, hive });
  const id = h.engine.startRunFromIR({ project_id: 'p1', file: 'w.ts', ir: par(), input: {} });
  await waitFor(() => ['a', 'b'].every((k) => rowOf(h, id, k)?.prompt_id !== undefined), 'both nodes running');
  const sb = rowOf(h, id, 'b')?.session_id as string;
  h.engine.disconnect();
  h.close();
  const promptsBefore = sim.of('PROMPT_ACP').length; // the original run's own turns

  const hive2 = new FakeHive();
  hive2.welcome = { boot_id: 'boot-1' };
  sim.attach(hive2);
  let release: () => void = () => {};
  let held = false;
  const answer = hive2.onSend.GET_ACP_TRANSCRIPT;
  hive2.onSend.GET_ACP_TRANSCRIPT = (p) => {
    if (p.session_id === sb) {
      held = true; // b's read is slow
      release = () => answer(p);
    } else answer(p);
  };
  const h2 = harness({ agent: agentExecutor(deps), check: never }, { dir, hive: hive2 });
  try {
    const rec = h2.engine.recover();
    await waitFor(() => held, 'b\'s read to be sent');
    await tick();
    h2.engine.cancelRun(id); // the user cancels and retries while the read is out
    await waitFor(() => runState(h2, id) === 'cancelled', 'the cancel');
    h2.engine.retryRun(id);
    release();
    await rec;
    await new Promise((r) => setTimeout(r, 300));
    assert.notEqual(runState(h2, id), 'failed', `the retried run was failed by a read of the old one: ${rowOf(h2, id, 'b')?.error ?? ''}`);
    assert.notEqual(rowOf(h2, id, 'b')?.session_id, sb, 'the retried node did not take over the old node\'s session');
    assert.deepEqual(
      sim.of('PROMPT_ACP').slice(promptsBefore).map((p) => p.session_id).filter((sid) => sid === sb),
      [],
      'nothing prompted the old session, whose turn is still running',
    );
    assert.deepEqual(sim.of('KILL_SESSION').filter((k) => k.session_id === sb), [], 'and nothing killed it: that turn is B3d\'s reconciliation of a run cancelled while the engine was away');
  } finally {
    h2.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('the orphan removal does not try again once the connection is lost', async () => {
  let cut: () => void = () => {};
  const w = world(one(), { holdCreate: never(), killFails: true, onKill: () => cut() });
  await waitFor(() => w.sim.sessions.length === 1, 'the session to be made');
  const twin = Object.assign(Object.create(Object.getPrototypeOf(w.sim.sessions[0])), w.sim.sessions[0], { id: 'sx' });
  w.sim.sessions.push(twin);
  w.h.engine.disconnect();
  w.h.close();
  const hive = new FakeHive();
  hive.welcome = { boot_id: 'boot-1' };
  w.sim.attach(hive);
  const h = harness({ agent: never, check: never }, { dir: w.dir, hive });
  let first = true;
  cut = () => {
    if (first) h.engine.disconnect(); // the first removal fails and the connection goes with it
    first = false;
  };
  try {
    await h.engine.recover();
    assert.equal(w.sim.of('KILL_SESSION').length, 1, 'one try, then the pass gave up: its second try would have gone to a connection it does not belong to');
  } finally {
    h.close();
    rmSync(w.dir, { recursive: true, force: true });
  }
});

test('a session set aside for a node is not handed to the node of a run that was cancelled and retried since', async () => {
  const w = world(one());
  await running(w);
  try {
    w.h.engine.adoptOrphan(w.id, 'a', 'sx');
    w.h.engine.cancelRun(w.id);
    w.h.engine.retryRun(w.id); // a new in-memory run
    assert.equal(w.h.engine.takeOrphan(w.id, 'a'), undefined, 'the old run\'s session is not the new run\'s');
    w.h.engine.adoptOrphan(w.id, 'a', 'sy');
    assert.equal(w.h.engine.takeOrphan(w.id, 'a'), 'sy', 'one set aside for this run is handed over, once');
    assert.equal(w.h.engine.takeOrphan(w.id, 'a'), undefined);
  } finally {
    w.cleanup();
  }
});

test('the orphan removal checks the run before each session, not only before each group', async () => {
  let swap: () => void = () => {};
  const w = world(one(), { holdCreate: never(), onKill: () => swap() });
  await waitFor(() => w.sim.sessions.length === 1, 'the session to be made');
  const twin = Object.assign(Object.create(Object.getPrototypeOf(w.sim.sessions[0])), w.sim.sessions[0], { id: 'sx' });
  w.sim.sessions.push(twin);
  w.h.engine.disconnect();
  w.h.close();
  const hive = new FakeHive();
  hive.welcome = { boot_id: 'boot-1' };
  w.sim.attach(hive);
  const h = harness({ agent: never, check: never }, { dir: w.dir, hive });
  let swapped = false;
  swap = () => {
    if (swapped) return;
    swapped = true;
    h.engine.cancelRun(w.id); // the user cancels and retries while the first removal is out
    h.engine.retryRun(w.id);
  };
  try {
    await h.engine.recover();
    assert.equal(w.sim.of('KILL_SESSION').length, 1, 'the second session belongs to the run that was replaced: this pass leaves it');
  } finally {
    h.close();
    rmSync(w.dir, { recursive: true, force: true });
  }
});
