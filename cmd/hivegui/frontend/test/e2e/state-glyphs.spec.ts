import { test, expect, type Page } from '@playwright/test';

// The session state glyph, end to end through a real layout engine.
//
// jsdom is enough to prove the right <use href> and data-state land on
// the element (test/dom/attention-icon.test.tsx does exactly that), and
// it is blind to everything that decides whether a user can actually
// see them: the CSS that colours each state, the animation, and the
// reduced-motion escape hatch. Those are the parts that have shipped
// broken before, so they get a browser.
async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

function glyph(page: Page, id: string) {
  return page.locator(
    `.hv-session-row[data-sid="${id}"] .hv-session-row__state`,
  );
}

async function firstSessionId(page: Page): Promise<string> {
  return page.evaluate(() => window.__hive.state?.sessions[0].id ?? '');
}

test('the daemon state drives the sidebar glyph', async ({ page }) => {
  await boot(page);
  const id = await firstSessionId(page);

  // The steady state. Empty on the wire, idle in the UI.
  await expect(glyph(page, id)).toHaveAttribute('data-state', 'running');

  await page.evaluate(
    (sid) => window.__hive.setSessionState?.(sid, 'working'),
    id,
  );
  await expect(glyph(page, id)).toHaveAttribute('data-state', 'working');

  await page.evaluate(
    (sid) => window.__hive.setSessionState?.(sid, 'waiting_permission', 'hook'),
    id,
  );
  await expect(glyph(page, id)).toHaveAttribute(
    'data-state',
    'waiting-permission',
  );

  await page.evaluate((sid) => window.__hive.setSessionState?.(sid, ''), id);
  await expect(glyph(page, id)).toHaveAttribute('data-state', 'running');
});

// Each state has to be visually distinguishable, not merely differently
// labelled. A glyph whose colour rule never matched would pass every
// jsdom assertion in the suite and render as inherited grey.
test('each state paints a different colour', async ({ page }) => {
  await boot(page);
  const id = await firstSessionId(page);

  const colourFor = async (state: string, rendered: string, source = '') => {
    await page.evaluate(
      ([sid, s, src]) => window.__hive.setSessionState?.(sid, s, src),
      [id, state, source] as const,
    );
    // Wait for the glyph to actually re-render, not for a fixed 50ms:
    // a loaded runner reads the computed style of the PREVIOUS state
    // and the assertion then passes or fails on the wrong element.
    await expect(glyph(page, id)).toHaveAttribute('data-state', rendered);
    return glyph(page, id).evaluate((el) => getComputedStyle(el).color);
  };

  const idle = await colourFor('', 'running');
  const working = await colourFor('working', 'working');
  const waiting = await colourFor(
    'waiting_permission',
    'waiting-permission',
    'hook',
  );

  for (const [name, value] of Object.entries({ idle, working, waiting })) {
    expect(value, `${name} has no colour of its own`).not.toBe(
      'rgba(0, 0, 0, 0)',
    );
  }
  // Working shares the healthy colour with idle by design — the shape
  // and the motion carry that difference — but "the agent needs you"
  // must never be the same colour as "everything is fine".
  expect(waiting).not.toBe(idle);
});

// A live agent-reported error wants the user like a wait: it takes the
// attention ground and name colour, and is NOT struck through like a dead
// row. Only a browser shows which of the competing rules actually wins.
test('a live error row reads as attention, not as exited', async ({ page }) => {
  await boot(page);
  const id = await firstSessionId(page);
  const row = page.locator(`.hv-session-row[data-sid="${id}"]`);
  const nameOf = () =>
    row.locator('.hv-session-row__name').evaluate((el) => {
      const cs = getComputedStyle(el);
      return { color: cs.color, deco: cs.textDecorationLine };
    });
  const overlayAnimation = () =>
    row.evaluate((el) => getComputedStyle(el, '::after').animationName);

  await page.evaluate(
    (sid) => window.__hive.setSessionState?.(sid, 'waiting_input', 'hook'),
    id,
  );
  await expect(row).toHaveAttribute('data-state', 'attention');
  const attention = await nameOf();

  await page.evaluate(
    (sid) => window.__hive.setSessionState?.(sid, 'error', 'hook'),
    id,
  );
  await expect(row).toHaveAttribute('data-state', 'failed');
  const failed = await nameOf();
  expect(failed.deco).toBe('none');
  expect(failed.color).toBe(attention.color);
  expect(await overlayAnimation()).toBe('hv-attn-tint');
  await expect(row.locator('.hv-session-row__sub')).toHaveText(
    'Stopped on an error',
  );
  // The glyph still says it failed, in its own colour.
  await expect(glyph(page, id)).toHaveAttribute('data-state', 'failed');
  // A failed session is still running: no Restart on the row.
  await row.hover();
  await expect(row.locator('[data-action="restart"]')).toHaveCount(0);
  const glyphColour = await glyph(page, id).evaluate(
    (el) => getComputedStyle(el).color,
  );
  expect(glyphColour).not.toBe(attention.color);
});

test('waiting animates and working fades, and reduced motion stops both', async ({
  page,
}) => {
  await boot(page);
  const id = await firstSessionId(page);
  const animationOf = () =>
    glyph(page, id).evaluate((el) => getComputedStyle(el).animationName);

  await page.evaluate(
    (sid) => window.__hive.setSessionState?.(sid, 'working'),
    id,
  );
  // Retrying attribute wait first: reading animationName straight after
  // the evaluate races the re-render and asserts the old state's style.
  await expect(glyph(page, id)).toHaveAttribute('data-state', 'working');
  expect(await animationOf()).toBe('hv-state-pulse-fg');

  await page.evaluate(
    (sid) => window.__hive.setSessionState?.(sid, 'waiting_permission', 'hook'),
    id,
  );
  await expect(glyph(page, id)).toHaveAttribute(
    'data-state',
    'waiting-permission',
  );
  expect(await animationOf()).toBe('hv-state-pulse');

  // The reduced-motion rule is one `!important` away from being dead
  // code; nothing else in the suite would notice if it were.
  await page.emulateMedia({ reducedMotion: 'reduce' });
  expect(await animationOf()).toBe('none');
});
