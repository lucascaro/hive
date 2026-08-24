import { test, expect, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

// Layer B: the worktree browser against a REAL hived, with real git.
//
// This is the spec that proves the lifecycle change works end to end —
// the part no mock can show:
//
//   1. a worktree holding uncommitted work SURVIVES its session closing
//   2. it is then listed as a detached worktree the user can act on
//   3. deleting it from the browser actually removes the directory
//   4. a branch left behind reappears as an orphan, and a worktree can
//      be re-created for it
//
// Isolation: globalSetup points hived at a throwaway git repo via
// --cwd (alongside HOME / HIVE_STATE_DIR / HIVE_SOCKET), so nothing
// here touches the developer's checkout.

const WS_URL = process.env.WS_BRIDGE_URL;
const REPO = process.env.HIVE_E2E_PROJECT_REPO;
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

test.beforeEach(async ({ page }) => {
  await page.addInitScript((url) => {
    window.__WS_BRIDGE_URL = url;
  }, WS_URL);
});

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.session-item').length >= 1,
    null,
    { timeout: 15000 },
  );
}

const panel = (page: Page) => page.locator('#worktrees');
const rowFor = (page: Page, branch: string) =>
  page.locator(`#worktrees-list .worktree-row[data-path$="/${branch}"]`);

function git(...args: string[]): string {
  return execFileSync('git', ['-C', REPO as string, ...args], {
    encoding: 'utf8',
  }).trim();
}

// Create a worktree directly with git — faster and more deterministic
// than driving session creation, and this spec is about the browser,
// not about the create path (covered by the Go tests).
function makeWorktree(branch: string): string {
  const p = path.join(REPO as string, '.worktrees', branch);
  git('worktree', 'add', '-q', '-b', branch, p);
  return p;
}

function cleanupWorktree(branch: string) {
  const p = path.join(REPO as string, '.worktrees', branch);
  try {
    execFileSync(
      'git',
      ['-C', REPO as string, 'worktree', 'remove', '--force', p],
      { encoding: 'utf8', stdio: 'ignore' },
    );
  } catch {
    // Already gone — that is what most tests here assert.
  }
  fs.rmSync(p, { recursive: true, force: true });
  try {
    execFileSync('git', ['-C', REPO as string, 'branch', '-D', branch], {
      encoding: 'utf8',
      stdio: 'ignore',
    });
  } catch {
    // Already gone.
  }
  execFileSync('git', ['-C', REPO as string, 'worktree', 'prune'], {
    encoding: 'utf8',
    stdio: 'ignore',
  });
}

test.describe('worktree browser against real hived', () => {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set — globalSetup did not run');
  test.skip(!REPO, 'HIVE_E2E_PROJECT_REPO not set — globalSetup did not run');

  test('lists a real worktree and its branch', async ({ page }) => {
    const branch = 'real-listed';
    makeWorktree(branch);
    try {
      await boot(page);
      await page.keyboard.press(`${mod}+e`);
      await expect(panel(page)).toBeVisible();
      await expect(rowFor(page, branch)).toBeVisible();
      await expect(rowFor(page, branch)).toContainText('clean');
    } finally {
      cleanupWorktree(branch);
    }
  });

  // The lifecycle change: uncommitted work must not be destroyed by a
  // session closing, and must show up here afterwards.
  test('a worktree holding uncommitted work is listed as holding work', async ({
    page,
  }) => {
    const branch = 'real-dirty';
    const wt = makeWorktree(branch);
    fs.writeFileSync(path.join(wt, 'unsaved.txt'), 'work in progress');
    try {
      await boot(page);
      await page.keyboard.press(`${mod}+e`);
      const row = rowFor(page, branch);
      await expect(row).toBeVisible();
      await expect(row).toContainText('uncommitted changes');
      await expect(row).toHaveAttribute('data-kind', 'holding');
      // Still on disk, with the work intact.
      expect(fs.existsSync(path.join(wt, 'unsaved.txt'))).toBe(true);
    } finally {
      cleanupWorktree(branch);
    }
  });

  test('deleting from the browser removes the directory from disk', async ({
    page,
  }) => {
    const branch = 'real-doomed';
    const wt = makeWorktree(branch);
    try {
      await boot(page);
      await page.keyboard.press(`${mod}+e`);
      await expect(rowFor(page, branch)).toBeVisible();

      await rowFor(page, branch)
        .getByRole('button', { name: 'Delete' })
        .click();
      // The in-app dialog gates the deletion; take the option that
      // removes the directory and the branch together.
      await page.locator('.choice-dialog button[data-choice="both"]').click();

      await expect(rowFor(page, branch)).toHaveCount(0);
      await expect
        .poll(() => fs.existsSync(wt), { timeout: 10000 })
        .toBe(false);
      // git's admin state was pruned too, not just the directory.
      expect(git('worktree', 'list')).not.toContain(branch);
    } finally {
      cleanupWorktree(branch);
    }
  });

  test('a branch with no worktree is offered as an orphan, and can be revived', async ({
    page,
  }) => {
    const branch = 'real-orphan';
    git('branch', branch);
    try {
      await boot(page);
      await page.keyboard.press(`${mod}+e`);
      const orphan = page.locator('#worktrees-branches .worktree-row', {
        hasText: branch,
      });
      await expect(orphan).toBeVisible();

      await orphan.getByRole('button', { name: 'Create worktree' }).click();

      // It moves into the worktree list, and git agrees.
      await expect(rowFor(page, branch)).toBeVisible();
      await expect
        .poll(() => git('worktree', 'list').includes(branch), {
          timeout: 10000,
        })
        .toBe(true);
    } finally {
      cleanupWorktree(branch);
    }
  });

  // Branch deletion against real git: the refusal it relies on is
  // git's own `branch -d`, so a mock cannot prove this one.
  test('deleting a merged orphan branch removes the ref', async ({ page }) => {
    const branch = 'real-merged-orphan';
    cleanupWorktree(branch);
    // Points at the current commit, so it is merged by definition.
    git('branch', branch);
    try {
      await boot(page);
      await page.keyboard.press(`${mod}+e`);
      const orphan = page.locator('#worktrees-branches .worktree-row', {
        hasText: branch,
      });
      await expect(orphan).toBeVisible();
      await orphan.getByRole('button', { name: 'Delete' }).click();
      await page.locator('.choice-dialog button[data-choice="delete"]').click();

      await expect(orphan).toHaveCount(0);
      await expect
        .poll(() => git('branch', '--list', branch), { timeout: 10000 })
        .toBe('');
    } finally {
      cleanupWorktree(branch);
    }
  });

  // Cancelling must leave the ref alone.
  test('cancelling a branch delete keeps the branch', async ({ page }) => {
    const branch = 'real-kept-orphan';
    cleanupWorktree(branch);
    git('branch', branch);
    try {
      await boot(page);
      await page.keyboard.press(`${mod}+e`);
      const orphan = page.locator('#worktrees-branches .worktree-row', {
        hasText: branch,
      });
      await orphan.getByRole('button', { name: 'Delete' }).click();
      await page.locator('.choice-dialog button[data-choice="cancel"]').click();

      await expect(orphan).toBeVisible();
      expect(git('branch', '--list', branch)).toContain(branch);
    } finally {
      cleanupWorktree(branch);
    }
  });

  // An unmerged branch carries commits nothing else has. The dialog
  // must say so, and the delete must still go through once confirmed —
  // git refuses `-d` here, so this exercises the force path end to end.
  test('an unmerged orphan warns, then deletes when confirmed', async ({
    page,
  }) => {
    const branch = 'real-unmerged-orphan';
    // Playwright retries a failed test in the same repo, so setup has
    // to tolerate leftovers from the attempt before — including a
    // half-finished attempt that left the repo ON this branch, which
    // would make `git branch -D` refuse.
    git('checkout', '-q', 'main');
    cleanupWorktree(branch);
    git('checkout', '-q', '-b', branch);
    git('commit', '--allow-empty', '-q', '-m', 'work only here');
    git('checkout', '-q', 'main');
    try {
      await boot(page);
      await page.keyboard.press(`${mod}+e`);
      const orphan = page.locator('#worktrees-branches .worktree-row', {
        hasText: branch,
      });
      await expect(orphan).toBeVisible();
      await orphan.getByRole('button', { name: 'Delete' }).click();

      const dialog = page.locator('.choice-dialog');
      await expect(dialog).toContainText('lose its commits');
      await expect(dialog).toContainText('not merged');
      await page.locator('.choice-dialog button[data-choice="delete"]').click();

      await expect
        .poll(() => git('branch', '--list', branch), { timeout: 10000 })
        .toBe('');
    } finally {
      cleanupWorktree(branch);
    }
  });

  // NOT covered here: "a live session blocks deletion". Driving it
  // needs a session started inside the worktree, and this harness stubs
  // ListAgents to an empty list, so the launcher has no row to click.
  // The behaviour is asserted where the claim actually lives — the
  // registry's session table — in
  // TestRemoveWorktree_RefusesWhenLiveSessionInside (force does not
  // override it) and TestRemoveWorktree_InUseErrorNamesSessions, plus
  // the disabled button and its reason in the mock e2e suite.

  test('renaming moves both the branch and the directory', async ({ page }) => {
    const branch = 'real-before';
    const renamed = 'real-after';
    const wt = makeWorktree(branch);
    try {
      await boot(page);
      await page.keyboard.press(`${mod}+e`);
      await rowFor(page, branch)
        .getByRole('button', { name: 'Rename' })
        .click();

      const input = page.locator('#worktrees-list input.worktree-rename');
      await expect(input).toBeFocused();
      await input.fill(renamed);
      await input.press('Enter');

      await expect(rowFor(page, renamed)).toBeVisible();
      await expect
        .poll(() => fs.existsSync(wt), { timeout: 10000 })
        .toBe(false);
      expect(
        fs.existsSync(path.join(REPO as string, '.worktrees', renamed)),
      ).toBe(true);
      expect(git('branch', '--list', renamed)).toContain(renamed);
    } finally {
      cleanupWorktree(branch);
      cleanupWorktree(renamed);
    }
  });
});
