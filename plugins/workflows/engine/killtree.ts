// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Stopping a check's process tree (spec 529, Check and human nodes). A check
// runs in its own process group so it can be signalled as one, which also
// means the plugin Manager's tree kill on a plugin crash does not reach it:
// the engine records each check's pid, pgid and start time, and reaps
// survivors at startup, but only when the pid's start time still matches. A
// reused pid must never kill a stranger.

import { spawn, spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';

/** How long a polite stop waits before the forceful one. */
export const KILL_GRACE_MS = 10_000;

export interface Tracked {
  pid: number;
  /** The process group on POSIX (the leader's pid, since a check is spawned detached). */
  pgid?: number;
  /** The process start time as `procStart` reads it. */
  proc_start?: string;
}

export interface KillDeps {
  platform?: string;
  graceMs?: number;
  /** For Windows: how taskkill is run. A seam so the test can see its arguments. */
  spawn?: typeof spawn;
  spawnSync?: typeof spawnSync;
  kill?: (pid: number, signal: string | number) => void;
}

const sleep = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));

export function alive(pid: number, kill: KillDeps['kill'] = process.kill): boolean {
  try {
    kill(pid, 0);
    return true;
  } catch (e) {
    return (e as { code?: string }).code === 'EPERM';
  }
}

/**
 * When the process started, in whatever form identifies it: the same string
 * every time for the same process, a different one for a reused pid. Undefined
 * when there is no such process. Linux reads clock ticks from /proc; elsewhere
 * `ps` gives whole seconds, so a pid reused inside the same second on macOS is
 * a known ceiling (B-9 research in B3 decides whether to go further).
 */
export function procStart(pid: number, deps: KillDeps = {}): string | undefined {
  const run = deps.spawnSync ?? spawnSync;
  if (deps.spawnSync === undefined && process.platform === 'linux') {
    // Field 22 of /proc/<pid>/stat is the start time in clock ticks since boot: finer than ps's whole seconds, so a pid reused within a second is told apart.
    try {
      const stat = readFileSync(`/proc/${pid}/stat`, 'utf8');
      const rest = stat.slice(stat.lastIndexOf(')') + 2).split(' ');
      if (rest[19]) return `proc:${rest[19]}`;
    } catch {
      return undefined; // no such process
    }
  }
  const win = (deps.platform ?? process.platform) === 'win32';
  const r = win
    ? run('powershell', ['-NoProfile', '-Command', `(Get-Process -Id ${pid}).StartTime.ToFileTimeUtc()`], { encoding: 'utf8', windowsHide: true })
    : run('ps', ['-o', 'lstart=', '-p', String(pid)], { encoding: 'utf8' });
  const out = r.stdout.trim();
  return r.status === 0 && out !== '' ? out : undefined;
}

async function taskkill(pid: number, force: boolean, deps: KillDeps): Promise<void> {
  const run = deps.spawn ?? spawn;
  await new Promise<void>((resolve) => {
    const c = run('taskkill', ['/T', ...(force ? ['/F'] : []), '/PID', String(pid)], { windowsHide: true, stdio: 'ignore' });
    c.on('error', () => resolve());
    c.on('close', () => resolve());
  });
}

/** What a stop achieved: `refused` (the recorded ids are not a process this engine may signal: nothing was sent), `stopped` (signalled and gone, or force sent), `gone` (nothing was left), `kept` (the OS refused the signal, EPERM: the process is still there). */
export type StopResult = 'stopped' | 'gone' | 'kept' | 'refused';

/**
 * Whether the recorded ids name something the engine may signal, as a reason
 * when not. A row in `runs.db` is only trusted this far: `kill(-1)` signals
 * every process the user owns, `kill(0)` and `kill(-own pid)` the engine's own
 * group, and an arbitrary pid is someone else's process. The engine writes
 * pgid == pid for every check, so anything else is a corrupt or forged row.
 */
export function unsafeTarget(t: Tracked, own: number = process.pid): string | undefined {
  const { pid, pgid } = t;
  if (!Number.isSafeInteger(pid) || pid < 2) return `pid ${String(pid)} is not a process id`;
  if (pid === own) return `pid ${pid} is the engine itself`;
  if (pgid !== undefined && (!Number.isSafeInteger(pgid) || pgid < 2)) return `process group ${String(pgid)} is not a process group id`;
  if (pgid !== undefined && pgid !== pid) return `process group ${pgid} is not the process's own group ${pid}`;
  return undefined;
}

/**
 * TERM, then KILL after the grace period, to the whole group (POSIX) or tree
 * (Windows, which has no graceful signal for a windowless tree: `taskkill /T`
 * asks, `/T /F` forces). Never throws: a signal the OS refuses (EPERM, say a
 * check started under sudo) is reported as `kept`, because only ESRCH proves
 * a group is gone and the caller must decide what a surviving process means.
 * Every caller (reap, shutdown, a check's own stop) comes through here, so the
 * guard against hostile ids is here: nothing is sent for ids `unsafeTarget`
 * refuses.
 */
export async function killTree(t: Tracked, deps: KillDeps = {}): Promise<StopResult> {
  if (unsafeTarget(t) !== undefined) return 'refused';
  const grace = deps.graceMs ?? KILL_GRACE_MS;
  const kill = deps.kill ?? process.kill;
  if ((deps.platform ?? process.platform) === 'win32') {
    await taskkill(t.pid, false, deps);
    await sleep(grace);
    await taskkill(t.pid, true, deps);
    return 'stopped';
  }
  const target = -t.pid;
  const signal = (sig: string): 'ok' | 'gone' | 'kept' => {
    try {
      kill(target, sig);
      return 'ok';
    } catch (e) {
      return (e as { code?: string }).code === 'ESRCH' ? 'gone' : 'kept';
    }
  };
  const term = signal('SIGTERM');
  if (term !== 'ok') return term === 'gone' ? 'gone' : 'kept';
  const deadline = Date.now() + grace;
  while (Date.now() < deadline) {
    await sleep(Math.min(50, Math.max(1, deadline - Date.now())));
    if (!alive(target, kill)) return 'stopped';
  }
  return signal('SIGKILL') === 'kept' ? 'kept' : 'stopped';
}

/**
 * Startup reaping: kills a recorded check's group only when the pid is alive
 * and its start time is the recorded one. Returns what it found.
 */
export async function reap(t: Tracked, deps: KillDeps = {}): Promise<'gone' | 'stranger' | 'killed' | 'kept' | 'refused'> {
  if (unsafeTarget(t) !== undefined) return 'refused';
  const now = procStart(t.pid, deps);
  if (now === undefined) return 'gone';
  if (t.proc_start === undefined || now !== t.proc_start) return 'stranger';
  const r = await killTree(t, { graceMs: 2_000, ...deps });
  return r === 'kept' || r === 'refused' ? r : 'killed';
}
