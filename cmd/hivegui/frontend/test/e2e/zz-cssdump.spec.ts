import { test, type Page } from '@playwright/test';
import { writeFileSync } from 'node:fs';

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';
const OUT = process.env.CSSDUMP_OUT ?? '/tmp/cssdump.json';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  await page.evaluate((n) => window.__hive.addSession?.(n), 's2');
  await page.waitForFunction(
    () => (window.__hive.state?.sessions.length ?? 0) >= 2,
  );
}

const dump = () =>
  [...document.querySelectorAll<HTMLElement>('body *')].map((el) => {
    const cs = getComputedStyle(el);
    const out: Record<string, string> = {};
    for (let i = 0; i < cs.length; i++) {
      const p = cs.item(i);
      if (p.startsWith('--')) continue;
      out[p] = cs.getPropertyValue(p);
    }
    const path: string[] = [];
    for (
      let n: Element | null = el;
      n && n !== document.body;
      n = n.parentElement
    ) {
      path.unshift(
        `${n.tagName}${n.id ? `#${n.id}` : ''}${[
          ...n.parentElement!.children,
        ].indexOf(n)}`,
      );
    }
    return { key: path.join('>'), cls: el.className.toString(), style: out };
  });

test('css dump', async ({ page }) => {
  test.setTimeout(120000);
  const shots: Record<string, unknown> = {};
  for (const theme of ['classic', 'hive-dark', 'hive-light']) {
    await page.addInitScript(
      (t) => localStorage.setItem('hive.theme', t),
      theme,
    );
    await page.setViewportSize({ width: 1100, height: 700 });
    await boot(page);
    shots[`${theme}:base`] = await page.evaluate(dump);

    await page.keyboard.press(`${mod}+Shift+g`);
    await page.waitForTimeout(200);
    shots[`${theme}:grid`] = await page.evaluate(dump);
    await page.keyboard.press(`${mod}+Shift+g`);
    await page.waitForTimeout(200);

    for (const [name, chord] of [
      ['settings', `${mod}+,`],
      ['help', `${mod}+/`],
      ['worktrees', `${mod}+e`],
      ['palette', `${mod}+Shift+k`],
      ['launcher', `${mod}+t`],
    ] as const) {
      await page.keyboard.press(chord);
      await page.waitForTimeout(400);
      shots[`${theme}:${name}`] = await page.evaluate(dump);
      await page.keyboard.press('Escape');
      await page.waitForTimeout(200);
    }
  }
  writeFileSync(OUT, JSON.stringify(shots, null, 1));
});
