import { test, expect, type Page } from '@playwright/test';

// E2E for the UX polish pass: ⌘/ help overlay, empty states, launcher
// loading row, collapse persistence, and the a11y attributes.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

test('⌘/ opens the shortcuts overlay, Esc closes it, typing reaches the terminal again', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+/`);
  const overlay = page.locator('#help-overlay');
  await expect(overlay).toBeVisible();
  // The overlay lists both palette-visible and palette-invisible bindings.
  await expect(overlay).toContainText('Command palette');
  await expect(overlay).toContainText('Copy selection');
  await page.keyboard.press('Escape');
  await expect(overlay).toBeHidden();
  // Wait for focus to actually land on the terminal (setFocusedTile defers
  // the real ta.focus() via requestAnimationFrame) before typing, or keys
  // sent too early can land nowhere under CI load.
  await expect
    .poll(() =>
      page.evaluate(() => {
        const ta =
          document.querySelector('.term-host.active .xterm-helper-textarea') ||
          document.querySelector('.term-host .xterm-helper-textarea');
        return document.activeElement === ta;
      }),
    )
    .toBe(true);
  // Focus must return to the terminal: typed keys land in stdin.
  await page.evaluate(() => window.__hive.resetStdin());
  await page.keyboard.type('hi');
  await expect
    .poll(() => page.evaluate(() => window.__hive.stdinText()))
    .toContain('hi');
});

test('⌘? also opens the shortcuts overlay, and closes it again', async ({
  page,
}) => {
  await boot(page);
  // "?" is Shift+/ — the near-universal "show me the shortcuts" key.
  // With the sidebar footer no longer advertising any bindings, this is
  // the key users are most likely to try unprompted, so it must work.
  await page.keyboard.press(`${mod}+Shift+Slash`);
  const overlay = page.locator('#help-overlay');
  await expect(overlay).toBeVisible();
  await expect(overlay).toContainText('Command palette');
  // The same key closes it, matching ⌘/ toggle behaviour.
  await page.keyboard.press(`${mod}+Shift+Slash`);
  await expect(overlay).toBeHidden();
});

test('the shortcuts overlay advertises both ⌘? and ⌘/', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+Shift+Slash`);
  await expect(
    page.locator('#help-overlay').getByText('Keyboard shortcuts (this panel)'),
  ).toBeVisible();
  // Whichever key the user arrived by, the panel must name the other.
  const expected =
    process.platform === 'darwin' ? '⌘? or ⌘/' : 'Ctrl+? or Ctrl+/';
  await expect(page.locator('#help-overlay kbd', { hasText: 'or' })).toHaveText(
    expected,
  );
});

test('help overlay is reachable from the command palette and gates other shortcuts', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+Shift+k`);
  await page.locator('#command-palette-input').fill('keyboard');
  await page.keyboard.press('Enter');
  await expect(page.locator('#help-overlay')).toBeVisible();
  // While open, app shortcuts must not fire: ⌘G would flip to grid.
  await page.keyboard.press(`${mod}+g`);
  await expect(page.locator('#terms')).not.toHaveClass(/grid/);
  await page.keyboard.press(`${mod}+/`); // toggle also closes
  await expect(page.locator('#help-overlay')).toBeHidden();
});

test('killing the last session shows an actionable empty state', async ({
  page,
}) => {
  await boot(page);
  await page.evaluate(() => window.__hive.killSession?.('s1'));
  const empty = page.locator('#empty-state');
  await expect(empty).toBeVisible();
  await expect(empty).toHaveAttribute('data-kind', 'first-run');
  await expect(empty).toContainText('No sessions yet');
  await empty.getByRole('button', { name: /New session/ }).click();
  await expect(page.locator('#launcher')).toBeVisible();
  // Creating a session hides the empty state again.
  await page.keyboard.press('Enter');
  await expect(empty).toBeHidden();
});

test('launcher shows a loading row before agents resolve', async ({ page }) => {
  await boot(page);
  // Stall ListAgents so the in-flight loading row is observable.
  await page.evaluate(() => window.__hive.delayNext?.('ListAgents', 500));
  await page.keyboard.press(`${mod}+t`);
  const launcher = page.locator('#launcher');
  await expect(launcher).toBeVisible();
  await expect(launcher.locator('.launcher-loading')).toBeVisible();
  // When the list resolves, the loading row is replaced by agents.
  await expect(launcher.locator('.launcher-item').first()).toContainText(
    'Shell',
  );
  await expect(launcher.locator('.launcher-loading')).toHaveCount(0);
  await page.keyboard.press('Escape');
});

test('selecting an empty project shows the empty state; switching away clears it', async ({
  page,
}) => {
  await boot(page);
  await page.evaluate(() => {
    window.__hive.emit(
      'project:event',
      JSON.stringify({
        kind: 'added',
        project: {
          id: 'p2',
          name: 'empty',
          color: '#888',
          cwd: '',
          order: 1,
          created: new Date().toISOString(),
        },
      }),
    );
  });
  // Clicking the empty project's row goes through switchToProject —
  // a repaint path that does not rebuild the sidebar.
  await page.locator('#projects .project[data-pid="p2"] .project-name').click();
  const empty = page.locator('#empty-state');
  await expect(empty).toBeVisible();
  await expect(empty).toHaveAttribute('data-kind', 'project-empty');
  await expect(empty).toContainText('No sessions in this project');
  // Switching to a live session must clear the pane — a stale overlay
  // here would sit above the terminal and intercept clicks.
  await page.locator('#projects .session-item[data-sid="s1"]').click();
  await expect(empty).toBeHidden();
});

test('first-run empty state updates when projects change without a kind change', async ({
  page,
}) => {
  await boot(page);
  await page.evaluate(() => window.__hive.killSession?.('s1'));
  const empty = page.locator('#empty-state');
  await expect(empty).toHaveAttribute('data-kind', 'first-run');
  await expect(empty.getByRole('button', { name: /New project/ })).toHaveCount(
    0,
  );
  // Remove the only project: same kind, but the model now offers a
  // New project action too.
  await page.evaluate(() => {
    const p = window.__hive.state?.projects[0];
    window.__hive.emit(
      'project:event',
      JSON.stringify({ kind: 'removed', project: p }),
    );
  });
  await expect(
    empty.getByRole('button', { name: /New project/ }),
  ).toBeVisible();
  // And adding a project back removes it again.
  await page.evaluate(() => {
    window.__hive.emit(
      'project:event',
      JSON.stringify({
        kind: 'added',
        project: {
          id: 'p9',
          name: 'fresh',
          color: '#888',
          cwd: '',
          order: 0,
          created: new Date().toISOString(),
        },
      }),
    );
  });
  await expect(empty.getByRole('button', { name: /New project/ })).toHaveCount(
    0,
  );
  await expect(
    empty.getByRole('button', { name: /New session/ }),
  ).toBeVisible();
});

test('native menu event toggles the help overlay open and closed', async ({
  page,
}) => {
  await boot(page);
  // The macOS menu accelerator intercepts ⌘/ before the webview's
  // keydown listener, so the menu:keyboard-shortcuts handler itself
  // must toggle — open on first fire, close on the second.
  await page.evaluate(() => window.__hive.emit('menu:keyboard-shortcuts'));
  await expect(page.locator('#help-overlay')).toBeVisible();
  await page.evaluate(() => window.__hive.emit('menu:keyboard-shortcuts'));
  await expect(page.locator('#help-overlay')).toBeHidden();
});

test('help overlay traps Tab inside the dialog', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+/`);
  await expect(page.locator('#help-overlay')).toBeVisible();
  for (let i = 0; i < 3; i++) await page.keyboard.press('Tab');
  await expect
    .poll(() => page.evaluate(() => document.activeElement?.id))
    .toBe('help-overlay-close');
  await page.keyboard.press('Escape');
  await expect(page.locator('#help-overlay')).toBeHidden();
});

test('sidebar footer shows hive/hived version and build', async ({ page }) => {
  await boot(page);
  const footer = page.locator('#sidebar-hints');
  const daemonLine = page.locator('#ver-daemon');

  // Matching builds: one line, daemon line hidden — the common case.
  await page.evaluate(() =>
    window.__hive.emit('daemon:stale', {
      severity: 'match',
      guiBuild: 'a3f9c1',
      daemonBuild: 'a3f9c1',
      guiRelease: 'v0.4.2',
      daemonRelease: 'v0.4.2',
    }),
  );
  await expect(footer).toContainText('hive v0.4.2 (a3f9c1)');
  await expect(daemonLine).toBeHidden();

  // Mismatch: both lines visible, warning styling applied.
  await page.evaluate(() =>
    window.__hive.emit('daemon:stale', {
      severity: 'mismatch',
      guiBuild: 'a3f9c1',
      daemonBuild: 'b7e220',
      guiRelease: 'v0.4.2',
      daemonRelease: 'v0.4.1',
    }),
  );
  await expect(daemonLine).toBeVisible();
  await expect(daemonLine).toContainText('hived v0.4.1 (b7e220)');
  await expect(footer).toHaveClass(/mismatch/);

  // Back to matching: collapses again and clears the warning class.
  await page.evaluate(() =>
    window.__hive.emit('daemon:stale', {
      severity: 'match',
      guiBuild: 'a3f9c1',
      daemonBuild: 'a3f9c1',
      guiRelease: 'v0.4.2',
      daemonRelease: 'v0.4.2',
    }),
  );
  await expect(daemonLine).toBeHidden();
  await expect(footer).not.toHaveClass(/mismatch/);

  // The hidden attribute must do the hiding on its own. `.hints > span`
  // sets an author-origin `display: block`, which outranks the UA's
  // `[hidden] { display: none }` — so without an explicit rule this
  // would stay visible, and the assertions above would only be passing
  // because the render also blanks the text.
  await page.evaluate(() => {
    const el = document.getElementById('ver-daemon');
    if (!el) throw new Error('#ver-daemon missing');
    el.textContent = 'hived v9.9.9 (deadbee)';
    el.hidden = true;
  });
  await expect(daemonLine).toBeHidden();
});

test('sidebar footer does not overflow a narrow sidebar', async ({ page }) => {
  await boot(page);
  // Two long lines at the narrowest sidebar width is the worst case:
  // this is what the removed `white-space: nowrap` used to hide behind
  // an ellipsis, which would have truncated the build hash the footer
  // exists to show.
  await page.evaluate(() => {
    const sidebar = document.getElementById('sidebar');
    if (!sidebar) throw new Error('#sidebar missing');
    sidebar.style.width = '150px';
    window.__hive.emit('daemon:stale', {
      severity: 'mismatch',
      guiBuild: 'a3f9c1d',
      daemonBuild: 'b7e220f',
      guiRelease: 'v0.4.2-rc.1',
      daemonRelease: 'v0.4.1-rc.9',
    });
  });

  const overflow = await page.evaluate(() => {
    const el = document.getElementById('sidebar-hints');
    const sidebar = document.getElementById('sidebar');
    if (!el || !sidebar) throw new Error('#sidebar-hints / #sidebar missing');
    return {
      footerRight: el.getBoundingClientRect().right,
      sidebarRight: sidebar.getBoundingClientRect().right,
      scrollOverflow: el.scrollWidth - el.clientWidth,
    };
  });
  // scrollWidth vs clientWidth is the load-bearing check: #sidebar sets
  // overflow-x: hidden, so the footerRight comparison alone would still
  // pass if the text overflowed (it would just be clipped invisibly).
  expect(overflow.scrollOverflow).toBeLessThanOrEqual(1);
  expect(overflow.footerRight).toBeLessThanOrEqual(overflow.sidebarRight + 1);
});

test('project collapse state survives a reload', async ({ page }) => {
  await boot(page);
  const caret = page.locator('#projects .project[data-pid="p1"] .caret');
  await caret.click();
  await expect(page.locator('#projects .project[data-pid="p1"]')).toHaveClass(
    /collapsed/,
  );
  await expect(caret).toHaveAttribute('aria-expanded', 'false');

  await page.reload();
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  await expect(page.locator('#projects .project[data-pid="p1"]')).toHaveClass(
    /collapsed/,
  );

  // Expand again and confirm that persists too.
  await page.locator('#projects .project[data-pid="p1"] .caret').click();
  await page.reload();
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  await expect(
    page.locator('#projects .project[data-pid="p1"]'),
  ).not.toHaveClass(/collapsed/);
});

test('a11y attributes: palette input label, alertdialog dead overlay, caret button', async ({
  page,
}) => {
  await boot(page);
  await expect(page.locator('#command-palette-input')).toHaveAttribute(
    'aria-label',
    'Search commands',
  );
  const caret = page.locator('#projects .project[data-pid="p1"] .caret');
  await expect(caret).toHaveAttribute('aria-expanded', 'true');

  // Drive a session death through the mock and check the overlay role.
  await page.evaluate(() => {
    const s = {
      ...window.__hive.state?.sessions[0],
      alive: false,
      last_error: 'boom',
    };
    window.__hive.emit(
      'session:event',
      JSON.stringify({ kind: 'updated', session: s }),
    );
  });
  const overlay = page.locator('.dead-overlay[role="alertdialog"]');
  await expect(overlay).toBeVisible();
  await expect(overlay).toHaveAttribute('aria-label', 'Session ended');
  // Escape dismisses and focus returns to the terminal.
  await page.keyboard.press('Escape');
  await expect(overlay).toBeHidden();
});

test('the sidebar cannot be dragged below the 220px design floor', async ({
  page,
}) => {
  await boot(page);
  const handle = page.locator('#sidebar-resizer');
  const box = await handle.boundingBox();
  if (!box) throw new Error('resizer not laid out');
  await page.mouse.move(box.x + box.width / 2, box.y + 100);
  await page.mouse.down();
  await page.mouse.move(40, box.y + 100, { steps: 5 });
  await page.mouse.up();
  const w = await page
    .locator('#sidebar')
    .evaluate((el) => el.getBoundingClientRect().width);
  expect(w).toBeGreaterThanOrEqual(220);
});
