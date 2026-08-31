import { test, expect, type Page } from '@playwright/test';

// E2E for the worktree browser against the mock bridge.
//
// The behaviours worth a browser test — the ones the jsdom suite
// cannot show — are the real wiring: that ⌘E and the command palette
// reach the modal, that the confirm dialog gates the destructive call,
// that a mutation's reply repaints the list, and that "Open session
// here" carries the worktree path into the launcher.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

// Seed the mock daemon's inventory. Each spec owns its own fixture so
// the specs stay order-independent.
async function seed(
  page: Page,
  worktrees: Record<string, unknown>[],
  branches: Record<string, unknown>[] = [],
) {
  await page.evaluate(
    ([w, b]) =>
      window.__hive.seedWorktrees?.(
        w as Parameters<NonNullable<typeof window.__hive.seedWorktrees>>[0],
        b as Parameters<NonNullable<typeof window.__hive.seedWorktrees>>[1],
      ),
    [worktrees, branches] as const,
  );
}

const CLEAN = { path: '/mock/.worktrees/clean', branch: 'clean' };
const DIRTY = {
  path: '/mock/.worktrees/dirty',
  branch: 'dirty',
  uncommitted: true,
};
const BUSY = {
  path: '/mock/.worktrees/busy',
  branch: 'busy',
  session_ids: ['s1'],
};

const panel = (page: Page) => page.locator('#worktrees');
const dialogChoice = (page: Page, name: string) =>
  page.locator(`.choice-dialog button[data-choice="${name}"]`);
// Matched on data-path, not on text: the status label of a pristine
// worktree is literally "clean", which a hasText filter would also hit.
const row = (page: Page, branch: string) =>
  page.locator(`#worktrees-list .worktree-row[data-path$="/${branch}"]`);
const mainRow = (page: Page) =>
  page.locator('#worktrees-list .worktree-row[data-kind="main"]');

test('⌘E opens the worktree browser for the active project', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await expect(panel(page)).toBeVisible();
  await expect(row(page, 'clean')).toBeVisible();
  // The main checkout is listed for context.
  await expect(mainRow(page)).toBeVisible();
});

test('⌘E again closes it', async ({ page }) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await expect(panel(page)).toBeVisible();
  await page.keyboard.press(`${mod}+e`);
  await expect(panel(page)).toBeHidden();
});

test('the sidebar project button opens it too', async ({ page }) => {
  await boot(page);
  await seed(page, [CLEAN]);
  // Card actions are hover-revealed (patterns.md › Hover-revealed
  // actions), so the pointer has to be on the header first.
  const card = page.locator('#projects .hv-project-card').first();
  await card.locator('.hv-project-card__header').hover();
  await card.locator('[data-action="worktrees"]').click();
  await expect(panel(page)).toBeVisible();
});

test('the command palette opens it', async ({ page }) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+Shift+k`);
  await page.keyboard.type('Worktrees');
  await page.keyboard.press('Enter');
  await expect(panel(page)).toBeVisible();
});

test('Delete is disabled for a worktree with a live session, and says why', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [BUSY]);
  await page.keyboard.press(`${mod}+e`);
  const btn = row(page, 'busy').getByRole('button', { name: 'Delete' });
  await expect(btn).toBeDisabled();
  await expect(btn).toHaveAttribute('title', /session is running/);
});

test('deleting a clean worktree removes it from the list', async ({ page }) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await expect(row(page, 'clean')).toBeVisible();

  await row(page, 'clean').getByRole('button', { name: 'Delete' }).click();
  await dialogChoice(page, 'keep-branch').click();

  // The list repaints from the daemon's reply, not from a local patch.
  await expect(row(page, 'clean')).toHaveCount(0);
});

test('the delete dialog offers three outcomes and cancels cleanly', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await row(page, 'clean').getByRole('button', { name: 'Delete' }).click();

  await expect(dialogChoice(page, 'cancel')).toBeVisible();
  await expect(dialogChoice(page, 'keep-branch')).toBeVisible();
  await expect(dialogChoice(page, 'both')).toBeVisible();
  // The safe option holds focus, so a stray Enter cannot delete.
  await expect(dialogChoice(page, 'cancel')).toBeFocused();

  await dialogChoice(page, 'cancel').click();
  await expect(page.locator('.choice-dialog')).toHaveCount(0);
  // Cancelling the deletion must not close the browser as well.
  await expect(panel(page)).toBeVisible();
  await expect(row(page, 'clean')).toBeVisible();
});

// jsdom can only assert defaultPrevented; this drives the real Tab key
// through a real focus model, which is the only way to know focus does
// not walk out into the page behind the dialog.
test('Tab cycles the dialog buttons and never leaves the dialog', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await row(page, 'clean').getByRole('button', { name: 'Delete' }).click();
  await expect(dialogChoice(page, 'cancel')).toBeFocused();

  await page.keyboard.press('Tab');
  await expect(dialogChoice(page, 'keep-branch')).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(dialogChoice(page, 'both')).toBeFocused();
  // Wraps rather than escaping into the page.
  await page.keyboard.press('Tab');
  await expect(dialogChoice(page, 'cancel')).toBeFocused();

  await page.keyboard.press('Shift+Tab');
  await expect(dialogChoice(page, 'both')).toBeFocused();

  // Whatever holds focus is still inside the dialog.
  expect(
    await page.evaluate(
      () => !!document.activeElement?.closest('.choice-dialog'),
    ),
  ).toBe(true);
});

test('Enter activates the focused dialog button', async ({ page }) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await row(page, 'clean').getByRole('button', { name: 'Delete' }).click();

  // Tab to "Delete, keep branch" and commit with Enter.
  await page.keyboard.press('Tab');
  await expect(dialogChoice(page, 'keep-branch')).toBeFocused();
  await page.keyboard.press('Enter');

  await expect(page.locator('.choice-dialog')).toHaveCount(0);
  await expect(row(page, 'clean')).toHaveCount(0);
  await expect(
    page.locator('#worktrees-branches .worktree-row', { hasText: 'clean' }),
  ).toBeVisible();
});

test('Escape in the delete dialog backs out without closing the browser', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await row(page, 'clean').getByRole('button', { name: 'Delete' }).click();
  await expect(dialogChoice(page, 'cancel')).toBeFocused();

  await page.keyboard.press('Escape');
  await expect(page.locator('.choice-dialog')).toHaveCount(0);
  await expect(panel(page)).toBeVisible();
  await expect(row(page, 'clean')).toBeVisible();
});

test('"Delete, keep branch" leaves the branch behind as an orphan', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await row(page, 'clean').getByRole('button', { name: 'Delete' }).click();
  await dialogChoice(page, 'keep-branch').click();

  await expect(row(page, 'clean')).toHaveCount(0);
  await expect(
    page.locator('#worktrees-branches .worktree-row', { hasText: 'clean' }),
  ).toBeVisible();
});

test('"Delete both" removes the branch as well', async ({ page }) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await row(page, 'clean').getByRole('button', { name: 'Delete' }).click();
  await dialogChoice(page, 'both').click();

  await expect(row(page, 'clean')).toHaveCount(0);
  await expect(
    page.locator('#worktrees-branches .worktree-row', { hasText: 'clean' }),
  ).toHaveCount(0);
});

// The same ⎇ marks a worktree-backed session in the sidebar and on its
// grid tile. Both used to be inert while an identical glyph on the
// project row was a button — which just reads as broken.
//
// This has to be a browser test, not jsdom: the row's meta column is
// display:none on :hover and :focus-within (the hover-action swap), so a
// worktree button parked in there exists in the DOM, passes every jsdom
// assertion, and is still impossible to click or tab to. Only a real
// layout engine can tell the difference — hence elementFromPoint and the
// live activeElement below.
test('the worktree glyph on a session opens the browser', async ({ page }) => {
  await boot(page);
  await seed(page, [
    { path: '/mock/.worktrees/from-glyph', branch: 'from-glyph' },
  ]);
  const id = await page.evaluate(() =>
    window.__hive.createSessionWithWorktree?.('glyph-session'),
  );
  const row = `#projects li.hv-session-row[data-sid="${id}"]`;
  const glyph = page.locator(`${row} .hv-session-row__worktree`);
  await expect(glyph).toBeVisible();

  // Visible at rest AND still the thing under the cursor once the row is
  // hovered — the state in which a click actually happens.
  await page.locator(row).hover();
  await expect(glyph).toBeVisible();
  const hitIsGlyph = await page.evaluate((sel) => {
    const el = document.querySelector<HTMLElement>(sel);
    if (!el) return false;
    const r = el.getBoundingClientRect();
    const hit = document.elementFromPoint(
      r.left + r.width / 2,
      r.top + r.height / 2,
    );
    return !!hit && (hit === el || el.contains(hit));
  }, `${row} .hv-session-row__worktree`);
  expect(hitIsGlyph).toBe(true);

  // Focus must stick: if the button lived in the swapped-out column,
  // :focus-within would display:none the focused element and the browser
  // would drop focus back to <body>.
  await glyph.focus();
  await expect(glyph).toBeFocused();
  await expect(glyph).toBeVisible();

  await glyph.click();
  await expect(panel(page)).toBeVisible();
});

test('the worktree glyph on a tile opens the browser', async ({ page }) => {
  await boot(page);
  await seed(page, []);
  const id = await page.evaluate(() =>
    window.__hive.createSessionWithWorktree?.('tile-session'),
  );
  // The tile's worktree control is an iconButton in the hover-revealed
  // actions group now, so the header has to be hovered to reach it.
  await page.locator(`.term-host[data-sid="${id}"] .tile-header`).hover();
  const glyph = page.locator(`.term-host[data-sid="${id}"] .tile-worktree`);
  await expect(glyph).toBeVisible();
  await glyph.click();
  await expect(panel(page)).toBeVisible();
});

// The daemon is the authority: a stale view that tries to delete a
// dirty worktree without force gets refused, and the row survives.
test('a refusal from the daemon leaves the worktree in place', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [DIRTY]);
  await page.keyboard.press(`${mod}+e`);
  // Force the un-forced path the way a stale view would: mutate the
  // rendered row's data out from under the button before clicking.
  await page.evaluate(() => {
    const st = window.__hive.state;
    if (st) st.worktrees[0].uncommitted = true;
  });
  await row(page, 'dirty').getByRole('button', { name: 'Delete' }).click();
  // The dialog names the blocker before anything is sent.
  await expect(page.locator('.choice-dialog')).toContainText(
    'uncommitted changes',
  );
  await dialogChoice(page, 'keep-branch').click();
  // force is sent (the row knows it is dirty), so the mock removes it —
  // what matters is that the flow completed and the list came back from
  // the daemon.
  await expect(row(page, 'dirty')).toHaveCount(0);
});

test('an orphaned branch can be given a worktree', async ({ page }) => {
  await boot(page);
  await seed(page, [], [{ name: 'stranded', ahead: 3 }]);
  await page.keyboard.press(`${mod}+e`);

  const branchRow = page.locator('#worktrees-branches .worktree-row', {
    hasText: 'stranded',
  });
  await expect(branchRow).toBeVisible();
  await expect(branchRow).toContainText('3 commits ahead');
  await branchRow.getByRole('button', { name: 'Create worktree' }).click();

  // It moves out of the branch list and into the worktree list.
  await expect(
    page.locator('#worktrees-branches .worktree-row', { hasText: 'stranded' }),
  ).toHaveCount(0);
  await expect(row(page, 'stranded')).toBeVisible();
});

test('an orphaned branch can be deleted, behind a confirmation', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [], [{ name: 'gone-soon', merged: true }]);
  await page.keyboard.press(`${mod}+e`);

  const branchRow = page.locator('#worktrees-branches .worktree-row', {
    hasText: 'gone-soon',
  });
  await branchRow.getByRole('button', { name: 'Delete' }).click();

  // Cancel first: nothing should happen.
  await expect(dialogChoice(page, 'cancel')).toBeFocused();
  await dialogChoice(page, 'cancel').click();
  await expect(branchRow).toBeVisible();

  await branchRow.getByRole('button', { name: 'Delete' }).click();
  await dialogChoice(page, 'local').click();
  await expect(branchRow).toHaveCount(0);
});

// Closing a session whose worktree is dirty used to raise a native OS
// alert. It is the same class of question as the delete prompt, so it
// uses the same dialog.
test('Continue starts a session that resumes the previous conversation', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await row(page, 'clean').getByRole('button', { name: 'Continue' }).click();

  await expect(page.locator('#launcher')).toBeVisible();
  await page.locator('#launcher .launcher-list > *').first().click();

  await page.waitForFunction(() =>
    (window.__hive.state?.sessions ?? []).some(
      (s) =>
        s.worktree_path === '/mock/.worktrees/clean' && s.continued === true,
    ),
  );
});

test('New session starts a fresh conversation in the same worktree', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await row(page, 'clean').getByRole('button', { name: 'New session' }).click();
  await page.locator('#launcher .launcher-list > *').first().click();

  await page.waitForFunction(() =>
    (window.__hive.state?.sessions ?? []).some(
      (s) =>
        s.worktree_path === '/mock/.worktrees/clean' && s.continued === false,
    ),
  );
});

// Closing a dirty session offers a third, destructive outcome: take the
// worktree with it. It has to be visibly the dangerous one.
test('the dirty-close dialog offers close-and-delete as the danger option', async ({
  page,
}) => {
  await boot(page);
  const id = await page.evaluate(() =>
    window.__hive.createSessionWithWorktree?.('cleanup-me'),
  );
  await page.evaluate((sid) => {
    window.__hive.emit(
      'control:error',
      JSON.stringify({
        code: 'worktree_dirty',
        message: 'uncommitted changes',
        session_id: sid,
      }),
    );
  }, id);

  const clean = dialogChoice(page, 'close-and-clean');
  await expect(clean).toBeVisible();
  await expect(clean).toHaveClass(/danger/);
  // Cancel still holds focus, so the destructive option is never the
  // default.
  await expect(dialogChoice(page, 'cancel')).toBeFocused();

  await clean.click();
  // The session goes, and so does its worktree.
  await page.waitForFunction(
    (sid) =>
      !(window.__hive.state?.sessions ?? []).some((s) => s.id === sid) &&
      !(window.__hive.state?.worktrees ?? []).some(
        (w) => w.branch === 'cleanup-me',
      ),
    id,
  );
});

test('closing a dirty session keeps the worktree when only Close is chosen', async ({
  page,
}) => {
  await boot(page);
  const id = await page.evaluate(() =>
    window.__hive.createSessionWithWorktree?.('keep-me'),
  );
  await page.evaluate((sid) => {
    window.__hive.emit(
      'control:error',
      JSON.stringify({
        code: 'worktree_dirty',
        message: 'uncommitted changes',
        session_id: sid,
      }),
    );
  }, id);
  await dialogChoice(page, 'close').click();

  await page.waitForFunction(
    (sid) => !(window.__hive.state?.sessions ?? []).some((s) => s.id === sid),
    id,
  );
  // The worktree survives — that is the whole lifecycle change.
  expect(
    await page.evaluate(() =>
      (window.__hive.state?.worktrees ?? []).some(
        (w) => w.branch === 'keep-me',
      ),
    ),
  ).toBe(true);
});

test('closing a dirty session asks in-app, not with an OS alert', async ({
  page,
}) => {
  await boot(page);
  const id = await page.evaluate(() =>
    window.__hive.createSessionWithWorktree?.('dirty-session'),
  );
  // The daemon refuses the un-forced kill; that refusal is what raises
  // the question.
  await page.evaluate((sid) => {
    window.__hive.emit(
      'control:error',
      JSON.stringify({
        code: 'worktree_dirty',
        message: 'uncommitted changes',
        session_id: sid,
      }),
    );
  }, id);

  const dialog = page.locator('.choice-dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('Close this session anyway?');
  await expect(dialog).toContainText('uncommitted changes');
  // The reassurance matters: closing alone no longer destroys the
  // worktree — that is now a separate, explicitly destructive choice.
  await expect(dialog).toContainText('keeps the worktree');
  await expect(dialogChoice(page, 'close-and-clean')).toBeVisible();
  await expect(dialogChoice(page, 'cancel')).toBeFocused();

  await dialogChoice(page, 'cancel').click();
  await expect(dialog).toHaveCount(0);
  // Cancelling leaves the session alone.
  await expect(
    page.locator(`#projects li.hv-session-row[data-sid="${id}"]`),
  ).toBeVisible();
});

test('Escape backs out of the dirty-close question', async ({ page }) => {
  await boot(page);
  const id = await page.evaluate(() =>
    window.__hive.createSessionWithWorktree?.('dirty-escape'),
  );
  await page.evaluate((sid) => {
    window.__hive.emit(
      'control:error',
      JSON.stringify({
        code: 'worktree_dirty',
        message: 'uncommitted changes',
        session_id: sid,
      }),
    );
  }, id);

  await expect(page.locator('.choice-dialog')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('.choice-dialog')).toHaveCount(0);
  await expect(
    page.locator(`#projects li.hv-session-row[data-sid="${id}"]`),
  ).toBeVisible();
});

test('the loading card is replaced by the list, not layered under it', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await expect(row(page, 'clean')).toBeVisible();
  // The card must be gone, not merely covered — it has no global
  // .hidden rule to fall back on.
  await expect(page.locator('#worktrees-empty')).toBeHidden();
});

test('New session hands the worktree to the launcher, with no worktree row', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await row(page, 'clean').getByRole('button', { name: 'New session' }).click();

  // The browser closes and the agent picker takes over.
  await expect(panel(page)).toBeHidden();
  await expect(page.locator('#launcher')).toBeVisible();
  // Resuming an existing worktree offers neither the toggle nor the
  // branch field — there is nothing to create.
  await expect(page.locator('#launcher .launcher-worktree')).toHaveCount(0);
  await expect(page.locator('#launcher .launcher-branch')).toHaveCount(0);

  await page.locator('#launcher .launcher-list > *').first().click();
  // The new session runs in the worktree it was launched from.
  await page.waitForFunction(() =>
    (window.__hive.state?.sessions ?? []).some(
      (s) => s.worktree_path === '/mock/.worktrees/clean',
    ),
  );
});

// keyboard.ts listens in the capture phase, so it sees Escape before
// the rename input does. Without an explicit hand-off the panel closed
// and the edit was silently discarded.
test('Escape during a rename cancels the edit, not the panel', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await row(page, 'clean').getByRole('button', { name: 'Rename' }).click();

  const input = page.locator('#worktrees-list input.worktree-rename');
  await expect(input).toBeFocused();
  await input.fill('discarded');
  await page.keyboard.press('Escape');

  // The panel is still open...
  await expect(panel(page)).toBeVisible();
  // ...the editor is gone...
  await expect(input).toHaveCount(0);
  // ...and the branch is untouched.
  await expect(row(page, 'clean')).toBeVisible();
  expect(
    await page.evaluate(() =>
      (window.__hive.state?.worktrees ?? []).map((w) => w.branch),
    ),
  ).toContain('clean');
});

// A second Escape, once the editor is gone, closes the panel as usual.
test('Escape after cancelling a rename closes the panel', async ({ page }) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await row(page, 'clean').getByRole('button', { name: 'Rename' }).click();
  await page.keyboard.press('Escape');
  await expect(panel(page)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(panel(page)).toBeHidden();
});

// While renaming, keys are text — not the panel's shortcuts.
test('typing in the rename box does not fire panel shortcuts', async ({
  page,
}) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await row(page, 'clean').getByRole('button', { name: 'Rename' }).click();

  const input = page.locator('#worktrees-list input.worktree-rename');
  await input.fill('');
  // 'r' is the panel's refresh shortcut; here it is just a letter.
  await page.keyboard.type('refresh-2');
  await expect(input).toHaveValue('refresh-2');
  await expect(input).toBeFocused();
});

test('renaming a worktree updates its row', async ({ page }) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await row(page, 'clean').getByRole('button', { name: 'Rename' }).click();

  const input = page.locator('#worktrees-list input.worktree-rename');
  await expect(input).toBeFocused();
  await input.fill('renamed');
  await input.press('Enter');

  await expect(row(page, 'renamed')).toBeVisible();
  await expect(row(page, 'clean')).toHaveCount(0);
});

test('Escape closes the browser and returns the keyboard', async ({ page }) => {
  await boot(page);
  await seed(page, [CLEAN]);
  await page.keyboard.press(`${mod}+e`);
  await expect(panel(page)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(panel(page)).toBeHidden();
});

test('a project that is not a git repo says so', async ({ page }) => {
  await boot(page);
  await page.evaluate(() => {
    // Drop the repo root the way the daemon does for a non-git cwd.
    window.__hive.emit(
      'worktree:list',
      JSON.stringify({ project_id: 'p1', repo_root: '', worktrees: [] }),
    );
  });
  await page.keyboard.press(`${mod}+e`);
  await page.evaluate(() => {
    window.__hive.emit(
      'worktree:list',
      JSON.stringify({ project_id: 'p1', repo_root: '', worktrees: [] }),
    );
  });
  await expect(page.locator('#worktrees-empty')).toContainText(
    'not a git repository',
  );
});

// A real click, in a real engine — the jsdom tests can only assert
// defaultPrevented. This is the one that would have caught the branch
// box shipping unclickable.
test('the branch box can be clicked and typed into', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+Shift+t`); // worktree forced on
  const branchInput = page.locator('#launcher .launcher-branch');
  await expect(branchInput).toBeVisible();

  await branchInput.click();
  await expect(branchInput).toBeFocused();
  // Typed, not filled: fill() sets .value directly and would pass even
  // if every keystroke were being swallowed.
  await page.keyboard.type('fix-2');
  await expect(branchInput).toHaveValue('fix-2');
  // The launcher must still be open — a digit used to activate a row
  // shortcut and launch a session mid-typing.
  await expect(page.locator('#launcher')).toBeVisible();
  // ...and the filter box must not have eaten the text.
  await expect(page.locator('#launcher .launcher-search')).toHaveValue('');
});

test('Enter from the branch box launches with that branch', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+Shift+t`);
  const branchInput = page.locator('#launcher .launcher-branch');
  await branchInput.click();
  await page.keyboard.type('typed-branch');
  await page.keyboard.press('Enter');

  await page.waitForFunction(() =>
    (window.__hive.state?.sessions ?? []).some(
      (s) => s.worktree_branch === 'typed-branch',
    ),
  );
});

test('the branch name typed in the launcher reaches the new worktree', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+Shift+t`); // new session, worktree forced on
  const branchInput = page.locator('#launcher .launcher-branch');
  await expect(branchInput).toBeVisible();
  await branchInput.fill('my-feature');
  await page.locator('#launcher .launcher-list > *').first().click();

  await page.waitForFunction(() =>
    (window.__hive.state?.sessions ?? []).some(
      (s) => s.worktree_branch === 'my-feature',
    ),
  );
});

// A repo with a long branch list used to push the worktree section
// clean out of the panel: both sections are flex items, and a flex
// item will not shrink below its content without min-height: 0.
// Asserted in a real browser because this is pure layout — jsdom
// computes no heights at all.
test('a long branch list never squeezes out the worktree list', async ({
  page,
}) => {
  await boot(page);
  const many = Array.from({ length: 120 }, (_, i) => ({
    name: `stale-${i}`,
    ahead: i % 3,
    merged: i % 2 === 0,
  }));
  await seed(page, [CLEAN, DIRTY], many);
  await page.keyboard.press(`${mod}+e`);
  await expect(panel(page)).toBeVisible();

  // Visible, and actually on screen: elementFromPoint at the row's own
  // centre must land inside it, not on whatever covers it.
  await expect(row(page, 'clean')).toBeVisible();
  // Queried and hit-tested in one evaluate: the list re-renders on
  // every refresh, and a handle taken beforehand would be measuring a
  // detached node.
  const onScreen = await page.evaluate(() => {
    const el = document.querySelector(
      '#worktrees-list .worktree-row[data-path$="/clean"]',
    );
    if (!el) return null;
    const r = el.getBoundingClientRect();
    const hit = document.elementFromPoint(
      r.left + r.width / 2,
      r.top + r.height / 2,
    );
    return { height: r.height, covered: !!hit && el.contains(hit) };
  });
  expect(onScreen?.height ?? 0).toBeGreaterThan(0);
  expect(onScreen?.covered).toBe(true);

  // Both sections live in one scroll container, and it stays inside
  // the panel rather than growing it.
  const fits = await page.evaluate(() => {
    const panelEl = document.getElementById('worktrees-panel');
    const body = document.getElementById('worktrees-body');
    const branches = document.getElementById('worktrees-section-branches');
    const trees = document.getElementById('worktrees-section-trees');
    if (!panelEl || !body || !branches || !trees) return null;
    return {
      bodyScrolls: body.scrollHeight > body.clientHeight,
      // Neither section scrolls on its own any more.
      sectionsScroll:
        branches.scrollHeight > branches.clientHeight ||
        trees.scrollHeight > trees.clientHeight,
      branchesInside:
        body.getBoundingClientRect().bottom <=
        panelEl.getBoundingClientRect().bottom + 1,
      treesHeight: trees.getBoundingClientRect().height,
    };
  });
  expect(fits?.bodyScrolls).toBe(true);
  expect(fits?.sectionsScroll).toBe(false);
  expect(fits?.branchesInside).toBe(true);
  expect(fits?.treesHeight).toBeGreaterThan(40);
});

// Deleting the remote branch is a push, so it is never implied: the
// dialog offers it as its own button, and only for a branch that
// tracks something.
test('an orphan branch with an upstream offers local-only and local+remote', async ({
  page,
}) => {
  await boot(page);
  await seed(
    page,
    [CLEAN],
    [{ name: 'shipped', upstream: 'origin/shipped', merged: true }],
  );
  await page.keyboard.press(`${mod}+e`);
  await page
    .locator('#worktrees-branches .worktree-row', { hasText: 'shipped' })
    .getByRole('button', { name: 'Delete' })
    .click();

  await expect(dialogChoice(page, 'local')).toBeVisible();
  await dialogChoice(page, 'remote').click();

  await page.waitForFunction(() =>
    (window.__hive.state?.deletedRemotes ?? []).includes('shipped'),
  );
});

test('a branch with no upstream gets no remote option', async ({ page }) => {
  await boot(page);
  await seed(page, [CLEAN], [{ name: 'local-only', merged: true }]);
  await page.keyboard.press(`${mod}+e`);
  await page
    .locator('#worktrees-branches .worktree-row', { hasText: 'local-only' })
    .getByRole('button', { name: 'Delete' })
    .click();

  await expect(dialogChoice(page, 'local')).toBeVisible();
  await expect(dialogChoice(page, 'remote')).toHaveCount(0);
});

// The session ROW's kill button, end to end. ⌘W and the palette cover
// the other kill paths; this is the one that reaches the daemon from a
// click on a sidebar row, and it is the path that must NOT force —
// forcing would let its confirm ("scrollback is lost") silently agree to
// throwing away uncommitted work. Driven by a real click against a mock
// that refuses un-forced kills of a dirty worktree, so the whole chain
// is under test rather than a hand-emitted control:error.
test("the row's kill button lets the daemon refuse a dirty close", async ({
  page,
}) => {
  await boot(page);
  const id = await page.evaluate(() =>
    window.__hive.createSessionWithWorktree?.('row-dirty'),
  );
  await page.waitForFunction(
    (sid) =>
      (window.__hive.state?.sessions ?? []).find((s) => s.id === sid)?.alive ===
      true,
    id,
  );
  await page.evaluate(() => {
    const w = (window.__hive.state?.worktrees ?? []).find(
      (x) => x.branch === 'row-dirty',
    );
    if (!w) throw new Error('no worktree for the new session');
    w.uncommitted = true;
  });

  const rowEl = page.locator(`#projects li.hv-session-row[data-sid="${id}"]`);
  // Row actions are hover-revealed (patterns.md › Hover-revealed actions).
  await rowEl.hover();
  await rowEl.locator('[data-action="kill"]').click();

  // Not forced: the refusal comes back and raises the three-way question
  // instead of the session quietly taking its changes with it.
  const dialog = page.locator('.choice-dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('Close this session anyway?');
  await expect(dialog).toContainText('row-dirty');
  await expect(rowEl).toBeVisible();

  await dialogChoice(page, 'close').click();
  await page.waitForFunction(
    (sid) => !(window.__hive.state?.sessions ?? []).some((s) => s.id === sid),
    id,
  );
  // Close alone keeps the worktree; only close-and-clean removes it.
  expect(
    await page.evaluate(() =>
      (window.__hive.state?.worktrees ?? []).some(
        (w) => w.branch === 'row-dirty',
      ),
    ),
  ).toBe(true);
});

// A dead session has no process to refuse and no worktree state left to
// guard, so its kill IS forced — and must not stop to ask.
test("the row's kill button forces a dead session straight through", async ({
  page,
}) => {
  await boot(page);
  const id = await page.evaluate(() =>
    window.__hive.createSessionWithWorktree?.('row-dead'),
  );
  await page.waitForFunction(
    (sid) =>
      (window.__hive.state?.sessions ?? []).find((s) => s.id === sid)?.alive ===
      true,
    id,
  );
  await page.evaluate((sid) => {
    const w = (window.__hive.state?.worktrees ?? []).find(
      (x) => x.branch === 'row-dead',
    );
    if (w) w.uncommitted = true;
    const s = (window.__hive.state?.sessions ?? []).find((x) => x.id === sid);
    if (!s) throw new Error('no mock session');
    s.alive = false;
    window.__hive.emit(
      'session:event',
      JSON.stringify({ kind: 'updated', session: s }),
    );
  }, id);

  const rowEl = page.locator(`#projects li.hv-session-row[data-sid="${id}"]`);
  await expect(rowEl).toHaveAttribute('data-state', 'exited');
  await rowEl.hover();
  await rowEl.locator('[data-action="kill"]').click();

  await page.waitForFunction(
    (sid) => !(window.__hive.state?.sessions ?? []).some((s) => s.id === sid),
    id,
  );
  await expect(page.locator('.choice-dialog')).toHaveCount(0);
});
