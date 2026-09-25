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

// Plan review (#457) against the REAL daemon and ws-bridge: an agent's
// review request over the events socket, the GUI raising it, and the
// user's answer arriving back at the requester — the whole loop the
// mock suite can only imitate.

const WS_URL = process.env.WS_BRIDGE_URL;
const TMP = process.env.HIVE_E2E_REAL_TMP ?? '';
const EVENTS_SOCK = path.join(TMP, 'hived.sock.events');
const SETTINGS = path.join(TMP, 'state', 'agent-settings.json');

test.beforeAll(() => {
  // Read live by the daemon on every review. GUI-owned file, so the
  // test writes it the way the Settings screen would.
  if (TMP) fs.writeFileSync(SETTINGS, JSON.stringify({ plan_review: true }));
});
test.afterAll(() => {
  if (TMP) fs.rmSync(SETTINGS, { force: true });
});

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

test('deny reaches the agent with the comment and its quote, then approve', async ({
  page,
}) => {
  const id = await firstSessionId(page);
  const plan = '# Plan\n\n1. Create `hello.txt`\n2. Verify it\n';

  const denied = requestPlanReview(EVENTS_SOCK, id, plan, TMP);
  await expect(page.locator('#plan-review')).toBeVisible();
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
  await expect(page.locator('#plan-review')).toBeHidden();

  const approved = requestPlanReview(EVENTS_SOCK, id, plan, TMP);
  await expect(page.locator('#plan-review')).toBeVisible();
  await page.locator('#plan-review-approve').click();
  expect((await approved).status).toBe('approve');
});

test('a requester that gives up closes the review in the GUI', async ({
  page,
}) => {
  const id = await firstSessionId(page);
  const ac = new AbortController();
  const req = requestPlanReview(EVENTS_SOCK, id, '# p\n', TMP, ac.signal);
  await expect(page.locator('#plan-review')).toBeVisible();
  ac.abort(); // Claude killed its hook: the user answered in the terminal
  expect((await req).status).toBe('aborted');
  await expect(page.locator('#plan-review')).toBeHidden();
});
