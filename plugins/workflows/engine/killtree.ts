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
 * when there is no such process.
 */
export function procStart(pid: number, deps: KillDeps = {}): string | undefined {
  const run = deps.spawnSync ?? spawnSync;
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

/**
 * TERM, then KILL after the grace period, to the whole group (POSIX) or tree
 * (Windows, which has no graceful signal for a windowless tree: `taskkill /T`
 * asks, `/T /F` forces). Resolves once the leader is gone or the force went out.
 */
export async function killTree(t: Tracked, deps: KillDeps = {}): Promise<void> {
  const grace = deps.graceMs ?? KILL_GRACE_MS;
  const kill = deps.kill ?? process.kill;
  if ((deps.platform ?? process.platform) === 'win32') {
    await taskkill(t.pid, false, deps);
    await sleep(grace);
    await taskkill(t.pid, true, deps);
    return;
  }
  const target = -(t.pgid ?? t.pid);
  const signal = (sig: string): boolean => {
    try {
      kill(target, sig);
      return true;
    } catch {
      return false; // ESRCH: nothing left in the group
    }
  };
  if (!signal('SIGTERM')) return;
  const deadline = Date.now() + grace;
  while (Date.now() < deadline) {
    await sleep(Math.min(50, Math.max(1, deadline - Date.now())));
    if (!alive(target, kill)) return;
  }
  signal('SIGKILL');
}

/**
 * Startup reaping: kills a recorded check's group only when the pid is alive
 * and its start time is the recorded one. Returns what it found.
 */
export async function reap(t: Tracked, deps: KillDeps = {}): Promise<'gone' | 'stranger' | 'killed'> {
  const now = procStart(t.pid, deps);
  if (now === undefined) return 'gone';
  if (t.proc_start === undefined || now !== t.proc_start) return 'stranger';
  await killTree(t, { graceMs: 2_000, ...deps });
  return 'killed';
}
