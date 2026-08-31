import { expect, type Page } from '@playwright/test';

// Shared readiness/sentinel helpers for the e2e-real suite.
//
// Every spec file in this suite drives the SAME long-lived bash session on
// the SAME hived daemon (globalSetup spawns one for the whole run), and each
// test opens a fresh page that re-attaches to it. Two consequences bit spec
// 245 repeatedly:
//
//   1. A fresh attach replays the session's whole scrollback. So a spec that
//      waits for a fixed string like `HIVE_PUMP_DONE` can be satisfied by an
//      EARLIER test's output, replayed — before the command it just typed has
//      run at all. Every sentinel must therefore be unique per call.
//   2. The shell may still be busy running the previous test's command (the
//      scrollback specs flood tens of thousands of lines). Typed input is not
//      lost, it is QUEUED — so `type(...)` followed by a fixed sleep proves
//      nothing. The only reliable readiness signal is a round trip: type a
//      unique marker and wait to see it come back.
//
// Under CPU load, the fixed `waitForTimeout(200)` these specs used instead
// reproduced 5 failures in one suite run (spec 245).

let seq = 0;

/** A sentinel that cannot collide with a replayed one from an earlier test. */
export function sentinel(prefix: string): string {
  seq += 1;
  return `${prefix}_${Date.now().toString(36)}_${seq}`;
}

/** Whether the first term's buffer currently contains `needle`. */
export function bufferHas(page: Page, needle: string): Promise<boolean> {
  return page.evaluate((n) => {
    const st = [...(window.__hive_state?.terms?.values() || [])][0] as
      | { term?: { buffer?: { active?: import('@xterm/xterm').IBuffer } } }
      | undefined;
    const buf = st?.term?.buffer?.active;
    if (!buf) return false;
    for (let i = 0; i < buf.length; i++) {
      if ((buf.getLine(i)?.translateToString(true) || '').includes(n))
        return true;
    }
    return false;
  }, needle);
}

/**
 * Disable echo and block until the shell has actually executed a command typed
 * NOW — which also waits out any flood a previous test left running.
 *
 * Replaces `keyboard.type('stty -echo\n'); waitForTimeout(200)`. The timeout is
 * generous on purpose: a queued 60 000-line flood ahead of us is normal here,
 * and waiting for it is the point.
 */
export async function settleShell(page: Page, timeout = 60000): Promise<void> {
  // Ctrl-C first: the scrollback specs start 40 000-60 000 line floods and
  // nothing stops them at test end, so the next test inherits a shell that is
  // still busy for minutes under CPU load. SIGINT is a no-op at an idle
  // prompt, so this is safe on every path.
  await page.keyboard.press('Control+C');
  const tag = sentinel('HIVE_READY');
  // The tag is typed as part of the command, and on the FIRST call of a run
  // the tty is still echoing, so the typed line itself paints the tag on
  // screen. Waiting on it verbatim would match that echo — proving the
  // keystrokes arrived, but NOT that the shell executed anything, which is
  // the one thing this helper exists to prove. Split the tag across two
  // adjacent shell strings: `echo "HIVE_READY_ab""cd"` echoes as written and
  // only prints the joined `HIVE_READY_abcd` once bash has actually run it.
  const cut = tag.length - 2;
  const typed = `${tag.slice(0, cut)}""${tag.slice(cut)}`;
  await page.keyboard.type(`stty -echo; echo "${typed}"\n`);
  await waitForSentinel(page, tag, timeout);
}

/** The last `n` non-empty lines of the first term, for failure messages. */
export function bufferTail(page: Page, n = 6): Promise<string[]> {
  return page.evaluate((count) => {
    const st = [...(window.__hive_state?.terms?.values() || [])][0] as
      | { term?: { buffer?: { active?: import('@xterm/xterm').IBuffer } } }
      | undefined;
    const buf = st?.term?.buffer?.active;
    if (!buf) return ['<no term buffer>'];
    const out: string[] = [];
    for (let i = 0; i < buf.length; i++) {
      const l = (buf.getLine(i)?.translateToString(true) || '').trim();
      if (l) out.push(l.slice(0, 100));
    }
    return out.slice(-count);
  }, n);
}

/**
 * Wait for a sentinel returned by one of the pump helpers below.
 *
 * On timeout the message carries the buffer tail: a shell error there means
 * the typed command was mangled, an unchanged prompt means it never ran, and
 * a stream of markers means the pump is merely slow.
 */
export async function waitForSentinel(
  page: Page,
  tag: string,
  timeout = 60000,
): Promise<void> {
  try {
    await expect
      .poll(() => bufferHas(page, tag), {
        timeout,
        intervals: [250, 500],
      })
      .toBe(true);
  } catch (e) {
    const tail = await bufferTail(page).catch(() => ['<unreadable>']);
    throw new Error(
      `sentinel ${tag} never appeared within ${timeout}ms; buffer tail:\n${tail.join('\n')}`,
      { cause: e },
    );
  }
}
