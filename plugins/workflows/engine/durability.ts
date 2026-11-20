// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Agent nodes across a restart (spec 529, Durability). When the engine starts,
// and again after a reconnect, every agent node that was running is read against
// what the daemon says now, and one of three things is true of it:
//
//   - its turn survived (same daemon boot, same transcript epoch): the turn is
//     recorded if it ended, watched if it is still running, and sent once if it
//     was never started. The attempt does not change;
//   - it did not (the daemon restarted, the session is gone or was restarted):
//     the node is `interrupted`, and its retry is attempt + 1;
//   - it was being created when the engine stopped: the session it made is
//     adopted when there is exactly one, and removed otherwise.
//
// Nothing here starts a second session while the first may still be running.

import { INTERRUPTED_NOTE } from './agent.ts';
import type { Engine } from './engine.ts';
import type { Row } from './model.ts';
import type { SessionState } from './sessions.ts';

type Listed = SessionState & { id: string };

const errMsg = (e: unknown): string => (e instanceof Error ? e.message : String(e));

export async function recoverAgents(engine: Engine, runIds: string[]): Promise<void> {
  const runs = runIds.map((id) => ({ id, rows: engine.agentRows(id) })).filter((r) => r.rows.length > 0);
  // A pending row may have been launched (its CREATE_SESSION sent) when the engine stopped; one that was never made ready cannot have been.
  const maybeLaunched = (r: Row): boolean => r.state === 'running' || r.state === 'waiting' || r.state === 'interrupted' || (r.state === 'pending' && r.ready);
  if (!runs.some((r) => r.rows.some(maybeLaunched))) return;

  const hive = engine.hive;
  const link = engine.link;
  // A connection lost while the nodes are read says nothing about them: they stay as they are, and the reconnect reads them again.
  const lost = (): boolean => engine.link !== link || engine.disconnected;
  const send = hive.send?.bind(hive);
  const listed = send ? await engine.feed.listSessions(send, engine.daemonWaitMs) : undefined;
  if (lost()) return;
  if (!listed || !send) {
    // Without the daemon's word nothing can be shown to have survived: the turns are retried, and a retry that finds one still running is refused by the daemon, never doubled.
    for (const run of runs) for (const r of run.rows) if (r.state === 'running' || r.state === 'waiting') engine.interruptNode(run.id, r.key);
    return;
  }
  const byId = new Map(listed.map((s) => [s.id, s]));

  for (const run of runs) {
    for (const r of run.rows) {
      if (r.state !== 'running' && r.state !== 'waiting') continue;
      if (lost()) return;
      try {
        await recoverRunning(engine, run.id, r, byId, send, lost);
      } catch (err) {
        if (lost()) return;
        console.error(`workflows: recovering ${run.id}/${r.key}: ${errMsg(err)}`);
        engine.interruptNode(run.id, r.key);
      }
    }
    if (lost()) return;
    await settleOrphans(engine, run.id, listed, run.rows, link, lost);
  }
}

/** One running or waiting agent row, against the daemon. */
async function recoverRunning(engine: Engine, runId: string, row: Row, byId: Map<string, Listed>, send: (name: string, payload?: object) => void, lost: () => boolean): Promise<void> {
  const key = row.key;
  const sid = row.session_id;
  const info = sid ? byId.get(sid) : undefined;
  // The row says which daemon boot its turn belongs to: after a restart there is no turn to find, whatever the transcript says.
  const boot = engine.hive.welcome?.boot_id;
  const sameBoot = !!boot && row.boot_id === boot;
  if (!sid || !sameBoot || !info || info.removed || !info.alive || info.last_error || info.phase !== '') return engine.interruptNode(runId, key);

  engine.feed.track(sid);
  const recorded = row.prompt_id !== undefined && row.epoch !== undefined ? { prompt_id: row.prompt_id, epoch: row.epoch } : undefined;
  if (info.kind === 'pty') {
    // The user has taken the session over (F2): the node keeps waiting for it to be handed back.
    return recorded ? engine.resumeAgent(runId, key, { kind: 'watch', ack: recorded, again: row.reprompted === true }) : engine.interruptNode(runId, key);
  }
  const tx = await engine.feed.fetchTranscript(send, sid, engine.daemonWaitMs);
  if (lost()) return;
  if (!tx) return engine.interruptNode(runId, key);

  // A turn of ours is a user item the daemon stamped with this run's principal.
  // A retried attempt may reuse a session whose transcript still holds the earlier attempt's turns of ours; its own first turn is the one that carries the interrupted note.
  const mine = (u: { origin: string; text: string }): boolean => u.origin === engine.principalOf(runId) && (recorded !== undefined || row.attempt <= 1 || u.text.startsWith(INTERRUPTED_NOTE.slice(0, 64)));
  const ours = [...tx.users].filter(([, u]) => mine(u)).map(([id]) => ({ id })).sort((a, b) => a.id - b.id);
  if (recorded && tx.epoch !== recorded.epoch) return engine.interruptNode(runId, key); // the session was restarted or reloaded: that turn is gone
  // The PROMPT_ACP ack can be lost (the engine died between the daemon starting the turn and the row recording it): the turn is in the transcript, and sending the prompt again would run it twice.
  const after = recorded ? recorded.prompt_id : 0;
  const unrecorded = ours.filter((u) => u.id > after).at(-1);

  // The daemon trims the oldest transcript items of a long conversation, but never the state fields: `prompt_id` is the turn the session is on.
  // Same boot, same epoch and the recorded id as the latest turn prove the turn is the row's, whether or not its item is still listed.
  let ack = recorded;
  let again = row.reprompted === true; // the engine's own record of the F3 re-prompt, never read back from the transcript's text
  if (unrecorded && tx.prompt_id === unrecorded.id) {
    // The only second turn an attempt sends is the F3 re-prompt, so a turn newer than the recorded one is it.
    ack = { prompt_id: unrecorded.id, epoch: tx.epoch };
    again = recorded !== undefined;
    if (lost()) return;
    engine.ackTurn(runId, key, ack, again);
  } else if (recorded && tx.prompt_id === recorded.prompt_id) {
    // follow the recorded turn
  } else if (!recorded && tx.prompt_id === 0) {
    ack = undefined; // nothing was ever sent: the turn is sent now
  } else {
    // A turn the node cannot follow: the user's own, or ours with its item trimmed away and no ack recorded. Which one it is cannot be told, so
    // nothing is sent (a prompt on a running session is refused as busy, and the refusal would fail the node) and nothing is killed: the node
    // waits for the session to go idle, then it is interrupted and its retry prompts an idle session.
    return awaitIdleThenInterrupt(engine, runId, row, tx.prompt_id === 0 || tx.stop_reason !== '', lost); // a turn is running only once a prompt has started and has no stop reason yet
  }
  if (lost()) return;
  engine.resumeAgent(runId, key, ack ? { kind: 'watch', ack, again } : { kind: 'send' });
}

/**
 * Interrupts the node once its session has no turn running. Not awaited by
 * recovery: the other nodes are read meanwhile. Bounded by the node's own
 * timeout, counted from when it started: past it the turn is cancelled (the
 * session is left alone) and the node fails naming the timeout.
 */
function awaitIdleThenInterrupt(engine: Engine, runId: string, row: Row, idle: boolean, lost: () => boolean): void {
  const { key, attempt } = row;
  const sid = row.session_id as string;
  if (idle) return engine.interruptNode(runId, key);
  // The waiter is for this attempt of this node: once the node is cancelled, retried or finished it must do nothing.
  const mineStill = (): boolean => {
    const r = engine.rowOf(runId, key);
    return r !== undefined && r.attempt === attempt && r.session_id === sid && (r.state === 'running' || r.state === 'waiting');
  };
  const node = engine.timeoutOf(runId, key);
  const since = Date.parse(row.started_at ?? '');
  const left = Math.max(1000, node * 1000 - (Number.isFinite(since) ? Date.now() - since : 0));
  const feed = engine.feed;
  let over = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const end = (then: () => void): void => {
    if (over) return;
    over = true;
    clearTimeout(timer);
    unsub();
    if (!lost() && mineStill()) then();
  };
  const look = (): void => {
    const info = feed.session(sid);
    if (lost() || !mineStill()) return end(() => {});
    const t = feed.transcript(sid);
    if (info === undefined || info.removed || !info.alive || (t && (t.prompt_id === 0 || t.stop_reason !== ''))) end(() => engine.interruptNode(runId, key, true));
  };
  const unsub = feed.subscribe(sid, look);
  timer = setTimeout(
    () =>
      end(() => {
        // The turn outlived the node's timeout: stop it, and fail the node as any timed-out node fails.
        void engine.hive.request('CANCEL_ACP', { session_id: sid }).catch(() => {});
        engine.failNode(runId, key, `timed out after ${node}s`, { link: engine.link });
      }),
    left,
  );
  timer.unref?.();
}

/**
 * Sessions this run made that no row names: a CREATE_SESSION whose ack the
 * engine never recorded. Exactly one, for a node waiting to be launched, is
 * adopted (it was never prompted: the first turn follows the ack); anything
 * else is killed, so a node never ends up with two sessions in its worktree.
 */
async function settleOrphans(engine: Engine, runId: string, listed: Listed[], before: Row[], link: number, lost: () => boolean): Promise<void> {
  const rows = engine.agentRows(runId);
  if (rows.length === 0) return; // the run ended while its nodes were read: its sessions are the run-end cleanup's
  // A session a node named when recovery began is that node's, even if the node has changed or ended since.
  const named = new Set([...before, ...rows].map((r) => r.session_id).filter((id): id is string => !!id));
  const prefix = `${engine.workflowName(runId)} · `;
  const principal = engine.principalOf(runId);
  const byKey = new Map<string, Listed[]>();
  for (const s of listed) {
    if (s.spawned_by !== principal || s.removed || named.has(s.id)) continue;
    const key = s.name?.startsWith(prefix) ? s.name.slice(prefix.length) : '';
    byKey.set(key, [...(byKey.get(key) ?? []), s]);
  }
  for (const [key, group] of byKey) {
    const row = rows.find((r) => r.key === key);
    const [only] = group;
    // Only a node that was about to launch can own it; a node with a session of its own, or a finished one, has no use for another.
    if (only && group.length === 1 && row && row.state === 'pending' && row.ready && !row.session_id && only.alive && only.kind === 'acp') {
      engine.adoptOrphan(runId, key, only.id);
      continue;
    }
    for (const s of group) {
      let err: unknown;
      // Twice: a node must not launch while a session of its own is left running in its worktree.
      for (let i = 0; i < 2; i++) {
        try {
          await engine.hive.request('KILL_SESSION', { session_id: s.id, force: true }, { timeoutMs: 60_000 });
          err = undefined;
          break;
        } catch (e) {
          err = e;
        }
      }
      if (err !== undefined) {
        if (lost()) return;
        console.error(`workflows: could not remove ${s.id}, a leftover session of ${runId}/${key}: ${errMsg(err)}`);
        if (row && (row.state === 'pending' || row.state === 'interrupted')) engine.failNode(runId, key, `a leftover session (${s.id}) from an earlier launch could not be removed (${errMsg(err)}), so the node was not started in its worktree`, { link });
      }
    }
  }
}
