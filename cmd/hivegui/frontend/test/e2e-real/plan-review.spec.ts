import fs from 'node:fs';
import path from 'node:path';
import { test, expect, type Page } from '@playwright/test';
// The one JS encoder of Hive frames, the same code a real Pi runs: the
// daemon is driven by it rather than by a hand-written copy. Loaded at
// runtime, not imported: the frontend's tsconfig deliberately has no
// node types (see node-shim.d.ts), and hive.ts is node code.
type PlanReviewDecision = { status: string; message?: string };
type RequestPlanReview = (
  sock: string,
  sid: string,
  plan: string,
  cwd: string,
  signal?: AbortSignal,
) => Promise<PlanReviewDecision>;
const HIVE_TS = '../../../../../internal/agent/pi/hive.ts';
let requestPlanReview: RequestPlanReview;
test.beforeAll(async () => {
  ({ requestPlanReview } = (await import(HIVE_TS)) as {
    requestPlanReview: RequestPlanReview;
  });
});

// Plan review (#457) against the REAL daemon and ws-bridge, as the
// bundled plan-review plugin (spec 471): the daemon materializes it into
// the isolated state dir, the user enables it in Settings → Plugins, an
// agent's review request over the events socket is raised by the
// plugin, and the user's answer arrives back at the requester.
//
// Serial: the first test needs the daemon's fresh state (the plugin
// installed and disabled), and the rest enable it.

test.describe.configure({ mode: 'serial' });

const WS_URL = process.env.WS_BRIDGE_URL;
const TMP = process.env.HIVE_E2E_REAL_TMP ?? '';
const EVENTS_SOCK = path.join(TMP, 'hived.sock.events');
const UI_MJS = path.join(TMP, 'state', 'plugins', 'plan-review', 'ui.mjs');
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

test.beforeEach(async ({ page }) => {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set — globalSetup did not run');
  await page.addInitScript((url) => {
    window.__WS_BRIDGE_URL = url;
  }, WS_URL);
  await page.goto('/');
  await page.waitForFunction(
    () => (window.__hive_state?.sessions ?? []).some((s) => s.alive),
    null,
    { timeout: 10000 },
  );
});

const firstSessionId = (page: Page) =>
  page.evaluate(
    () => (window.__hive_state?.sessions ?? []).find((s) => s.alive)?.id ?? '',
  );
const view = (page: Page) => page.locator('#plugin-view');
const row = (page: Page) =>
  page.locator('.settings-plugin-row[data-plugin-id="plan-review"]');

async function openPlugins(page: Page) {
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();
  await page.locator('#settings-tab-plugins').click();
  await expect(row(page)).toBeVisible();
}

async function closeSettings(page: Page) {
  await page.keyboard.press('Escape');
  await expect(page.locator('#settings')).toBeHidden();
}

/** Enables the bundled plugin the way a user does, and waits for its UI
 * to be running in this window (its settings section shows). */
async function enableReview(page: Page) {
  await openPlugins(page);
  const box = row(page).locator('.settings-plugin-enabled');
  // A click, not check(): the box follows the daemon's answer, a round
  // trip later, so it never flips under the click itself.
  if (!(await box.isChecked())) await box.click();
  await expect(box).toBeChecked();
  await expect(row(page).locator('#plan-review-reviewer')).toBeVisible({
    timeout: 15000,
  });
  await closeSettings(page);
}

/** Asks for a review until the daemon parks one, and returns the
 * pending decision boxed: an async function returning the promise
 * itself would wait for the decision. The window announces its review
 * UI a round trip after the plugin activates; a request that lands
 * before that falls back (no_client), which is correct and just means
 * "ask again". */
async function raise(
  page: Page,
  id: string,
  plan: string,
  signal?: AbortSignal,
): Promise<{ decision: Promise<PlanReviewDecision> }> {
  for (let i = 0; i < 20; i++) {
    const req = requestPlanReview(EVENTS_SOCK, id, plan, TMP, signal);
    const first = await Promise.race([
      req.then((d) => d.status),
      view(page)
        .waitFor({ state: 'visible', timeout: 5000 })
        .then(() => 'raised'),
    ]);
    if (first === 'raised') return { decision: req };
    expect(first).toBe('no_client');
    await page.waitForTimeout(100);
  }
  throw new Error('the review never parked');
}

// Criterion 3: on a fresh install the plugin is listed, installed and
// disabled, with nothing to fetch and no Remove; and disabled, plan
// review is off, so the agent is told to carry on without it.
test('fresh state: the bundled plugin is installed and disabled, and review is off', async ({
  page,
}, testInfo) => {
  await openPlugins(page);
  const box = row(page).locator('.settings-plugin-enabled');
  // A retry reruns this serial group against the same daemon, which a
  // later test already switched on: only the first attempt sees fresh
  // state, so a retry switches it back off first.
  if (testInfo.retry > 0 && (await box.isChecked())) await box.click();
  await expect(box).not.toBeChecked();
  await expect(row(page).locator('.settings-plugin-remove')).toHaveCount(0);
  await expect(row(page).locator('.settings-plugin-source')).toHaveText(
    'Ships with Hive',
  );
  expect(fs.existsSync(UI_MJS)).toBe(true);
  await closeSettings(page);
  const id = await firstSessionId(page);
  const d = await requestPlanReview(EVENTS_SOCK, id, '# p\n', TMP);
  expect(d.status).toBe('disabled');
  await expect(view(page)).toBeHidden();
});

test('deny reaches the agent with the comment and its quote, then approve', async ({
  page,
}) => {
  await enableReview(page);
  const id = await firstSessionId(page);
  const plan = '# Plan\n\n1. Create `hello.txt`\n2. Verify it\n';

  const denied = (await raise(page, id, plan)).decision;
  await expect(view(page)).toBeVisible();
  await expect(page.locator('#plan-review-body li')).toHaveCount(2);
  await page.evaluate(() => {
    const node = document.querySelector('#plan-review-body li code');
    const range = document.createRange();
    range.selectNodeContents(node as Node);
    const sel = window.getSelection();
    sel?.removeAllRanges(); // Chrome ignores a second range
    sel?.addRange(range);
  });
  await page.locator('#plan-review-body').dispatchEvent('mouseup');
  await page.locator('#plan-review-comment-start').click();
  await page.locator('#plan-review-comment').fill('Name it greeting.txt');
  await page.locator('#plan-review-comment-add').click();
  await page.locator('#plan-review-deny').click();

  const d = await denied;
  expect(d.status).toBe('deny');
  expect(d.message).toContain('On "hello.txt"');
  expect(d.message).toContain('Name it greeting.txt');
  expect(d.message).toContain('call hive_submit_plan again');
  await expect(view(page)).toBeHidden();

  const approved = (await raise(page, id, plan)).decision;
  await expect(view(page)).toBeVisible();
  await page.locator('#plan-review-approve').click();
  expect((await approved).status).toBe('approve');
});

test('a requester that gives up closes the review in the GUI', async ({
  page,
}) => {
  await enableReview(page);
  const id = await firstSessionId(page);
  const ac = new AbortController();
  const req = (await raise(page, id, '# p\n', ac.signal)).decision;
  await expect(view(page)).toBeVisible();
  ac.abort(); // Claude killed its hook: the user answered in the terminal
  expect((await req).status).toBe('aborted');
  await expect(view(page)).toBeHidden();
});

// Enabled is not enough: the daemon parks a review only for a window
// whose review UI is actually running. With the plugin's module broken
// in the state dir, the UI fails in the app, the window announces
// nothing, and the agent falls back to its terminal prompt.
test('enabled but the UI failed: the agent falls back (no_client)', async ({
  page,
}) => {
  await enableReview(page);
  const good = fs.readFileSync(UI_MJS, 'utf8');
  try {
    fs.writeFileSync(
      UI_MJS,
      'export default function () { throw new Error("broken on purpose"); }\n',
    );
    await page.reload();
    await page.waitForFunction(
      () => (window.__hive_state?.sessions ?? []).some((s) => s.alive),
      null,
      { timeout: 10000 },
    );
    await openPlugins(page);
    await expect(row(page).locator('.settings-plugin-error')).toContainText(
      'broken on purpose',
      { timeout: 15000 },
    );
    await closeSettings(page);
    const id = await firstSessionId(page);
    const d = await requestPlanReview(EVENTS_SOCK, id, '# p\n', TMP);
    expect(d.status).toBe('no_client');
    await expect(view(page)).toBeHidden();
  } finally {
    fs.writeFileSync(UI_MJS, good);
  }
});
