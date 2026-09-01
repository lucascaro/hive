import { test, expect, type Page } from '@playwright/test';

// Focus containment for every modal surface, in a real browser.
//
// jsdom cannot answer these questions: it has no focus model worth the
// name, so a DOM test can assert `defaultPrevented` and nothing more.
// The bug that prompted this spec was invisible to that — the trap
// worked once focus was inside the dialog, but a dialog opened over a
// terminal starts with focus elsewhere, so the first Tab walked out
// into the page behind it.
//
// The stakes: the next tab stop behind a modal is a hidden terminal's
// textarea. Focus escaping means the user types into a session they
// cannot see.
//
// Every modal that claims aria-modal is covered here, plus the two
// list popups (launcher, command palette) which use Tab for selection
// instead of trapping.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

// activeInside reports whether focus is anywhere within selector.
async function activeInside(page: Page, selector: string): Promise<boolean> {
  return page.evaluate(
    (sel) => !!document.activeElement?.closest(sel),
    selector,
  );
}

// The id/label of whatever holds focus — enough to assert movement
// without pinning exact DOM structure.
async function activeTag(page: Page): Promise<string> {
  return page.evaluate(() => {
    const el = document.activeElement as HTMLElement | null;
    if (!el) return 'none';
    return `${el.tagName.toLowerCase()}#${el.id || ''}.${el.className || ''}`.trim();
  });
}

// tabAround presses Tab n times and asserts focus never leaves
// selector. n is deliberately larger than the number of focusables so
// the wrap is exercised several times over.
async function tabAround(page: Page, selector: string, n: number) {
  for (let i = 0; i < n; i++) {
    await page.keyboard.press('Tab');
    expect(
      await activeInside(page, selector),
      `focus escaped ${selector} after ${i + 1} Tab press(es) — landed on ${await activeTag(page)}`,
    ).toBe(true);
  }
}

async function shiftTabAround(page: Page, selector: string, n: number) {
  for (let i = 0; i < n; i++) {
    await page.keyboard.press('Shift+Tab');
    expect(
      await activeInside(page, selector),
      `focus escaped ${selector} after ${i + 1} Shift+Tab press(es) — landed on ${await activeTag(page)}`,
    ).toBe(true);
  }
}

// ---------- the choice dialog ----------

// Opened from the worktree browser. This is the exact path that was
// broken: the dialog appears while focus is still elsewhere.
async function openDeleteDialog(page: Page) {
  await page.evaluate(() =>
    window.__hive.seedWorktrees?.([
      { path: '/mock/.worktrees/trapped', branch: 'trapped' },
    ]),
  );
  await page.keyboard.press(`${mod}+e`);
  await page
    .locator('#worktrees-list .worktree-row[data-path$="/trapped"]')
    .getByRole('button', { name: 'Delete' })
    .click();
  await expect(page.locator('.choice-dialog')).toBeVisible();
}

test('choice dialog: focus starts inside it', async ({ page }) => {
  await boot(page);
  await openDeleteDialog(page);
  expect(await activeInside(page, '.choice-dialog')).toBe(true);
  // ...and on the safe option, so a stray Enter cannot destroy anything.
  await expect(
    page.locator('.choice-dialog button[data-choice="cancel"]'),
  ).toBeFocused();
});

test('choice dialog: Tab cycles and never escapes', async ({ page }) => {
  await boot(page);
  await openDeleteDialog(page);
  await tabAround(page, '.choice-dialog', 8);
});

test('choice dialog: Shift+Tab cycles and never escapes', async ({ page }) => {
  await boot(page);
  await openDeleteDialog(page);
  await shiftTabAround(page, '.choice-dialog', 8);
});

// The regression this spec exists for: with focus deliberately moved
// out of the dialog, the very next Tab must pull it back in rather
// than continue through the page.
test('choice dialog: Tab reclaims focus that started outside it', async ({
  page,
}) => {
  await boot(page);
  await openDeleteDialog(page);
  await page.evaluate(() => (document.activeElement as HTMLElement)?.blur());
  expect(await activeInside(page, '.choice-dialog')).toBe(false);

  await page.keyboard.press('Tab');
  expect(await activeInside(page, '.choice-dialog')).toBe(true);
});

test('choice dialog: visits every button as Tab goes round', async ({
  page,
}) => {
  await boot(page);
  await openDeleteDialog(page);
  const seen = new Set<string>();
  for (let i = 0; i < 6; i++) {
    seen.add(
      await page.evaluate(
        () => (document.activeElement as HTMLElement)?.dataset.choice ?? '',
      ),
    );
    await page.keyboard.press('Tab');
  }
  expect([...seen].sort()).toEqual(['both', 'cancel', 'keep-branch']);
});

// A dialog is a detour: closing it should put the user back on the
// control they invoked it from, not adrift on <body> with the panel's
// trap about to grab the first thing it finds.
test('choice dialog: cancelling returns focus to the button that opened it', async ({
  page,
}) => {
  await boot(page);
  await openDeleteDialog(page);
  await page.locator('.choice-dialog button[data-choice="cancel"]').click();
  await expect(page.locator('.choice-dialog')).toHaveCount(0);

  await expect(
    page
      .locator('#worktrees-list .worktree-row[data-path$="/trapped"]')
      .getByRole('button', { name: 'Delete' }),
  ).toBeFocused();
});

test('choice dialog: Escape also returns focus to the opener', async ({
  page,
}) => {
  await boot(page);
  await openDeleteDialog(page);
  await page.keyboard.press('Escape');
  await expect(page.locator('.choice-dialog')).toHaveCount(0);

  await expect(
    page
      .locator('#worktrees-list .worktree-row[data-path$="/trapped"]')
      .getByRole('button', { name: 'Delete' }),
  ).toBeFocused();
});

// Tab must keep working straight after the round trip — a restore that
// left focus somewhere detached would strand the panel's trap.
test('choice dialog: the panel keeps the keyboard after a cancel', async ({
  page,
}) => {
  await boot(page);
  await openDeleteDialog(page);
  await page.keyboard.press('Escape');
  await tabAround(page, '#worktrees', 8);
});

// The opener frequently does not survive the answer: deleting a
// worktree removes the row the Delete button lived on. That must not
// throw or leave focus on a detached node.
test('choice dialog: survives its opener being removed by the answer', async ({
  page,
}) => {
  await boot(page);
  await openDeleteDialog(page);
  await page
    .locator('.choice-dialog button[data-choice="keep-branch"]')
    .click();

  await expect(
    page.locator('#worktrees-list .worktree-row[data-path$="/trapped"]'),
  ).toHaveCount(0);
  // Focus is not on a node that has left the document.
  expect(
    await page.evaluate(() => document.activeElement?.isConnected !== false),
  ).toBe(true);
  await tabAround(page, '#worktrees', 6);
});

// ---------- the worktree browser ----------

test('worktree browser: Tab never escapes the panel', async ({ page }) => {
  await boot(page);
  await page.evaluate(() =>
    window.__hive.seedWorktrees?.(
      [{ path: '/mock/.worktrees/a', branch: 'a' }],
      [{ name: 'orphan-b' }],
    ),
  );
  await page.keyboard.press(`${mod}+e`);
  await expect(page.locator('#worktrees')).toBeVisible();
  await tabAround(page, '#worktrees', 12);
});

test('worktree browser: Shift+Tab never escapes the panel', async ({
  page,
}) => {
  await boot(page);
  await page.evaluate(() =>
    window.__hive.seedWorktrees?.([
      { path: '/mock/.worktrees/a', branch: 'a' },
    ]),
  );
  await page.keyboard.press(`${mod}+e`);
  await expect(page.locator('#worktrees')).toBeVisible();
  await shiftTabAround(page, '#worktrees', 10);
});

// ---------- inline rename ----------
//
// An edit is a detour like a dialog: finishing it should return focus
// to the control that started it, not leave it on <body> for the
// panel's trap to grab at random.

async function startWorktreeRename(page: Page) {
  await page.evaluate(() =>
    window.__hive.seedWorktrees?.([
      { path: '/mock/.worktrees/renameme', branch: 'renameme' },
    ]),
  );
  await page.keyboard.press(`${mod}+e`);
  await page
    .locator('#worktrees-list .worktree-row[data-path$="/renameme"]')
    .getByRole('button', { name: 'Rename' })
    .click();
  await expect(
    page.locator('#worktrees-list input.worktree-rename'),
  ).toBeFocused();
}

const renameButton = (page: Page) =>
  page
    .locator('#worktrees-list .worktree-row[data-path$="/renameme"]')
    .getByRole('button', { name: 'Rename' });

test('rename: Escape returns focus to the Rename button', async ({ page }) => {
  await boot(page);
  await startWorktreeRename(page);
  await page.keyboard.press('Escape');
  await expect(
    page.locator('#worktrees-list input.worktree-rename'),
  ).toHaveCount(0);
  await expect(renameButton(page)).toBeFocused();
});

test('rename: the panel keeps the keyboard after cancelling', async ({
  page,
}) => {
  await boot(page);
  await startWorktreeRename(page);
  await page.keyboard.press('Escape');
  await tabAround(page, '#worktrees', 8);
});

// Escape twice: the first cancels the edit and lands on the button,
// the second closes the panel — neither drops focus into the page.
test('rename: Escape twice cancels then closes, without stranding focus', async ({
  page,
}) => {
  await boot(page);
  await startWorktreeRename(page);
  await page.keyboard.press('Escape');
  await expect(page.locator('#worktrees')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('#worktrees')).toBeHidden();
  expect(
    await page.evaluate(() => document.activeElement?.isConnected !== false),
  ).toBe(true);
});

// ---------- settings ----------

test('settings: Tab walks the form and wraps at the ends', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();
  await tabAround(page, '#settings', 12);
});

test('settings: Shift+Tab wraps backwards without escaping', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();
  await shiftTabAround(page, '#settings', 10);
});

// A sidebar re-render must not reach into an open dialog. renderSidebar
// wraps its rebuild in preserveFocus (src/lib/preserve-focus.ts, from
// main), and the daemon emits session:event `updated` on every phase
// step — up to 30s after a spawn. If that restore fired while a modal
// held the keyboard, typing would jump out of the dialog mid-edit.
test('a sidebar re-render does not steal focus from an open dialog', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();
  // A row has to exist before anything in it can hold focus.
  await page.locator('#settings-agent-add').click();
  await page.locator('.settings-agent-name').first().focus();

  await page.evaluate(() => {
    const s = window.__hive.state?.sessions[0];
    if (!s) throw new Error('no seeded session');
    window.__hive.emit(
      'session:event',
      JSON.stringify({ kind: 'updated', session: s }),
    );
  });

  await expect
    .poll(() =>
      page.evaluate(
        () =>
          document
            .getElementById('settings')
            ?.contains(document.activeElement) ?? false,
      ),
    )
    .toBe(true);
});

// ---------- project editor ----------

// The editor claims aria-modal="true" (the dialog primitive sets it), so
// Tab must actually stay inside. It had no containment test before the
// migration because it was a bare role="dialog" that trapped nothing.
test('project editor: Tab stays inside the dialog', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+n`);
  await expect(page.locator('#project-editor')).toBeVisible();
  await tabAround(page, '#project-editor', 10);
});

// ---------- help overlay ----------

test('help overlay: Tab stays on its single control', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+/`);
  await expect(page.locator('#help-overlay')).toBeVisible();
  await tabAround(page, '#help-overlay', 6);
});

// ---------- list popups (Tab means "next item", not "trap") ----------

test('launcher: Tab moves the selection instead of leaving', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+t`);
  await expect(page.locator('#launcher')).toBeVisible();
  const before = await page
    .locator('#launcher .launcher-list > *[data-selected]')
    .textContent();
  await page.keyboard.press('Tab');
  const after = await page
    .locator('#launcher .launcher-list > *[data-selected]')
    .textContent();
  expect(after).not.toBe(before);
  await expect(page.locator('#launcher')).toBeVisible();
});

test('command palette: Tab moves the selection instead of leaving', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+Shift+k`);
  await expect(page.locator('#command-palette')).toBeVisible();
  await page.keyboard.press('Tab');
  await expect(page.locator('#command-palette')).toBeVisible();
});

// ---------- interaction between the two layers ----------

// A dialog opened over the worktree browser must own the keyboard; Tab
// must not fall through to the panel underneath.
test('a dialog over the browser traps to the dialog, not the panel', async ({
  page,
}) => {
  await boot(page);
  await openDeleteDialog(page);
  await tabAround(page, '.choice-dialog', 6);
  // Still inside the dialog specifically, not merely inside #worktrees.
  expect(await activeInside(page, '.choice-dialog')).toBe(true);
});

// Once the dialog closes, the panel takes the keyboard back.
test('closing the dialog returns the trap to the browser', async ({ page }) => {
  await boot(page);
  await openDeleteDialog(page);
  await page.keyboard.press('Escape');
  await expect(page.locator('.choice-dialog')).toHaveCount(0);
  await expect(page.locator('#worktrees')).toBeVisible();
  await tabAround(page, '#worktrees', 8);
});

// ---------- closing never strands the keyboard ----------
//
// Every modal, swept: after it closes, focus must be on a live element
// that is NOT <body> and NOT inside the modal that just went away.
// Both failure modes were real — the worktree panel left focus on
// <body> (so the next Tab restarted from the top of the page), and the
// project editor could leave it on a field inside a display:none
// dialog, sending keystrokes somewhere invisible.

interface ModalCase {
  name: string;
  selector: string;
  open: (page: Page) => Promise<void>;
}

const MODALS: ModalCase[] = [
  {
    name: 'launcher',
    selector: '#launcher',
    open: (page) => page.keyboard.press(`${mod}+t`),
  },
  {
    name: 'command palette',
    selector: '#command-palette',
    open: (page) => page.keyboard.press(`${mod}+Shift+k`),
  },
  {
    name: 'settings',
    selector: '#settings',
    open: (page) => page.keyboard.press(`${mod}+,`),
  },
  {
    name: 'help overlay',
    selector: '#help-overlay',
    open: (page) => page.keyboard.press(`${mod}+/`),
  },
  {
    name: 'project editor',
    selector: '#project-editor',
    open: (page) => page.keyboard.press(`${mod}+n`),
  },
  {
    name: 'worktree browser',
    selector: '#worktrees',
    open: (page) => page.keyboard.press(`${mod}+e`),
  },
];

for (const m of MODALS) {
  test(`${m.name}: closing returns the keyboard to the terminal`, async ({
    page,
  }) => {
    await boot(page);
    await m.open(page);
    await expect(page.locator(m.selector)).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.locator(m.selector)).toBeHidden();

    // Polled, not sampled: the refocus is deferred a frame, so reading
    // document.activeElement the instant the modal hides is a race.
    await expect
      .poll(
        () =>
          page.evaluate(
            () =>
              (document.activeElement as HTMLElement | null)?.className ?? '',
          ),
        { message: 'focus never returned to the terminal', timeout: 3000 },
      )
      .toContain('xterm-helper-textarea');

    const landed = await page.evaluate((sel) => {
      const el = document.activeElement as HTMLElement | null;
      return {
        isBody: !el || el === document.body,
        connected: !!el?.isConnected,
        insideClosedModal: !!el?.closest(sel),
      };
    }, m.selector);

    expect(landed.isBody, 'focus was stranded on <body>').toBe(false);
    expect(landed.connected, 'focus landed on a detached node').toBe(true);
    expect(
      landed.insideClosedModal,
      'focus was left inside the modal that just closed — keystrokes go somewhere invisible',
    ).toBe(false);
  });

  // Closing must not just look right — the keyboard has to work again.
  test(`${m.name}: app shortcuts work again after closing`, async ({
    page,
  }) => {
    await boot(page);
    await m.open(page);
    await expect(page.locator(m.selector)).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.locator(m.selector)).toBeHidden();

    // The help overlay is reachable from anywhere and closes cleanly,
    // so it is a safe probe for "did global bindings come back".
    await page.keyboard.press(`${mod}+/`);
    await expect(page.locator('#help-overlay')).toBeVisible();
  });

  // Opening and immediately closing is the race that broke the project
  // editor: a deferred focus landed after the close.
  test(`${m.name}: open-then-immediately-close leaves focus sane`, async ({
    page,
  }) => {
    await boot(page);
    await m.open(page);
    await page.keyboard.press('Escape');
    await expect(page.locator(m.selector)).toBeHidden();
    // Give any deferred focus a chance to land where it should not.
    await page.waitForTimeout(50);

    expect(
      await page.evaluate(
        (sel) => !!document.activeElement?.closest(sel),
        m.selector,
      ),
      'a deferred focus landed inside the closed modal',
    ).toBe(false);
  });
}
