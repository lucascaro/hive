import { test, expect, type Page } from '@playwright/test';

// Layer C: payload-shape parity. The real hived daemon emits JSON
// with snake_case keys (SessionInfo.project_id, .worktree_branch,
// .worktree_path); some Wails / consumer paths historically emitted
// camelCase. main.ts reads both via `snake_case ?? camelCase` (or
// vice versa). These tests pin BOTH shapes so a future refactor
// that drops one branch surfaces here, not in production where a
// daemon update silently breaks the sidebar.
//
// Project memory note: "Hive wire payloads use snake_case in JS —
// prefer snake_case ?? camelCase when reading SessionInfo/ProjectInfo."

const SHAPES = [
  {
    kind: 'snake_case',
    shape: (id: string) => ({
      id,
      name: `shape-${id}`,
      color: '#0af',
      order: 1,
      created: new Date().toISOString(),
      alive: true,
      agent: '',
      project_id: 'p1',
      worktree_path: `/wt/${id}`,
      worktree_branch: `feat/${id}`,
      last_error: '',
    }),
  },
  {
    kind: 'camelCase',
    shape: (id: string) => ({
      id,
      name: `shape-${id}`,
      color: '#0af',
      order: 1,
      created: new Date().toISOString(),
      alive: true,
      agent: '',
      projectId: 'p1',
      worktreePath: `/wt/${id}`,
      worktreeBranch: `feat/${id}`,
      lastError: '',
    }),
  },
];

async function bootMinimal(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

for (const { kind, shape } of SHAPES) {
  test(`sidebar renders ${kind} SessionInfo`, async ({ page }) => {
    await bootMinimal(page);

    // Inject a session via session:event (the same path hived uses
    // for broadcasts) with the shape under test.
    const id = `inject-${kind}`;
    await page.evaluate((info) => {
      window.__hive.state?.sessions.push(info);
      window.__hive.emit(
        'session:event',
        JSON.stringify({ kind: 'added', session: info }),
      );
    }, shape(id));

    // The session appears in the sidebar (its row is keyed by id).
    const row = page.locator(`#projects li.hv-session-row[data-sid="${id}"]`);
    await expect(row).toBeVisible({ timeout: 2000 });
    // Name renders.
    await expect(row).toContainText(`shape-${id}`);
    // The row's worktree control (ui/session-row.ts) is
    // rendered iff `s.worktreeBranch ?? s.worktree_branch` resolves —
    // this is the read-both pattern under test.
    await expect(row.locator('.hv-session-row__worktree')).toBeVisible();
  });

  test(`session is routed to its project from ${kind} payload`, async ({
    page,
  }) => {
    await bootMinimal(page);
    const id = `route-${kind}`;
    await page.evaluate((info) => {
      window.__hive.state?.sessions.push(info);
      window.__hive.emit(
        'session:event',
        JSON.stringify({ kind: 'added', session: info }),
      );
    }, shape(id));

    // The session must end up under its project's <li.hv-project-card
    // data-pid="p1"> in the sidebar — the routing read is
    // `projectId ?? project_id` (app/sidebar.ts, app/view.ts). If a
    // future refactor drops one branch, the session would orphan
    // out of its project subtree and this assertion fails.
    const sessionRow = page.locator(
      `#projects li.hv-project-card[data-pid="p1"] li.hv-session-row[data-sid="${id}"]`,
    );
    await expect(sessionRow).toBeVisible({ timeout: 2000 });
  });
}
