import { expect, type Page, test } from '@playwright/test';
import type { SessionTerm } from '../../src/app/session-term.js';

// Spec 449: ⌘-click a file path in a session to open it.
//
// Driven as Linux, so the modifier is Ctrl: on macOS the real gesture
// is ⌘-click, and Playwright's Meta on a Linux CI runner is not the
// same event. cmdOrCtrl() is the single place that difference lives,
// and the dom suite covers it directly.
//
// The clicks here are dispatched at the link's own cells rather than
// through page.mouse, because what is under test is the provider and
// the activation rules, not xterm's hit-testing of pixel positions.

async function boot(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'platform', { get: () => 'Linux x86_64' });
    Object.defineProperty(navigator, 'userAgentData', {
      get: () => ({ platform: 'Linux' }),
    });
  });
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  await page.waitForFunction(
    () => document.activeElement?.classList?.contains('xterm-helper-textarea'),
    null,
    { timeout: 3000 },
  );
}

/** Write text into the focused session's terminal. */
async function writeLine(page: Page, text: string) {
  await page.evaluate(async (t) => {
    const app = window.__hive_state;
    const st = app?.terms.get(app.activeId ?? '') as SessionTerm | undefined;
    if (!st) throw new Error('no tile for the active session');
    await new Promise<void>((r) => st.term.write(`${t}\r\n`, r));
  }, text);
}

/**
 * Ask the file-link provider for the links on a line, the same way
 * xterm does on hover. The link texts are what the underline follows.
 */
async function linksOnLine(page: Page, line: number): Promise<string[]> {
  return page.evaluate(async (ln) => {
    const app = window.__hive_state;
    const st = app?.terms.get(app.activeId ?? '') as SessionTerm | undefined;
    if (!st?.fileLinks) throw new Error('no file link provider');
    const links = await new Promise<{ text: string }[] | undefined>((r) =>
      st.fileLinks?.provideLinks(ln, r),
    );
    return (links ?? []).map((l) => l.text);
  }, line);
}

/** Activate the first file link on a line with the given modifiers. */
async function clickLink(
  page: Page,
  line: number,
  mods: { ctrl?: boolean; shift?: boolean },
) {
  await page.evaluate(
    async ({ ln, ctrl, shift }) => {
      const app = window.__hive_state;
      const st = app?.terms.get(app.activeId ?? '') as SessionTerm | undefined;
      if (!st?.fileLinks) throw new Error('no file link provider');
      const links = await new Promise<
        | { activate(e: MouseEvent, text: string): void; text: string }[]
        | undefined
      >((r) => st.fileLinks?.provideLinks(ln, r));
      if (!links?.length) throw new Error('no link to activate');
      links[0].activate(
        new MouseEvent('click', { ctrlKey: ctrl, shiftKey: shift }),
        links[0].text,
      );
    },
    { ln: line, ctrl: !!mods.ctrl, shift: !!mods.shift },
  );
}

/**
 * Activate an OSC 8 link by handing its URI to the terminal's own
 * linkHandler — the exact callback xterm invokes for an OSC 8 click.
 */
async function clickOscLink(
  page: Page,
  uri: string,
  mods: { ctrl?: boolean } = {},
) {
  await page.evaluate(
    ({ u, ctrl }) => {
      const app = window.__hive_state;
      const st = app?.terms.get(app.activeId ?? '') as SessionTerm | undefined;
      const handler = st?.term.options.linkHandler;
      if (!handler) throw new Error('no linkHandler');
      handler.activate(new MouseEvent('click', { ctrlKey: ctrl }), u, {
        start: { x: 1, y: 1 },
        end: { x: 1, y: 1 },
      });
    },
    { u: uri, ctrl: !!mods.ctrl },
  );
}

type OpenCall = {
  baseDir: string;
  path: string;
  line: number;
  col: number;
  editor: boolean;
};

async function openCalls(page: Page): Promise<OpenCall[]> {
  return page.evaluate(() => window.__hive.openFileCalls?.() ?? []);
}

test.describe('spec 449 file links', () => {
  test.beforeEach(async ({ page }) => {
    await boot(page);
    await page.evaluate(() =>
      window.__hive.setMockFiles?.(['src/foo.ts', 'README.md']),
    );
  });

  test('underlines a path that exists, not one that does not', async ({
    page,
  }) => {
    await writeLine(page, 'edited src/foo.ts:12:5 and nope/missing.ts');
    const term = await page.evaluate(() => {
      const app = window.__hive_state;
      const st = app?.terms.get(app.activeId ?? '') as SessionTerm | undefined;
      return st?.term.buffer.active.cursorY ?? 0;
    });
    // The text landed on the row above the cursor.
    const links = await linksOnLine(page, term);
    expect(links).toEqual(['src/foo.ts']);
  });

  test('cmd-click opens with the OS default, shift adds the editor', async ({
    page,
  }) => {
    await writeLine(page, 'edited src/foo.ts:12:5');
    const row = await page.evaluate(() => {
      const app = window.__hive_state;
      const st = app?.terms.get(app.activeId ?? '') as SessionTerm | undefined;
      return st?.term.buffer.active.cursorY ?? 0;
    });

    // A plain click must not open anything: in a terminal that gesture
    // belongs to selection and cursor positioning.
    await clickLink(page, row, {});
    expect(await openCalls(page)).toHaveLength(0);

    await clickLink(page, row, { ctrl: true });
    await expect.poll(async () => (await openCalls(page)).length).toBe(1);
    expect((await openCalls(page))[0]).toMatchObject({
      path: 'src/foo.ts',
      line: 12,
      col: 5,
      editor: false,
    });

    await clickLink(page, row, { ctrl: true, shift: true });
    await expect.poll(async () => (await openCalls(page)).length).toBe(2);
    expect((await openCalls(page))[1]).toMatchObject({
      path: 'src/foo.ts',
      line: 12,
      col: 5,
      editor: true,
    });
  });

  test('an OSC 8 file link opens the file; an https one goes to the browser', async ({
    page,
  }) => {
    await page.evaluate(() => window.__hive.resetOpenUrl?.());

    // A file:// URI is a path, and it needs the modifier like any other
    // file link.
    await clickOscLink(page, 'file:///tmp/report.md');
    expect(await openCalls(page)).toHaveLength(0);

    await clickOscLink(page, 'file:///tmp/report.md', { ctrl: true });
    await expect
      .poll(async () => (await openCalls(page)).map((c) => c.path))
      .toEqual(['/tmp/report.md']);
    // It never reached OpenURL, whose Go-side allowlist refuses file://
    // anyway — that refusal is still the backstop.
    expect(
      await page.evaluate(() => window.__hive.openedUrls?.() ?? []),
    ).toEqual([]);

    // Everything else still routes to the browser, unchanged.
    await clickOscLink(page, 'https://example.com/x', { ctrl: true });
    await expect
      .poll(async () => page.evaluate(() => window.__hive.openedUrls?.() ?? []))
      .toEqual(['https://example.com/x']);
    expect((await openCalls(page)).map((c) => c.path)).toEqual([
      '/tmp/report.md',
    ]);
  });

  test('a failed open is reported in the status bar', async ({ page }) => {
    await writeLine(page, 'edited src/foo.ts');
    const row = await page.evaluate(() => {
      const app = window.__hive_state;
      const st = app?.terms.get(app.activeId ?? '') as SessionTerm | undefined;
      return st?.term.buffer.active.cursorY ?? 0;
    });
    await page.evaluate(() => window.__hive.failNext?.('OpenFile'));
    await clickLink(page, row, { ctrl: true });
    await expect(page.locator('#status-text')).toContainText('open file');
  });

  test('the mouse-protocol workaround swallows a click only when it activates something', async ({
    page,
  }) => {
    // session-term.ts intercepts mousedown on .xterm-screen so a link
    // click survives a program that has mouse reporting on. A plain
    // click on a *file* link activates nothing, so it must NOT be
    // swallowed — it still belongs to selection and click-to-position.
    // URL links are unchanged: they follow on a plain click.
    const fileLink = { text: 'src/foo.ts', hiveFile: true };
    const urlLink = { text: 'https://example.com/x' };

    expect(await mousedownReachedScreen(page, fileLink, false)).toBe(true);
    expect(await mousedownReachedScreen(page, fileLink, true)).toBe(false);
    expect(await mousedownReachedScreen(page, urlLink, false)).toBe(false);
  });
});

/**
 * Dispatch a mousedown on the session's .xterm-screen with `target`
 * staged as the link under the cursor, and report whether the event
 * survived the mouse-protocol workaround's capture listener.
 *
 * A sentinel capture listener registered after that one runs only when
 * it did not call stopImmediatePropagation — which is exactly the
 * "don't swallow it" contract under test.
 */
async function mousedownReachedScreen(
  page: Page,
  target: { text?: string; hiveFile?: boolean },
  ctrl: boolean,
): Promise<boolean> {
  return page.evaluate(
    ({ t, c }) => {
      const app = window.__hive_state;
      const st = app?.terms.get(app.activeId ?? '') as SessionTerm | undefined;
      if (!st) throw new Error('no tile for the active session');
      const core = (
        st.term as unknown as {
          _core?: {
            linkifier?: {
              currentLink?: unknown;
              _currentLink?: unknown;
            } | null;
          };
        }
      )._core;
      if (!core?.linkifier) throw new Error('no linkifier');
      const screen =
        st.term.element?.querySelector<HTMLElement>('.xterm-screen');
      if (!screen) throw new Error('no .xterm-screen');

      // `currentLink` is a getter with no setter, so stage the link by
      // writing the backing field the getter reads.
      const prev = core.linkifier._currentLink;
      core.linkifier._currentLink = { link: t };
      if (core.linkifier.currentLink === undefined) {
        throw new Error('staging currentLink failed — xterm internals moved');
      }
      let reached = false;
      const sentinel = () => {
        reached = true;
      };
      screen.addEventListener('mousedown', sentinel, { capture: true });
      try {
        screen.dispatchEvent(
          new MouseEvent('mousedown', {
            bubbles: true,
            cancelable: true,
            ctrlKey: c,
          }),
        );
      } finally {
        screen.removeEventListener('mousedown', sentinel, { capture: true });
        core.linkifier._currentLink = prev;
      }
      return reached;
    },
    { t: target, c: ctrl },
  );
}
