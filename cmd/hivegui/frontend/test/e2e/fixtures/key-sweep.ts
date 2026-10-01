import { expect, type Page } from '@playwright/test';

// Spec 481: helpers for test/e2e/every-shortcut.spec.ts, the sweep that
// fires every chord in KEY_SCOPES and every MENU_COMMANDS event.
//
// The binding data cannot be imported here: app/key-scopes.ts pulls in
// the store and the modal graph, which only load in a browser. So every
// helper reads it INSIDE the page, by importing the app's own modules
// through the vite dev server — the same URLs the app loaded, so the same
// module instances and the same live state.
//
// The specifiers are variables on purpose: tsc would otherwise try to
// resolve an absolute '/src/…' path from this file and fail.

export type Platform = 'mac' | 'other';

export const SESSION_COUNT = 3;

/** One chord string of one binding, as the page resolved it. */
export interface SweepChord {
  chord: string;
  command: string | null;
}

/** What firing one synthetic keydown did. */
export interface FireResult {
  /** resolveKey's answer before the key was dispatched. */
  resolved: { kind: string; scope?: string; command?: string | null } | null;
  /** The dispatcher called preventDefault. */
  consumed: boolean;
  log: { id: string; ran: boolean }[];
}

/** Forces the platform the app sees (lib/platform.ts reads navigator once
 * at import) and starts every load from empty storage, so one chord's
 * persisted prefs — the view, zoom, hive.debug — cannot leak into the
 * next. A test that must read storage across a reload sets
 * sessionStorage 'hive.sweep.keep' first; the next load honours it once. */
export async function prepare(page: Page, platform: Platform) {
  await page.addInitScript((mac: boolean) => {
    Object.defineProperty(navigator, 'platform', {
      get: () => (mac ? 'MacIntel' : 'Linux x86_64'),
    });
    Object.defineProperty(navigator, 'userAgentData', {
      get: () => ({ platform: mac ? 'macOS' : 'Linux' }),
    });
    try {
      if (sessionStorage.getItem('hive.sweep.keep') === '1') {
        sessionStorage.removeItem('hive.sweep.keep');
      } else {
        localStorage.clear();
        sessionStorage.clear();
      }
    } catch {
      /* storage off */
    }
  }, platform === 'mac');
}

/** Loads the app with `sessions` sessions in project p1, in single view. */
export async function boot(page: Page, sessions = SESSION_COUNT) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  await page.evaluate(async (n) => {
    for (let i = 2; i <= n; i++) await window.__hive.addSession?.(`s${i}`);
  }, sessions);
  await page.waitForFunction(
    (n) => window.__hive_state?.sessions.length === n,
    sessions,
  );
  // Adding a session activates it; every fixture works on s1.
  await activate(page, 's1');
  expect(
    await page.evaluate(() => window.__hive_state?.view),
    'every chord starts in single view',
  ).toBe('single');
}

/** Runs a command the way the menu would, for fixtures that need a state
 * no key reaches from a fresh boot. */
export async function run(page: Page, id: string) {
  await page.evaluate(async (cmd) => {
    const url = '/src/app/command-registry.ts';
    const m = await import(/* @vite-ignore */ url);
    if (!m.runCommand(cmd)) throw new Error(`fixture command ${cmd} declined`);
  }, id);
}

/** Calls an exported function of an app module in the page. */
export async function call(page: Page, path: string, fn: string) {
  await page.evaluate(
    async ([url, name]) => {
      const m = await import(/* @vite-ignore */ url);
      await m[name]();
    },
    [path, fn] as const,
  );
}

export async function scopeActive(page: Page, scopeId: string) {
  return page.evaluate(async (id) => {
    const url = '/src/app/key-scopes.ts';
    const m = await import(/* @vite-ignore */ url);
    return m.scopeById(id).active() as boolean;
  }, scopeId);
}

export async function scopeIds(page: Page) {
  return page.evaluate(async () => {
    const url = '/src/app/key-scopes.ts';
    const m = await import(/* @vite-ignore */ url);
    return (m.KEY_SCOPES as { id: string; bindings: () => unknown[] }[]).map(
      (s) => ({ id: s.id, bindings: s.bindings().length }),
    );
  });
}

/** Every chord string of every binding of a scope, on the page's
 * platform: an array or {mac, other} binding contributes each of its
 * chords, so an alternative spelling is pressed too. */
export async function scopeChords(page: Page, scopeId: string) {
  return page.evaluate(async (id) => {
    const imp = (u: string) => import(/* @vite-ignore */ u);
    const ks = await imp('/src/app/key-scopes.ts');
    const ch = await imp('/src/lib/chord.ts');
    const pf = await imp('/src/lib/platform.ts');
    const out: { chord: string; command: string | null }[] = [];
    for (const b of ks.scopeById(id).bindings()) {
      for (const chord of ch.chordsFor(b.keys, pf.isMac) as string[]) {
        out.push({ chord, command: b.command });
      }
    }
    return out;
  }, scopeId);
}

/** Builds the keydown a chord describes, checks which binding it resolves
 * to, dispatches it, and returns the command log — all in one task, so a
 * command that reloads the page cannot lose the result. */
export async function fireChord(page: Page, chord: string) {
  return page.evaluate(async (s): Promise<FireResult> => {
    const imp = (u: string) => import(/* @vite-ignore */ u);
    const ch = await imp('/src/lib/chord.ts');
    const kb = await imp('/src/app/keyboard.ts');
    const pf = await imp('/src/lib/platform.ts');
    const c = ch.parseChord(s, pf.isMac);

    // parseChord lowercases the key; a real event carries it cased.
    const NAMED: Record<string, string> = {
      escape: 'Escape',
      enter: 'Enter',
      backspace: 'Backspace',
      tab: 'Tab',
      arrowup: 'ArrowUp',
      arrowdown: 'ArrowDown',
      arrowleft: 'ArrowLeft',
      arrowright: 'ArrowRight',
    };
    const CODE_OF: Record<string, string> = {
      '-': 'Minus',
      _: 'Minus',
      '=': 'Equal',
      '+': 'Equal',
      '/': 'Slash',
      '?': 'Slash',
      ',': 'Comma',
      '[': 'BracketLeft',
      ']': 'BracketRight',
      '`': 'Backquote',
    };
    const KEY_OF: Record<string, string> = {
      Minus: '-',
      Equal: '=',
      Slash: '/',
      Comma: ',',
      BracketLeft: '[',
      BracketRight: ']',
      Backquote: '`',
    };
    const codeFor = (k: string) => {
      if (NAMED[k]) return NAMED[k];
      if (/^[a-z]$/.test(k)) return `Key${k.toUpperCase()}`;
      if (/^[0-9]$/.test(k)) return `Digit${k}`;
      if (CODE_OF[k]) return CODE_OF[k];
      throw new Error(`no e.code for key "${k}" in chord ${s}`);
    };
    const keyFor = (code: string) => {
      const m = /^(?:Key|Digit)(.)$/.exec(code);
      if (m) return m[1].toLowerCase();
      if (KEY_OF[code]) return KEY_OF[code];
      throw new Error(`no e.key for code "${code}" in chord ${s}`);
    };

    let key: string = c.key ?? keyFor(c.code);
    if (key.length > 1) {
      if (!NAMED[key]) throw new Error(`unknown key "${key}" in chord ${s}`);
      key = NAMED[key];
    }
    const shift = c.shift === true;
    if (shift && /^[a-z]$/.test(key)) key = key.toUpperCase();
    const ev = new KeyboardEvent('keydown', {
      key,
      code: c.code ?? codeFor(key.toLowerCase()),
      metaKey: c.meta === true,
      ctrlKey: c.ctrl === true,
      altKey: c.alt === true,
      shiftKey: shift,
      bubbles: true,
      cancelable: true,
    });

    const r = kb.resolveKey(ev, pf.isMac);
    const log = window.__hive_commandLog;
    if (!log) throw new Error('command log missing: is VITE_WAILS_MOCK set?');
    log.length = 0;
    const consumed = !document.body.dispatchEvent(ev);
    return {
      resolved: r
        ? { kind: r.kind, scope: r.scope?.id, command: r.binding?.command }
        : null,
      consumed,
      log: [...log],
    };
  }, chord);
}

/** Fires one native-menu event the way the Wails runtime delivers it, and
 * returns the command log plus hive.debug, read in the same task. */
export async function fireMenu(page: Page, event: string) {
  return page.evaluate((name) => {
    const log = window.__hive_commandLog;
    if (!log) throw new Error('command log missing: is VITE_WAILS_MOCK set?');
    log.length = 0;
    window.__hive.emit(name);
    let debug: string | null = null;
    try {
      debug = localStorage.getItem('hive.debug');
    } catch {
      /* storage off */
    }
    return { log: [...log], debug };
  }, event);
}

/** Installs a plugin from /plugins/<id> through Settings → Plugins (the
 * mock's Confirm accepts), waits for it to be active, and closes
 * Settings. Opens Settings by its menu event, not a key, so it works
 * whichever platform the page was forced to. */
export async function installPlugin(page: Page, id: string) {
  await page.evaluate(() => window.__hive.emit('menu:settings'));
  await expect(page.locator('#settings')).toBeVisible();
  await page.locator('#settings-tab-plugins').click();
  await page.locator('#settings-plugin-source').fill(`/plugins/${id}`);
  await page.locator('#settings-plugin-install').click();
  const row = page.locator(`.settings-plugin-row[data-plugin-id="${id}"]`);
  await expect(row.locator('.settings-plugin-enabled')).toBeChecked();
  await expect
    .poll(() =>
      page.evaluate(async (pid) => {
        const url = '/src/store/store.ts';
        const m = await import(/* @vite-ignore */ url);
        return Object.keys(m.appStore.getState().pluginUI).includes(pid);
      }, id),
    )
    .toBe(true);
  await call(page, '/src/app/modals/settings.ts', 'closeSettings');
  await expect(page.locator('#settings')).toBeHidden();
}

/** Parks session s1 on a worktree decision, as the daemon does when its
 * worktree setup fails. events.ts raises the choice dialog for it. */
export async function blockActiveSession(page: Page) {
  await page.evaluate(() => {
    const s = window.__hive.state?.sessions.find((x) => x.id === 's1');
    if (!s) throw new Error('no s1');
    Object.assign(s, {
      phase: 'blocked',
      pending_worktree_choice: {
        park_id: 'park-1',
        kind: 'fetch_failed',
        message: 'fatal: could not read from remote',
        branch: 'feat',
      },
    });
    window.__hive.emit(
      'session:event',
      JSON.stringify({ kind: 'updated', session: s }),
    );
  });
}

/** Kills s1 with an error, which raises its dead-session overlay. */
export async function killActiveSession(page: Page) {
  await page.evaluate(() => {
    const s = window.__hive.state?.sessions.find((x) => x.id === 's1');
    if (!s) throw new Error('no s1');
    s.alive = false;
    s.last_error = 'boom';
    window.__hive.emit(
      'session:event',
      JSON.stringify({ kind: 'updated', session: s }),
    );
  });
}

/** Makes a session active by clicking its sidebar row. */
export async function activate(page: Page, id = 's1') {
  await page.locator(`#projects .hv-session-row[data-sid="${id}"]`).click();
  await expect
    .poll(() => page.evaluate(() => window.__hive_state?.activeId))
    .toBe(id);
}
