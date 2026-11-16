// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Agent nodes against a scripted daemon (spec 529, B3b): the executor, its
// turns, F3, permissions, timeout and cancel. The real daemon and fake adapter
// are exercised by internal/workflowe2e.

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { agentExecutor } from '../engine/agent.ts';
import { REPROMPT } from '../engine/agent.ts';
import type { RunEvent } from '../engine/events.ts';
import { workflow } from '../sdk/dsl.ts';
import { s } from '../sdk/schema.ts';
import { CLAUDE, type SimSession, simulate } from './daemon-sim.ts';
import { call, FakeHive, type Harness, harness, rowOf, runState, tick, waitFor } from './harness.ts';

const out = s.object({ summary: s.string() });
const never = (): Promise<void> => new Promise(() => {});
const ok = (summary = 'done') => ({ status: 'ok', summary, data: { summary } });

const one = (extra: Record<string, unknown> = {}) =>
  workflow('w', s.object({}), (g) => {
    g.agent('a', { agent: 'claude', prompt: 'do it', output: out, ...extra });
  });

function rig(onPrompt: (turn: number, text: string, sess: SimSession) => void, o: { ackFirst?: boolean; grace?: number; neverReady?: boolean; agents?: unknown[] } = {}) {
  const hive = new FakeHive();
  const sim = simulate(hive, { onPrompt, ...(o.ackFirst === undefined ? {} : { ackFirst: o.ackFirst }), ...(o.neverReady ? { neverReady: true } : {}), ...(o.agents ? { agents: o.agents } : {}) });
  const h = harness({ agent: agentExecutor({ cancelGraceMs: o.grace ?? 40, readyTimeoutMs: 400 }), check: never }, { hive });
  const start = (ir: ReturnType<typeof one>): string => h.engine.startRunFromIR({ project_id: 'p1', file: 'w.ts', ir, input: {} });
  return { hive, sim, h, start };
}

const events = (h: Harness, id: string, key = 'a'): RunEvent[] => h.store.events(id, 0, 500).filter((e) => e.key === key);
const types = (h: Harness, id: string, key = 'a'): string[] => events(h, id, key).map((e) => e.type);

test('an agent node creates a session with an explicit least permissive mode and no opening prompt, prompts it, commits, then finishes', async () => {
  const r = rig((_, __, sess) => {
    sess.submit(ok('fixed it'));
    sess.end();
  });
  try {
    const id = r.start(one());
    await waitFor(() => runState(r.h, id) === 'done', 'the run');
    const create = r.sim.of('CREATE_SESSION')[0];
    assert.equal(create.acp_mode, 'plan', 'Modes[0]: an empty mode would mean the ceiling');
    assert.equal(create.kind, 'acp');
    assert.equal(create.agent, 'claude');
    assert.equal(create.workflow_run_id, id);
    assert.equal(create.project_id, 'p1');
    assert.equal('initial_prompt' in create, false, 'a failed opening prompt is only logged by the daemon');
    assert.deepEqual(create.result_schema, out.json);
    assert.equal(create.worktree_path, `/wt/a-0`);
    assert.equal(r.sim.of('PROMPT_ACP')[0].workflow_run_id, id);
    const row = rowOf(r.h, id, 'a');
    assert.deepEqual(row?.output_raw, { summary: 'fixed it' });
    assert.equal(row?.session_id, 's1');
    assert.equal(row?.branch, `hive/wf/${id}/a`);
    assert.equal(row?.worktree, '/wt/a-0');
    assert.deepEqual([row?.prompt_id, row?.epoch, row?.boot_id], [10, 1, 'boot-1']);
    assert.equal(row?.turns, 1);
    const commit = r.sim.of('COMMIT_WORKTREE')[0];
    assert.deepEqual(commit, { project_id: 'p1', workflow_run_id: id, branch: `hive/wf/${id}/a`, message: 'wf: a (attempt 1)' });
    // The commit is requested before the node is done, and node.output and node.finished are one transaction.
    const order = r.hive.requests.map((q) => q.name);
    assert.ok(order.indexOf('COMMIT_WORKTREE') > order.indexOf('PROMPT_ACP'));
    assert.deepEqual(types(r.h, id).slice(-2), ['node.output', 'node.finished']);
  } finally {
    r.h.close();
  }
});

test('a turn that ends before its ack is read still finishes the node', async () => {
  const r = rig(
    (_, __, sess) => {
      sess.submit(ok());
      sess.end();
    },
    { ackFirst: false },
  );
  try {
    const id = r.start(one());
    await waitFor(() => runState(r.h, id) === 'done', 'the run');
  } finally {
    r.h.close();
  }
});

test('a frame for another prompt or epoch is not this turn', async () => {
  const r = rig((_, __, sess) => {
    sess.stop_reason = 'end_turn';
    sess.result_status = 'submitted';
    sess.result = ok('stale');
    sess.prompt_id -= 5; // a late message of an earlier prompt
    sess.send();
    sess.prompt_id += 5;
    sess.epoch = 9; // and one of another epoch
    sess.send();
    sess.epoch = 1;
  });
  try {
    const id = r.start(one());
    await waitFor(() => r.h.engine.feed.transcript('s1') !== undefined, 'the frames');
    await new Promise((res) => setTimeout(res, 30));
    assert.equal(rowOf(r.h, id, 'a')?.state, 'running');
  } finally {
    r.h.close();
  }
});

test('F3: a turn without a result gets one re-prompt, and a second miss fails the node', async () => {
  const r = rig((_, __, sess) => sess.end());
  try {
    const id = r.start(one());
    await waitFor(() => runState(r.h, id) === 'failed', 'the run to fail');
    assert.deepEqual(r.sim.prompts, ['do it', REPROMPT]);
    assert.match(rowOf(r.h, id, 'a')?.error ?? '', /twice without calling submit_result/);
    assert.equal(rowOf(r.h, id, 'a')?.turns, 2, 'the re-prompt counted against the cap');
  } finally {
    r.h.close();
  }
});

test('F3: the re-prompt can succeed', async () => {
  const r = rig((turn, _, sess) => {
    if (turn === 2) sess.submit(ok('second try'));
    sess.end();
  });
  try {
    const id = r.start(one());
    await waitFor(() => runState(r.h, id) === 'done', 'the run');
    assert.deepEqual(rowOf(r.h, id, 'a')?.output_raw, { summary: 'second try' });
    assert.equal(r.sim.of('COMMIT_WORKTREE').length, 1, 'only the turn with a result is committed');
  } finally {
    r.h.close();
  }
});

test('the re-prompt is a turn like any other: past the 50-turn cap it fails the run with cap agent_turns', async () => {
  const r = rig((_, __, sess) => sess.end());
  try {
    const ir = workflow('w', s.object({}), (g) => {
      const q = g.human('gate', { prompt: 'go?', output: s.object({ yes: s.boolean() }) });
      const a = g.agent('a', { agent: 'claude', prompt: 'do it', output: out });
      g.edge(q, a);
    });
    const id = r.start(ir as ReturnType<typeof one>);
    await waitFor(() => rowOf(r.h, id, 'gate')?.state === 'waiting', 'the human gate');
    for (let i = 0; i < 49; i++) r.h.engine.commit(id, { t: 'turn', key: 'a' });
    call(r.h, 'human.answer', { run_id: id, key: 'gate', answer: { yes: true } });
    await waitFor(() => runState(r.h, id) === 'failed', 'the run to fail');
    const a = rowOf(r.h, id, 'a');
    assert.equal(a?.cap, 'agent_turns');
    assert.deepEqual(r.sim.prompts, ['do it'], 'the re-prompt that would be the 51st was never sent');
  } finally {
    r.h.close();
  }
});

test('a result with status error fails the node and its summary is redacted', async () => {
  const r = rig((_, __, sess) => {
    sess.submit({ status: 'error', summary: 'no access, token ghp_abcdefghijklmnopqrstuvwxyz0123456789' });
    sess.end();
  });
  try {
    const id = r.start(one());
    await waitFor(() => runState(r.h, id) === 'failed', 'the run to fail');
    const err = rowOf(r.h, id, 'a')?.error ?? '';
    assert.match(err, /could not do the task: no access/);
    assert.ok(!err.includes('ghp_abcdef'), 'redacted before it is stored');
    assert.equal(r.sim.of('COMMIT_WORKTREE').length, 0, 'a failed task commits nothing');
  } finally {
    r.h.close();
  }
});

test('a stop reason other than end_turn fails the node, naming it', async () => {
  const r = rig((_, __, sess) => sess.end('max_tokens'));
  try {
    const id = r.start(one());
    await waitFor(() => runState(r.h, id) === 'failed', 'the run to fail');
    assert.match(rowOf(r.h, id, 'a')?.error ?? '', /stop reason "max_tokens"/);
  } finally {
    r.h.close();
  }
});

test('a commit that fails fails the node with the reason', async () => {
  const r = rig((_, __, sess) => {
    sess.submit(ok());
    sess.end();
  });
  r.hive.replies.COMMIT_WORKTREE = () => {
    throw new Error('git_identity_missing: set user.name');
  };
  try {
    const id = r.start(one());
    await waitFor(() => runState(r.h, id) === 'failed', 'the run to fail');
    assert.match(rowOf(r.h, id, 'a')?.error ?? '', /could not commit the agent's work: git_identity_missing/);
  } finally {
    r.h.close();
  }
});

// ------------------------------------------------------------- permissions

const perm = (request_id: string, ids = ['allow', 'reject']) => ({ request_id, title: 'Read file', options: ids.map((option_id) => ({ option_id, name: option_id, kind: 'allow_once' })) });

test('a permission request becomes node.waiting once, however many frames repeat it, and node.resumed precedes node.finished', async () => {
  let sess: SimSession | undefined;
  const r = rig((_, __, s0) => {
    sess = s0;
    s0.ask(perm('req-1'));
    s0.send(); // every message carries the pending request
    s0.send();
  });
  try {
    const id = r.start(one());
    await waitFor(() => rowOf(r.h, id, 'a')?.state === 'waiting', 'the node to wait');
    await tick();
    const w = events(r.h, id).filter((e) => e.type === 'node.waiting');
    assert.equal(w.length, 1);
    assert.equal(w[0].data.reason, 'permission');
    assert.equal(w[0].data.session_id, 's1');
    assert.equal(w[0].data.request_id, 'req-1');
    assert.deepEqual((w[0].data.options as { option_id: string }[]).map((o) => o.option_id), ['allow', 'reject']);
    assert.equal(runState(r.h, id), 'waiting');
    const node = call(r.h, 'node.get', { run_id: id, key: 'a' }) as { waiting?: { request_id: string } };
    assert.equal(node.waiting?.request_id, 'req-1');

    (sess as SimSession).answer();
    await waitFor(() => rowOf(r.h, id, 'a')?.state === 'running', 'the node to resume');
    (sess as SimSession).submit(ok());
    (sess as SimSession).end();
    await waitFor(() => runState(r.h, id) === 'done', 'the run');
    const t = types(r.h, id);
    assert.ok(t.indexOf('node.waiting') < t.indexOf('node.resumed') && t.indexOf('node.resumed') < t.indexOf('node.finished'), t.join(','));
    assert.deepEqual(events(r.h, id).find((e) => e.type === 'node.resumed')?.data, { reason: 'permission' });
  } finally {
    r.h.close();
  }
});

test('a new permission request replaces the one the node was waiting on', async () => {
  let sess: SimSession | undefined;
  const r = rig((_, __, s0) => {
    sess = s0;
    s0.ask(perm('req-1'));
  });
  try {
    const id = r.start(one());
    await waitFor(() => rowOf(r.h, id, 'a')?.waiting?.request_id === 'req-1', 'the first request');
    (sess as SimSession).ask(perm('req-2'));
    await waitFor(() => rowOf(r.h, id, 'a')?.waiting?.request_id === 'req-2', 'the second request');
    assert.deepEqual(types(r.h, id).filter((t) => t === 'node.waiting' || t === 'node.resumed'), ['node.waiting', 'node.resumed', 'node.waiting']);
  } finally {
    r.h.close();
  }
});

test('a turn that ends in the same frame that clears its permission resumes before it finishes', async () => {
  const r = rig((_, __, sess) => {
    sess.ask(perm('req-1'));
    setImmediate(() => {
      sess.submit(ok());
      sess.end(); // one frame: no permission, a stop reason
    });
  });
  try {
    const id = r.start(one());
    await waitFor(() => runState(r.h, id) === 'done', 'the run');
    const t = types(r.h, id);
    assert.ok(t.includes('node.resumed') && t.indexOf('node.resumed') < t.indexOf('node.finished'));
  } finally {
    r.h.close();
  }
});

test('a permission id the GUI could not send back unchanged fails the node (B-18) and the agent is cancelled', async () => {
  const r = rig((_, __, sess) => sess.ask(perm('req-1', ['allow', 'not valid!'])));
  try {
    const id = r.start(one());
    await waitFor(() => runState(r.h, id) === 'failed', 'the run to fail');
    assert.equal(rowOf(r.h, id, 'a')?.cap, 'permission_option_invalid');
    assert.ok(!types(r.h, id).includes('node.waiting'), 'no wait was opened');
    await waitFor(() => r.sim.of('CANCEL_ACP').length === 1, 'the agent to be cancelled');
    assert.deepEqual(r.sim.of('CANCEL_ACP')[0], { session_id: 's1', prompt_id: 10, epoch: 1 }, 'pinned to our turn');
  } finally {
    r.h.close();
  }
});

// ------------------------------------------------------- timeout and cancel

test('a timeout cancels the turn, pinned to it, and fails the node naming the timeout', async () => {
  const r = rig(() => {});
  r.hive.replies.CANCEL_ACP = () => {
    setImmediate(() => {
      const sess = r.sim.sessions[0];
      sess.end('cancelled');
    });
    return { was_busy: true };
  };
  try {
    const id = r.start(one({ timeoutSec: 0.15 }));
    await waitFor(() => runState(r.h, id) === 'failed', 'the run to fail');
    assert.match(rowOf(r.h, id, 'a')?.error ?? '', /timed out after 0\.15s/);
    await waitFor(() => r.sim.of('CANCEL_ACP').length === 1, 'the cancel');
    assert.deepEqual(r.sim.of('CANCEL_ACP')[0], { session_id: 's1', prompt_id: 10, epoch: 1 });
    await new Promise((res) => setTimeout(res, 80));
    assert.equal(r.sim.of('KILL_SESSION').length, 0, 'the turn reported cancelled in time: no kill');
  } finally {
    r.h.close();
  }
});

test('a cancel that the agent ignores is followed by KILL_SESSION after the grace, forced, and no worktree is removed', async () => {
  const r = rig(() => {}, { grace: 60 });
  try {
    const id = r.start(one());
    await waitFor(() => rowOf(r.h, id, 'a')?.state === 'running' && r.sim.prompts.length === 1, 'the turn');
    r.h.engine.cancelRun(id);
    await waitFor(() => r.sim.of('KILL_SESSION').length === 1, 'the kill');
    assert.deepEqual(r.sim.of('KILL_SESSION')[0], { session_id: 's1', force: true }, 'force so a dirty worktree cannot keep the session alive; no remove_worktree so dirty work is kept');
    assert.equal(r.sim.of('REMOVE_WORKTREE').length, 0);
    assert.deepEqual(r.sim.of('CANCEL_ACP')[0], { session_id: 's1', prompt_id: 10, epoch: 1 });
  } finally {
    r.h.close();
  }
});

test('a cancel the agent honours is not followed by a kill', async () => {
  const r = rig(() => {}, { grace: 200 });
  r.hive.replies.CANCEL_ACP = () => {
    setImmediate(() => r.sim.sessions[0].end('cancelled'));
    return { was_busy: true };
  };
  try {
    const id = r.start(one());
    await waitFor(() => r.sim.prompts.length === 1, 'the turn');
    r.h.engine.cancelRun(id);
    await waitFor(() => r.sim.of('CANCEL_ACP').length === 1, 'the cancel');
    await new Promise((res) => setTimeout(res, 300));
    assert.equal(r.sim.of('KILL_SESSION').length, 0);
  } finally {
    r.h.close();
  }
});

test('a run cancelled while the session is being created kills the new session instead of leaking it', async () => {
  const r = rig(() => {});
  const make = r.hive.replies.CREATE_SESSION;
  let id = '';
  r.hive.replies.CREATE_SESSION = (p) => {
    r.h.engine.cancelRun(id);
    return make(p);
  };
  try {
    id = r.start(one());
    await waitFor(() => r.sim.of('KILL_SESSION').length === 1, 'the kill');
    assert.deepEqual(r.sim.of('KILL_SESSION')[0], { session_id: 's1', force: true });
    assert.equal(r.sim.prompts.length, 0, 'no turn was started');
  } finally {
    r.h.close();
  }
});

test('F2: while the user has taken the session over, the node waits and its clock stops', async () => {
  const r = rig(() => {});
  r.hive.replies.CANCEL_ACP = () => {
    setImmediate(() => r.sim.sessions[0].end('cancelled'));
    return { was_busy: true };
  };
  try {
    const id = r.start(one({ timeoutSec: 0.25 }));
    await waitFor(() => r.sim.prompts.length === 1, 'the turn');
    r.sim.sessions[0].event('updated', { kind: '' }); // a terminal session has no kind
    await new Promise((res) => setTimeout(res, 400));
    assert.equal(rowOf(r.h, id, 'a')?.state, 'running', 'the timeout did not fire during the takeover');
    r.sim.sessions[0].event('updated');
    await waitFor(() => runState(r.h, id) === 'failed', 'the timeout to run out after the hand-back');
    assert.match(rowOf(r.h, id, 'a')?.error ?? '', /timed out/);
  } finally {
    r.h.close();
  }
});

// ------------------------------------------------------------ session start

test('a session that reports an error while starting fails the node with it', async () => {
  const r = rig(() => {}, { neverReady: true });
  try {
    const id = r.start(one());
    await waitFor(() => r.sim.sessions.length === 1, 'the session');
    r.sim.sessions[0].event('updated', { last_error: 'adapter exited: no such file' });
    await waitFor(() => runState(r.h, id) === 'failed', 'the run to fail');
    assert.match(rowOf(r.h, id, 'a')?.error ?? '', /failed to start: adapter exited/);
    assert.equal(r.sim.prompts.length, 0);
  } finally {
    r.h.close();
  }
});

test('an ERROR frame that arrives after the CREATE_SESSION ack fails the node', async () => {
  const r = rig(() => {}, { neverReady: true });
  try {
    const id = r.start(one());
    await waitFor(() => r.sim.sessions.length === 1, 'the session');
    r.hive.emit('ERROR', { code: 'worktree_failed', message: 'could not make the worktree', session_id: 's1', req_id: 1 });
    await waitFor(() => runState(r.h, id) === 'failed', 'the run to fail');
    assert.match(rowOf(r.h, id, 'a')?.error ?? '', /could not make the worktree/);
  } finally {
    r.h.close();
  }
});

test('a session that is never ready fails the node after the ready timeout', async () => {
  const r = rig(() => {}, { neverReady: true });
  try {
    const id = r.start(one());
    await waitFor(() => runState(r.h, id) === 'failed', 'the run to fail', 3000);
    assert.match(rowOf(r.h, id, 'a')?.error ?? '', /not ready after/);
  } finally {
    r.h.close();
  }
});

test('a session the daemon refuses to create fails the node with the daemon\'s reason', async () => {
  const hive = new FakeHive();
  simulate(hive, { onPrompt: () => {} });
  hive.replies.CREATE_SESSION = () => {
    throw new Error('acp_mode_above_ceiling: mode "acceptEdits" is above your "default" ceiling');
  };
  const h = harness({ agent: agentExecutor(), check: never }, { hive });
  try {
    const id = h.engine.startRunFromIR({ project_id: 'p', file: 'w.ts', ir: one(), input: {} });
    await waitFor(() => runState(h, id) === 'failed', 'the run to fail');
    assert.match(rowOf(h, id, 'a')?.error ?? '', /above your "default" ceiling/);
  } finally {
    h.close();
  }
});

// ------------------------------------------------------------------- modes

test('a named mode is sent as named; an omitted one as the adapter\'s least permissive', async () => {
  const r = rig((_, __, sess) => {
    sess.submit(ok());
    sess.end();
  });
  try {
    const id = r.start(one({ mode: 'default' }));
    await waitFor(() => runState(r.h, id) === 'done', 'the run');
    assert.equal(r.sim.of('CREATE_SESSION')[0].acp_mode, 'default');
  } finally {
    r.h.close();
  }
});

test('a mode above the ceiling the user lowered since the run started fails the node naming the ceiling, and nothing is created', async () => {
  const r = rig(() => {});
  try {
    const id = r.start(one({ mode: 'acceptEdits' }));
    await waitFor(() => runState(r.h, id) === 'failed', 'the run to fail');
    assert.match(rowOf(r.h, id, 'a')?.error ?? '', /mode "acceptEdits" is above the "default" ceiling set for agent "claude"/);
    assert.equal(r.sim.of('CREATE_SESSION').length, 0);
  } finally {
    r.h.close();
  }
});

test('an agent the catalog does not offer fails the node and creates nothing', async () => {
  const r = rig(() => {}, { agents: [{ ...CLAUDE, available: false, reason: 'claude was not found on your login shell\'s PATH' }] });
  try {
    const id = r.start(one());
    await waitFor(() => runState(r.h, id) === 'failed', 'the run to fail');
    assert.match(rowOf(r.h, id, 'a')?.error ?? '', /unavailable: claude was not found/);
    assert.equal(r.sim.of('CREATE_SESSION').length, 0);
  } finally {
    r.h.close();
  }
});

test('run start and retry are refused before anything runs when the catalog would refuse an agent (R6)', async () => {
  const r = rig(() => {}, { agents: [{ ...CLAUDE, ceiling: 'plan' }] });
  try {
    const refused = await r.h.topics.preflight('test.start-run', { nonce: 'n9', project_id: 'p', ir: one({ mode: 'default' }), input: {} });
    assert.equal(refused?.data.ok, false);
    assert.equal(refused?.data.nonce, 'n9');
    assert.match((refused?.data.error as { message: string }).message, /mode "default" is above the "plan" ceiling/);
    assert.equal((refused?.data.error as { code: string }).code, 'invalid_ir');
    assert.equal(await r.h.topics.preflight('test.start-run', { project_id: 'p', ir: one(), input: {} }), undefined, 'a workflow the daemon would run goes on');
    assert.equal(await r.h.topics.preflight('human.answer', {}), undefined, 'other topics are not asked');
    assert.equal(r.sim.of('PREPARE_WORKTREE').length, 0);
    assert.equal(r.sim.of('CREATE_SESSION').length, 0);
  } finally {
    r.h.close();
  }
});

test('run.retry is asked again: a ceiling lowered while the run was failed refuses the retry', async () => {
  const r = rig((_, __, sess) => sess.end('refusal'));
  try {
    const id = r.start(one({ mode: 'default' }));
    await waitFor(() => runState(r.h, id) === 'failed', 'the run to fail');
    r.sim.sessions.length = 0;
    r.hive.replies.LIST_ACP_AGENTS = () => ({ agents: [{ ...CLAUDE, ceiling: 'plan' }] });
    const refused = await r.h.topics.preflight('run.retry', { nonce: 'n1', run_id: id });
    assert.match((refused?.data.error as { message: string }).message, /above the "plan" ceiling/);
    assert.equal(runState(r.h, id), 'failed', 'a refused retry changes nothing');
  } finally {
    r.h.close();
  }
});

// A cancel or a timeout that lands while PROMPT_ACP is in flight must still stop the turn that prompt starts.

/** Cancels the run just after the nth PROMPT_ACP is answered but before the engine reads the answer. */
function cancelDuringPrompt(r: ReturnType<typeof rig>, nth: number, id: () => string): void {
  const answer = r.hive.replies.PROMPT_ACP;
  let n = 0;
  r.hive.replies.PROMPT_ACP = (p) => {
    const reply = answer(p);
    if (++n === nth) void Promise.resolve().then(() => r.h.engine.cancelRun(id()));
    return reply;
  };
}

test('a cancel while the first PROMPT_ACP is in flight cancels the turn it started, pinned to the new ack, then kills if it never ends', async () => {
  const r = rig(() => {}, { grace: 50 });
  let id = '';
  cancelDuringPrompt(r, 1, () => id);
  try {
    id = r.start(one());
    await waitFor(() => r.sim.of('KILL_SESSION').length === 1, 'the kill');
    assert.deepEqual(r.sim.of('CANCEL_ACP'), [{ session_id: 's1', prompt_id: 10, epoch: 1 }], 'the turn that was started, not "whatever turn is running"');
    assert.equal(rowOf(r.h, id, 'a')?.state, 'cancelled');
  } finally {
    r.h.close();
  }
});

test('a cancel while the first PROMPT_ACP is in flight needs no kill when the turn reports cancelled', async () => {
  const r = rig(() => {}, { grace: 200 });
  r.hive.replies.CANCEL_ACP = () => {
    setImmediate(() => r.sim.sessions[0].end('cancelled'));
    return { was_busy: true };
  };
  let id = '';
  cancelDuringPrompt(r, 1, () => id);
  try {
    id = r.start(one());
    await waitFor(() => r.sim.of('CANCEL_ACP').length === 1, 'the cancel');
    await new Promise((res) => setTimeout(res, 300));
    assert.equal(r.sim.of('KILL_SESSION').length, 0);
  } finally {
    r.h.close();
  }
});

test('a cancel while the F3 re-prompt is in flight is pinned to the re-prompt, not the finished first turn', async () => {
  const r = rig(
    (turn, _, sess) => {
      if (turn === 1) sess.end(); // no result: the re-prompt follows
    },
    { grace: 50 },
  );
  let id = '';
  cancelDuringPrompt(r, 2, () => id);
  try {
    id = r.start(one());
    await waitFor(() => r.sim.of('KILL_SESSION').length === 1, 'the kill');
    assert.deepEqual(r.sim.of('CANCEL_ACP'), [{ session_id: 's1', prompt_id: 20, epoch: 1 }], 'the second turn: a cancel pinned to the first would be a no-op');
  } finally {
    r.h.close();
  }
});
