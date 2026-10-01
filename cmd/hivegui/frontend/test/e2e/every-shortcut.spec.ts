import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, type Page, test } from '@playwright/test';
import {
  activate,
  blockActiveSession,
  boot,
  call,
  fireChord,
  fireMenu,
  installPlugin,
  killActiveSession,
  type Platform,
  prepare,
  run,
  SESSION_COUNT,
  scopeActive,
  scopeChords,
  scopeIds,
} from './fixtures/key-sweep.js';

// Spec 481: every keyboard shortcut and every native-menu command,
// driven end to end through the running app and GENERATED from the
// binding data (KEY_SCOPES in app/key-scopes.ts, MENU_COMMANDS in
// app/commands.ts). A new binding or menu item is covered with no new
// test; a binding whose command is unregistered, declines when it should
// not, or is shadowed by another scope fails here.
//
// Each chord is a synthetic keydown on a freshly loaded page (storage
// cleared, SESSION_COUNT sessions, single view, the scope's fixture
// applied). The keydown goes through the app's real capture-phase
// dispatcher; the test-only command log (app/command-registry.ts) says
// which command ran and whether it declined.
//
// What the sweep does NOT prove on its own: that a binding names the
// RIGHT existing command (zoom-in remapped to zoom-out passes by
// construction). test/dom/keymap-parity.test.ts pins chord → id by hand,
// and the spot checks at the bottom assert real effects.

const PLATFORMS: Platform[] = ['mac', 'other'];

// Scopes with no bindings of their own: their modal's own listener
// answers keys (launcher, project editor) or keys pass through to a
// text box (find). The coverage test fails if one ever gains a binding.
const UNBOUND_SCOPES = [
  'find-box',
  'launcher',
  'project-editor',
  // Settings › Shortcuts' capture button: binds nothing on purpose, so
  // every key reaches the button (test/e2e/shortcuts-tab.spec.ts).
  'shortcut-capture',
];

// Puts each scope on screen. A scope with bindings and no entry here
// fails the coverage test, so a new scope cannot go untested.
const FIXTURES: Record<string, (page: Page) => Promise<void>> = {
  'inline-rename': async (page) => {
    await page.locator('#projects .hv-session-row').first().dblclick();
    await expect(page.locator('#projects .name-input')).toBeVisible();
  },
  'choice-dialog': async (page) => {
    await blockActiveSession(page);
    await expect(page.locator('.choice-dialog')).toBeVisible();
  },
  'command-palette': (page) => run(page, 'command-palette'),
  settings: (page) => run(page, 'settings'),
  worktrees: (page) => run(page, 'worktrees'),
  'quick-idea': (page) => run(page, 'quick-idea'),
  'idea-inbox': (page) => run(page, 'idea-inbox'),
  'help-overlay': (page) => run(page, 'keyboard-shortcuts'),
  'help-modal': (page) => call(page, '/src/app/modals/help.ts', 'openHelp'),
  'whats-new': (page) =>
    call(page, '/src/app/modals/whats-new.ts', 'openWhatsNew'),
  'plugin-view': async (page) => {
    await installPlugin(page, 'session-notes');
    await run(page, 'plugin:session-notes:edit-note');
  },
  'build-log': async (page) => {
    await page.evaluate(() => {
      window.__hive.setBuildLog?.('line 1\nline 2');
      window.__hive.emit('update:progress', {
        stage: 'error',
        message: 'build.sh failed (exit status 1)',
        canApply: true,
        hasBuildLog: true,
      });
    });
    await page.locator('#update-banner [data-action-id="log"]').click();
  },
  'blocked-tile': async (page) => {
    await blockActiveSession(page);
    await expect(page.locator('.choice-dialog')).toBeVisible();
    // Escape defers the question; the tile stays parked on it.
    await page.keyboard.press('Escape');
    await expect(page.locator('.choice-dialog')).toBeHidden();
  },
  'dead-overlay': (page) => killActiveSession(page),
  app: async () => {},
  plugins: (page) => installPlugin(page, 'session-notes'),
};

// Commands whose source declines in single view, where every fixture
// starts. Anything else that declines is a failure to look at, not an
// entry to add here.
const DECLINES_IN_SINGLE_VIEW = new Set([
  // commands.ts: nothing to zoom into; ⌘⏎ belongs to the agent.
  'focus-active-session',
  // actions.ts handleArrow: horizontal ⌘-arrows are start/end-of-line
  // in the focused terminal.
  'grid-left',
  'grid-right',
]);

function expectedRan(command: string): boolean {
  const n = /^switch-(\d)$/.exec(command);
  if (n) return Number(n[1]) <= SESSION_COUNT;
  return !DECLINES_IN_SINGLE_VIEW.has(command);
}

// A command whose id names a scope opens it.
const OPENS: Record<string, string> = {
  settings: 'settings',
  worktrees: 'worktrees',
  'quick-idea': 'quick-idea',
  'idea-inbox': 'idea-inbox',
  'command-palette': 'command-palette',
  'keyboard-shortcuts': 'help-overlay',
  'help-modal.shortcuts': 'help-overlay',
  'session.answer-worktree-question': 'choice-dialog',
};

/** The visible effect a command must have, beyond running: closers take
 * their scope down, openers put theirs up. Commands with neither are
 * proven dispatch-only here and by real effect in the spot checks. */
async function expectEffect(page: Page, scopeId: string, command: string) {
  if (/\.(close|dismiss|cancel)$/.test(command)) {
    await expect
      .poll(() => scopeActive(page, scopeId), {
        message: `${command} should close ${scopeId}`,
      })
      .toBe(false);
  }
  const opens = OPENS[command];
  if (opens) {
    await expect
      .poll(() => scopeActive(page, opens), {
        message: `${command} should open ${opens}`,
      })
      .toBe(true);
  }
  if (command === 'dead-session.restart') {
    await expect
      .poll(() =>
        page.evaluate(() => window.__hive.bridgeCalls?.('RestartSession')),
      )
      .toEqual([{ method: 'RestartSession', args: ['s1'] }]);
  }
}

async function enter(page: Page, scopeId: string) {
  await boot(page);
  await FIXTURES[scopeId](page);
  await expect
    .poll(() => scopeActive(page, scopeId), { message: `${scopeId} fixture` })
    .toBe(true);
}

test('every key scope has a fixture or is declared unbound', async ({
  page,
}) => {
  await prepare(page, 'mac');
  await boot(page);
  const scopes = await scopeIds(page);
  for (const { id, bindings } of scopes) {
    expect(
      id in FIXTURES || UNBOUND_SCOPES.includes(id),
      `scope "${id}" needs a fixture in every-shortcut.spec.ts`,
    ).toBe(true);
    if (UNBOUND_SCOPES.includes(id)) {
      expect(bindings, `"${id}" gained bindings: give it a fixture`).toBe(0);
    }
  }
  expect(scopes.map((s) => s.id).sort()).toEqual(
    [...Object.keys(FIXTURES), ...UNBOUND_SCOPES].sort(),
  );
});

for (const platform of PLATFORMS) {
  test.describe(`every chord — ${platform}`, () => {
    test.describe.configure({ mode: 'parallel' });

    for (const scopeId of Object.keys(FIXTURES)) {
      test(`scope ${scopeId}: every chord dispatches its command`, async ({
        page,
      }) => {
        await prepare(page, platform);
        await enter(page, scopeId);
        const chords = await scopeChords(page, scopeId);
        expect(
          chords.length,
          `${scopeId} has no chords on ${platform}`,
        ).toBeGreaterThan(0);
        test.setTimeout(15_000 + chords.length * 4_000);

        for (const [i, { chord, command }] of chords.entries()) {
          await test.step(`${chord} → ${command ?? 'reserved'}`, async () => {
            if (i > 0) await enter(page, scopeId);
            const r = await fireChord(page, chord);
            expect(r.resolved, `${chord} resolves to its own binding`).toEqual({
              kind: 'binding',
              scope: scopeId,
              command,
            });
            if (command === null) {
              expect(r.log, 'a reserved chord runs nothing').toEqual([]);
              expect(r.consumed, 'a reserved chord stays unconsumed').toBe(
                false,
              );
              return;
            }
            const ran = expectedRan(command);
            expect(r.log).toEqual([{ id: command, ran }]);
            expect(r.consumed, 'consumed exactly when it ran').toBe(ran);
            if (ran) await expectEffect(page, scopeId, command);
          });
        }
      });
    }
  });
}

// MENU_COMMANDS entries no native menu item emits. Each still runs its
// command when fired, so the sweep below covers it; the list only keeps
// the menu-parity check honest about the gap.
const NOT_IN_MENU = [
  // Handler added with the worktree browser (#277) but menu_darwin.go
  // never got the item; ⌘E reaches the webview as a keydown instead.
  'menu:worktrees',
];

// The only menu events not named after their command (commands.ts).
const MENU_ALIASES: Record<string, string> = {
  'menu:move-session-forward': 'move-forward',
  'menu:move-session-backward': 'move-backward',
};

// The native menu exists on macOS only (menu_darwin.go).
test('every native-menu event runs its command', async ({ page }) => {
  await prepare(page, 'mac');
  await boot(page);
  const MENU_COMMANDS = await page.evaluate(async () => {
    const url = '/src/app/commands.ts';
    const m = await import(/* @vite-ignore */ url);
    return m.MENU_COMMANDS as Record<string, string>;
  });
  const events = Object.entries(MENU_COMMANDS);
  test.setTimeout(15_000 + events.length * 4_000);
  // The table must hold every event the native menu emits, or a new
  // menu item would go unfired here. The menu is Go; read its source.
  const go = readFileSync(
    join(test.info().project.testDir, '..', '..', '..', 'menu_darwin.go'),
    'utf8',
  );
  const emitted = new Set(
    [...go.matchAll(/"(menu:[a-z-]+)"/g)].map((m) => m[1]),
  );
  // The Switch-to-Session items build their names in a 1…9 loop.
  expect(emitted.delete('menu:switch-')).toBe(true);
  expect(go).toMatch(/for i := 1; i <= 9; i\+\+/);
  for (let i = 1; i <= 9; i++) emitted.add(`menu:switch-${i}`);
  for (const e of NOT_IN_MENU) {
    expect(emitted.has(e), `${e} is in the menu now: drop it`).toBe(false);
    emitted.add(e);
  }
  expect([...emitted].sort()).toEqual(Object.keys(MENU_COMMANDS).sort());
  // A menu item runs the command it is named after, so a remapped entry
  // (menu:zoom-out → zoom-in) fails here; the sweep alone would pass it.
  for (const [event, command] of events) {
    expect(command, `${event} runs the command it names`).toBe(
      MENU_ALIASES[event] ?? event.slice('menu:'.length),
    );
  }

  for (const [i, [event, command]] of events.entries()) {
    await test.step(`${event} → ${command}`, async () => {
      if (i > 0) await boot(page);
      if (command === 'toggle-scroll-debug') {
        // It flips hive.debug and reloads. Keep storage across that one
        // load so the flip can be read back after it.
        await page.evaluate(() =>
          sessionStorage.setItem('hive.sweep.keep', '1'),
        );
        const load = page.waitForEvent('load');
        // The evaluate normally returns before the reload lands; if the
        // reload wins, its context is gone and only the reload is left
        // to check. Any other error is a real failure.
        const r = await fireMenu(page, event).catch((e: Error) => {
          if (/context was destroyed|navigat/i.test(e.message)) return null;
          throw e;
        });
        if (r) expect(r.log).toEqual([{ id: command, ran: true }]);
        await load;
        expect(
          await page.evaluate(() => localStorage.getItem('hive.debug')),
        ).toBe('1');
        return;
      }
      const r = await fireMenu(page, event);
      expect(r.log).toEqual([{ id: command, ran: expectedRan(command) }]);
      if (OPENS[command]) await expectEffect(page, 'app', command);
    });
  }
});

// ---------- real-effect spot checks ----------
//
// Real key presses (not synthetic events) for the shortcuts no other e2e
// spec presses, asserting what the user would see — or, for the ones
// that open OS windows and terminals, the bridge call that would.

const activeId = (page: Page) =>
  page.evaluate(() => window.__hive_state?.activeId);

const activeProject = (page: Page) =>
  page.evaluate(() => {
    const st = window.__hive_state;
    return st?.sessions.find((s) => s.id === st.activeId)?.project_id;
  });

// The mock names added sessions by the name given and ids them itself.
const idOf = async (page: Page, name: string) => {
  const id = await page.evaluate(
    (n) => window.__hive_state?.sessions.find((s) => s.name === n)?.id,
    name,
  );
  if (!id) throw new Error(`no session named ${name}`);
  return id;
};

const bridgeCalls = (page: Page, method: string) =>
  page.evaluate((m) => window.__hive.bridgeCalls?.(m) ?? [], method);

for (const platform of PLATFORMS) {
  const M = platform === 'mac' ? 'Meta' : 'Control';
  // Chords whose spelling differs by platform beyond ⌘/Ctrl.
  const NAV_BACK = platform === 'mac' ? 'Control+Minus' : 'Control+Alt+Minus';
  const NAV_FWD =
    platform === 'mac' ? 'Control+Shift+Minus' : 'Control+Alt+Shift+Minus';
  const GRID =
    platform === 'mac' ? 'Meta+Shift+KeyJ' : 'Control+Alt+Shift+KeyJ';

  test.describe(`spot checks — ${platform}`, () => {
    test.describe.configure({ mode: 'parallel' });
    test.beforeEach(async ({ page }) => {
      await prepare(page, platform);
    });

    test('zoom in grows the font', async ({ page }) => {
      await boot(page);
      const before = await page.evaluate(() => window.__hive_state?.fontSize);
      await page.keyboard.press(`${M}+Equal`);
      await expect
        .poll(() => page.evaluate(() => window.__hive_state?.fontSize))
        .toBeGreaterThan(before ?? Number.POSITIVE_INFINITY);
    });

    test('Ctrl+` opens an OS terminal', async ({ page }) => {
      await boot(page);
      await page.keyboard.press('Control+Backquote');
      await expect
        .poll(() => bridgeCalls(page, 'OpenTerminalAt'))
        .toHaveLength(1);
    });

    test('back and forward walk session history', async ({ page }) => {
      await boot(page);
      const s2 = await idOf(page, 's2');
      const s3 = await idOf(page, 's3');
      await activate(page, s2);
      await activate(page, s3);
      await page.keyboard.press(NAV_BACK);
      await expect.poll(() => activeId(page)).toBe(s2);
      await page.keyboard.press(NAV_FWD);
      await expect.poll(() => activeId(page)).toBe(s3);
    });

    test('the activity grid opens', async ({ page }) => {
      await boot(page);
      await page.keyboard.press(GRID);
      await expect(page.locator('#terms')).toHaveClass(/activity/);
      await expect(page.locator('#terms')).toHaveClass(/grid/);
    });

    test('duplicate with another tool opens the launcher', async ({ page }) => {
      await boot(page);
      // Duplicating needs a directory to start the copy in.
      await page.evaluate(() => {
        const p = window.__hive.state?.projects.find((x) => x.id === 'p1');
        if (!p) throw new Error('no p1');
        p.cwd = '/tmp/hive-e2e';
        window.__hive.emit(
          'project:event',
          JSON.stringify({ kind: 'updated', project: p }),
        );
      });
      await page.keyboard.press(`${M}+Shift+KeyP`);
      await expect.poll(() => scopeActive(page, 'launcher')).toBe(true);
    });

    test('new window asks Go for one', async ({ page }) => {
      await boot(page);
      await page.keyboard.press(`${M}+Shift+KeyN`);
      await expect
        .poll(() => bridgeCalls(page, 'OpenNewWindow'))
        .toHaveLength(1);
    });

    test('close window asks Go to close it', async ({ page }) => {
      await boot(page);
      await page.keyboard.press(`${M}+Shift+KeyW`);
      await expect.poll(() => bridgeCalls(page, 'CloseWindow')).toHaveLength(1);
    });

    test('delete project removes it', async ({ page }) => {
      await boot(page);
      // The mock's Confirm accepts (wails-mock.ts Confirm).
      await page.keyboard.press(`${M}+Shift+Backspace`);
      await expect
        .poll(() =>
          page.evaluate(() => window.__hive_state?.projects.map((p) => p.id)),
        )
        .not.toContain('p1');
    });

    test('next-attention jumps to the bell, jump-back returns', async ({
      page,
    }) => {
      await boot(page);
      const s2 = await idOf(page, 's2');
      await page.evaluate((id) => window.__hive.ringBell?.(id), s2);
      await page.keyboard.press(`${M}+KeyB`);
      await expect.poll(() => activeId(page)).toBe(s2);
      await page.keyboard.press(`${M}+Shift+KeyB`);
      await expect.poll(() => activeId(page)).toBe('s1');
    });

    test('previous and next project', async ({ page }) => {
      await boot(page);
      await page.evaluate(async () => {
        const p = {
          id: 'p2',
          name: 'other',
          color: '#f80',
          cwd: '',
          order: 1,
          created: new Date().toISOString(),
        };
        window.__hive.state?.projects.push(p);
        window.__hive.emit(
          'project:event',
          JSON.stringify({ kind: 'added', project: p }),
        );
        await window.__hive.addSession?.('t1', undefined, 'p2');
      });
      await activate(page, 's1');
      await page.keyboard.press(`${M}+BracketRight`);
      await expect.poll(() => activeProject(page)).toBe('p2');
      await page.keyboard.press(`${M}+BracketLeft`);
      await expect.poll(() => activeProject(page)).toBe('p1');
    });

    test('switch to sessions 3 through 9', async ({ page }) => {
      await boot(page, 9);
      const order = await page
        .locator('#projects .hv-session-row')
        .evaluateAll((rows) => rows.map((r) => r.getAttribute('data-sid')));
      expect(order).toHaveLength(9);
      for (let n = 3; n <= 9; n++) {
        await page.keyboard.press(`${M}+Digit${n}`);
        await expect.poll(() => activeId(page)).toBe(order[n - 1]);
      }
    });

    test('the reserved redo chord runs nothing', async ({ page }) => {
      await boot(page);
      await page.evaluate(() => {
        if (window.__hive_commandLog) window.__hive_commandLog.length = 0;
      });
      await page.keyboard.press(`${M}+Shift+KeyZ`);
      expect(await page.evaluate(() => window.__hive_commandLog)).toEqual([]);
      expect(await activeId(page)).toBe('s1');
      expect(await page.evaluate(() => window.__hive_state?.view)).toBe(
        'single',
      );
    });
  });
}
