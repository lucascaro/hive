import { test, expect, type Page } from '@playwright/test';

// E2E for the settings modal and custom agents: the ⌘, binding, the
// native menu:settings event, and the round-trip that puts a custom
// agent into the ⌘T launcher. These drive the real index.html markup
// and keyboard wiring, which the jsdom unit spec (test/dom/settings)
// stubs out.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

async function addAgent(page: Page, name: string, cmd: string) {
  await page.locator('#settings-agent-add').click();
  const row = page.locator('.settings-agent-row').last();
  await row.locator('.settings-agent-name').fill(name);
  await row.locator('.settings-agent-cmd').fill(cmd);
}

test('⌘, opens settings, Esc closes it, typing reaches the terminal again', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();
  await expect(page.locator('#settings-tabs')).toContainText('Agents');

  await page.keyboard.press('Escape');
  await expect(page.locator('#settings')).toBeHidden();

  // Wait for focus to actually land before typing. toBeHidden() only
  // proves the class flipped, which is synchronous — but closeSettings
  // restores focus through setFocusedTile, which defers the real
  // focus() into a requestAnimationFrame with a retry chain
  // (src/app/focus.ts). Typing in that gap sends the keys nowhere and
  // the assertion below fails with an empty stdin, which is what this
  // test did once on a loaded macOS CI runner.
  await expect(
    page.getByRole('textbox', { name: 'Terminal input' }),
  ).toBeFocused();

  // Focus is back on the terminal: typed keys land in stdin.
  await page.evaluate(() => window.__hive.resetStdin());
  await page.keyboard.type('hi');
  await expect
    .poll(() => page.evaluate(() => window.__hive.stdinText()))
    .toContain('hi');
});

test('settings owns the keyboard while open', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();

  // ⌘G would flip to grid if the shortcut leaked past the modal.
  await page.keyboard.press(`${mod}+g`);
  await expect(page.locator('#terms')).not.toHaveClass(/grid/);

  // Typing into a field must not reach the terminal behind the backdrop.
  await page.evaluate(() => window.__hive.resetStdin());
  await page.locator('#settings-agent-add').click();
  await page.keyboard.type('typed');
  expect(await page.evaluate(() => window.__hive.stdinText())).not.toContain(
    'typed',
  );
});

test('Tab stays inside the dialog but still walks the form fields', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await addAgent(page, 'Tabbed', 'tabbed');

  // Tab must move between the row's own inputs, not pin to one element
  // the way the single-control help overlay does.
  await page.locator('.settings-agent-name').focus();
  await page.keyboard.press('Tab');
  await expect
    .poll(() => page.evaluate(() => document.activeElement?.className))
    .toContain('settings-agent-cmd');

  // aria-modal promises focus never leaves: tabbing off the last
  // control wraps to the first instead of reaching the terminal.
  for (let i = 0; i < 12; i++) await page.keyboard.press('Tab');
  await expect
    .poll(() =>
      page.evaluate(() => {
        const settings = document.getElementById('settings');
        if (!settings) throw new Error('#settings missing');
        return settings.contains(document.activeElement);
      }),
    )
    .toBe(true);
});

test('re-opening settings does not wipe an in-progress draft', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await addAgent(page, 'Half Typed', 'halftyped --flag');

  // On macOS the native File ▸ Settings… accelerator wins ⌘, over the
  // webview, so this arrives as menu:settings with the modal already
  // open — the path that used to hit `draft = []` and discard the row.
  await page.evaluate(() => window.__hive.emit('menu:settings'));
  await expect(page.locator('#settings')).toBeVisible();

  const row = page.locator('.settings-agent-row').first();
  await expect(row.locator('.settings-agent-name')).toHaveValue('Half Typed');
  await expect(row.locator('.settings-agent-cmd')).toHaveValue(
    'halftyped --flag',
  );
});

test('a drag that starts in a field and ends on the backdrop does not close', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await addAgent(page, 'Selected', 'selected --x');

  // Selecting text in an input and releasing outside the panel
  // dispatches the click on the nearest common ancestor — the backdrop
  // — which used to close the modal and discard the draft.
  const field = page.locator('.settings-agent-name').first();
  const box = await field.boundingBox();
  if (!box) throw new Error('the settings agent-name field has no box');
  await page.mouse.move(box.x + 5, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width - 2, box.y + box.height / 2, {
    steps: 5,
  });
  await page.mouse.move(5, 5, { steps: 5 }); // release far outside the panel
  await page.mouse.up();

  await expect(page.locator('#settings')).toBeVisible();
  await expect(field).toHaveValue('Selected');
});

test('a genuine backdrop click still closes', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();
  await page.mouse.click(5, 5);
  await expect(page.locator('#settings')).toBeHidden();
});

test('native menu event opens settings', async ({ page }) => {
  await boot(page);
  // On macOS the menu accelerator intercepts ⌘, before the webview's
  // keydown listener ever sees it, so this path must work on its own.
  await page.evaluate(() => window.__hive.emit('menu:settings'));
  await expect(page.locator('#settings')).toBeVisible();
});

test('settings is reachable from the command palette', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+Shift+k`);
  await page.locator('#command-palette-input').fill('settings');
  await page.keyboard.press('Enter');
  await expect(page.locator('#settings')).toBeVisible();
});

test('a saved custom agent shows up in the launcher', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();

  await addAgent(page, 'Claude Lite', 'claude --model haiku');
  await page.locator('#settings-save').click();
  await expect(page.locator('#settings')).toBeHidden();

  // The launcher renders whatever ListAgents returns, so a custom
  // agent needs no launcher-specific code to appear.
  await page.keyboard.press(`${mod}+t`);
  await expect(page.locator('#launcher')).toContainText('Claude Lite');
});

test('custom agents round-trip: reopening settings shows the saved agent', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await addAgent(page, 'My Tool', 'mytool --fast');
  await page.locator('#settings-save').click();
  await expect(page.locator('#settings')).toBeHidden();

  await page.keyboard.press(`${mod}+,`);
  const row = page.locator('.settings-agent-row').first();
  await expect(row.locator('.settings-agent-name')).toHaveValue('My Tool');
  await expect(row.locator('.settings-agent-cmd')).toHaveValue('mytool --fast');
});

test('deleting a custom agent removes it', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await addAgent(page, 'Doomed', 'doomed');
  await page.locator('#settings-save').click();
  await expect(page.locator('#settings')).toBeHidden();

  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('.settings-agent-row')).toHaveCount(1);
  await page.locator('.settings-agent-delete').first().click();
  await page.locator('#settings-save').click();
  await expect(page.locator('#settings')).toBeHidden();

  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('.settings-agent-row')).toHaveCount(0);
  await expect(page.locator('#settings-agents-list')).toContainText(
    'No custom agents yet',
  );
});

test('cancel discards edits', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await addAgent(page, 'Ghost', 'ghost');
  await page.locator('#settings-cancel').click();
  await expect(page.locator('#settings')).toBeHidden();

  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('.settings-agent-row')).toHaveCount(0);
});

// Updates used to be pinned below a scrolling agent list so a long list
// could not push it off screen; it is its own tab now, and the tab is
// what holds that invariant. jsdom cannot tell whether a control is
// actually on screen, so this stays in a real browser: with enough
// agents to overflow the list, the channel control must still be
// visible and hittable one tab click away.
test('the Updates tab shows the channel picker under a long agent list', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();

  for (let i = 0; i < 12; i++) await addAgent(page, `Agent ${i}`, `cmd${i}`);

  await page.locator('#settings-tab-updates').click();
  const channel = page.locator('#settings-update-channel');
  await expect(channel).toBeVisible();

  // Visible is not the same as hittable — a section pushed under the
  // action bar still reports visible. elementFromPoint at the control's
  // own centre is the check that catches an overlap.
  const onTop = await channel.evaluate((el) => {
    const r = el.getBoundingClientRect();
    const hit = document.elementFromPoint(
      r.x + r.width / 2,
      r.y + r.height / 2,
    );
    return el.contains(hit) || el === hit;
  });
  expect(onTop).toBe(true);

  // The panel itself must not have grown past the window.
  const overflows = await page
    .locator('#settings-panel')
    .evaluate((el) => el.getBoundingClientRect().bottom > window.innerHeight);
  expect(overflows).toBe(false);
});

test('choosing the latest channel reveals the source-repo row', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await page.locator('#settings-tab-updates').click();
  await expect(page.locator('#settings-source-repo-row')).toBeHidden();

  await page.locator('#settings-update-channel').selectOption('latest');
  await expect(page.locator('#settings-source-repo-row')).toBeVisible();
  await expect(page.locator('#settings-source-repo')).toBeEditable();
});

// Appearance is a preference; the agent list is what people open
// Settings to edit. Putting Appearance first pushed the list off-screen
// on open, which is the one thing this dialog must not do — which is
// why Agents is the tab Settings opens on, and why this assertion still
// means what it meant before the split.
test('the agent list is on screen the moment Settings opens', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();

  const onTop = await page.locator('#settings-agents-list').evaluate((el) => {
    const r = el.getBoundingClientRect();
    if (r.height === 0) return false;
    const hit = document.elementFromPoint(r.x + r.width / 2, r.y + 8);
    return !!hit && el.contains(hit);
  });
  expect(onTop).toBe(true);
});

// The error slot is a <p> inside the dialog body, and the primitive's
// body-paragraph rule outranks the error class on colour: a rejected
// save rendered in --fg-muted, pixel-identical to the help text above
// it. Computed colour, because this is exactly the kind of bug that
// looks fine in the DOM.
test('a rejected save renders as an error, not as another hint', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();
  await addAgent(page, 'Broken', 'nope');
  await page.evaluate(() =>
    window.__hive.failNext?.('SaveCustomAgents', 'nope'),
  );
  await page.locator('#settings-save').click();

  const err = page.locator('#settings-error');
  await expect(err).toBeVisible();
  // Outside the tab panels, so it is still on screen from any tab.
  await page.locator('#settings-tab-updates').click();
  await expect(err).toBeVisible();
  const [errColor, hintColor] = await Promise.all([
    err.evaluate((el) => getComputedStyle(el).color),
    page
      .locator('#settings .settings-hint')
      .first()
      .evaluate((el) => getComputedStyle(el).color),
  ]);
  expect(errColor).not.toBe(hintColor);
});

// ---------- tabs ----------

test('Settings opens on Agents and the tabs swap panels', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();

  await expect(page.locator('#settings-tab-agents')).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await expect(page.locator('#settings-panel-agents')).toBeVisible();
  await expect(page.locator('#settings-panel-appearance')).toBeHidden();
  await expect(page.locator('#settings-panel-updates')).toBeHidden();

  await page.locator('#settings-tab-updates').click();
  await expect(page.locator('#settings-panel-updates')).toBeVisible();
  await expect(page.locator('#settings-panel-agents')).toBeHidden();
  await expect(page.locator('#settings-tab-updates')).toHaveAttribute(
    'aria-selected',
    'true',
  );
});

// Arrows are grid navigation everywhere else in the app; inside the
// strip they are the ARIA tabs pattern, and keyboard.ts's modal branch
// has to keep letting them through to the focused element.
test('arrow keys walk the tab strip and wrap', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await page.locator('#settings-tab-agents').focus();

  await page.keyboard.press('ArrowRight');
  await expect(page.locator('#settings-panel-appearance')).toBeVisible();
  await page.keyboard.press('ArrowLeft');
  await page.keyboard.press('ArrowLeft'); // wraps past Agents to Updates
  await expect(page.locator('#settings-panel-updates')).toBeVisible();

  // Roving tabindex: focus follows selection, or the keyboard user is
  // stranded on a tab that is no longer tabbable.
  await expect(page.locator('#settings-tab-updates')).toBeFocused();

  // The grid must not have moved behind the backdrop.
  await expect(page.locator('#terms')).not.toHaveClass(/grid/);
});

// The whole point of keeping every panel mounted: a switch must not cost
// the user an edit, and Save must still see it.
test('an unsaved agent survives a tab round-trip and still saves', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await addAgent(page, 'Roundtrip', 'roundtrip --x');

  await page.locator('#settings-tab-appearance').click();
  await page.locator('#settings-tab-updates').click();
  await page.locator('#settings-tab-agents').click();

  const row = page.locator('.settings-agent-row').first();
  await expect(row.locator('.settings-agent-name')).toHaveValue('Roundtrip');
  await expect(row.locator('.settings-agent-cmd')).toHaveValue('roundtrip --x');

  await page.locator('#settings-save').click();
  await expect(page.locator('#settings')).toBeHidden();
  await page.keyboard.press(`${mod}+,`);
  await expect(
    page.locator('.settings-agent-row').first().locator('.settings-agent-name'),
  ).toHaveValue('Roundtrip');
});

// display:none is what keeps a hidden panel's controls out of the tab
// order. If that ever regresses, Tab lands on an invisible field and the
// dialog looks like it swallowed the keyboard.
test('Tab never reaches a control in a hidden panel', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await page.locator('#settings-tab-agents').focus();

  for (let i = 0; i < 14; i++) {
    await page.keyboard.press('Tab');
    const landed = await page.evaluate(() => {
      const el = document.activeElement as HTMLElement | null;
      const settings = document.getElementById('settings');
      if (!settings || !el) throw new Error('#settings or focus missing');
      const hiddenPanel = el.closest('.settings-panel.hidden');
      return { inside: settings.contains(el), inHidden: !!hiddenPanel };
    });
    expect(landed.inside).toBe(true);
    expect(landed.inHidden).toBe(false);
  }
});
