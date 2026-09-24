import { expect, type Page, test } from '@playwright/test';
import { PRESETS } from '../../src/theme/theme';

// Sidebar legibility (spec 455), in a real engine. Every claim here is
// about paint or geometry, which jsdom cannot see:
//   - the selected row is an --accent fill read in --on-accent;
//   - a selected row that wants attention still pulses, in the ink;
//   - worktree members sit behind a rail in the group's colour;
//   - the session list has no side gutters, and the resizer sits outside
//     the sidebar so the colour bar at the list's edge is reachable;
//   - a project header carries a 2px rule in the project's colour;
//   - hairlines and rules are the only separators: no margin around a
//     group or before a project, and no doubled line where two meet.
// Colours are compared against tokens RESOLVED BY THE BROWSER (a probe
// painted with var(--x)), never against hand-copied hex.

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';
const FIRST_PARTY = PRESETS.filter((p) => p.id !== 'system');

async function boot(page: Page, theme?: string) {
  if (theme) {
    await page.addInitScript(
      (t) => localStorage.setItem('hive.theme', t),
      theme,
    );
  }
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

const selected = (page: Page) =>
  page.locator('#projects .hv-session-row[data-selected]');

// A token's value as the browser resolves it, in the same notation
// getComputedStyle reports a painted colour in.
function token(page: Page, name: string, prop = 'color') {
  return page.evaluate(
    ([n, p]) => {
      const probe = document.createElement('span');
      probe.style.setProperty(p, `var(${n})`);
      document.getElementById('sidebar')?.appendChild(probe);
      const v = getComputedStyle(probe).getPropertyValue(p);
      probe.remove();
      return v;
    },
    [name, prop] as const,
  );
}

type RGB = { r: number; g: number; b: number; a: number };
function parse(painted: string): RGB {
  const nums = painted.match(/-?[\d.]+/g);
  if (!nums || nums.length < 3) throw new Error(`unparsed: ${painted}`);
  const [r, g, b] = nums.slice(0, 3).map(Number.parseFloat);
  const a = nums.length > 3 ? Number.parseFloat(nums[3]) : 1;
  const scale = painted.startsWith('color(') ? 255 : 1;
  return { r: r * scale, g: g * scale, b: b * scale, a };
}
function luminance({ r, g, b }: RGB) {
  const lin = (c: number) => {
    const s = c / 255;
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}
function contrast(a: string, b: string) {
  const [la, lb] = [luminance(parse(a)), luminance(parse(b))];
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

async function selectedSid(page: Page) {
  const sid = await selected(page).getAttribute('data-sid');
  if (!sid) throw new Error('no selected row');
  return sid;
}

// A worktree with two sessions in it, plus the default ungrouped session.
async function seedGroup(page: Page) {
  await page.evaluate(() =>
    window.__hive.createSessionWithWorktree?.('alpha', 'feat/rail'),
  );
  await page.waitForFunction(() =>
    (window.__hive.state?.sessions ?? []).some((s) => !!s.worktree_path),
  );
  const wt = await page.evaluate(
    () =>
      (window.__hive.state?.sessions ?? []).find((s) => !!s.worktree_path)
        ?.worktree_path ?? '',
  );
  await page.evaluate(
    (p) => window.__hive.createSessionInWorktree?.('beta', p),
    wt,
  );
  await page.waitForSelector('.hv-worktree-group__rows > li.hv-session-row');
}

async function setDensity(page: Page, value: string) {
  await page.keyboard.press(`${MOD}+,`);
  await page.locator('#settings-tab-appearance').click();
  await expect(page.locator('#settings-density')).toBeVisible();
  await page.locator('#settings-density').selectOption(value);
  await page.keyboard.press('Escape');
  await expect(page.locator('#settings-density')).toBeHidden();
}

test.describe('selected row', () => {
  for (const { id } of FIRST_PARTY) {
    test(`${id}: fills with --accent and reads in --on-accent at AA`, async ({
      page,
    }) => {
      await boot(page, id);
      const row = selected(page);
      await expect(row).toHaveCount(1);
      const [bg, ink] = await row.evaluate((el) => {
        const cs = getComputedStyle(el);
        return [cs.backgroundColor, cs.color];
      });
      expect(bg).toBe(await token(page, '--accent', 'background-color'));
      expect(ink).toBe(await token(page, '--on-accent'));
      // The claim the fill exists to make, computed rather than trusted:
      // contrast-exempt presets included, because the selected row reads
      // in --on-accent on every one of them.
      expect(contrast(ink, bg)).toBeGreaterThanOrEqual(4.5);

      // The state icon keeps its state colour and sits on a --surface
      // disc, the ground state colours are gated on — so it neither goes
      // silent on the fill nor loses its colour-coding.
      const icon = await row.locator('.hv-state-icon').evaluate((el) => {
        const cs = getComputedStyle(el);
        return { color: cs.color, disc: cs.backgroundColor };
      });
      expect(icon.disc).toBe(
        await token(page, '--surface', 'background-color'),
      );
      expect(icon.color).toBe(await token(page, '--state-running'));
      // Gated where the preset is gated: a --contrast-exempt port keeps
      // upstream's state colours, which fail on its own --surface on EVERY
      // row (themes.md) — the disc is the same ground, so no worse here.
      const exempt = await page.evaluate(
        () =>
          getComputedStyle(document.documentElement)
            .getPropertyValue('--contrast-exempt')
            .trim() === '1',
      );
      if (!exempt) {
        expect(contrast(icon.color, icon.disc)).toBeGreaterThanOrEqual(3);
      }

      // Every other piece of text on the row — the key hint in particular
      // sets its own --fg-subtle (kbd.css) — reads in the ink.
      const inks = await row.evaluate((el) =>
        Array.from(
          el.querySelectorAll<HTMLElement>(
            '.hv-kbd, .hv-session-row__agent, .hv-session-row__name',
          ),
        ).map((n) => getComputedStyle(n).color),
      );
      expect(inks.length).toBeGreaterThanOrEqual(2);
      for (const c of inks) expect(c).toBe(ink);
    });
  }

  test('keeps the fill under hover, and hover is not selection', async ({
    page,
  }) => {
    await boot(page);
    await page.evaluate(() => window.__hive.addSession?.('second'));
    const rows = page.locator('#projects .hv-session-row');
    await expect(rows).toHaveCount(2);
    const accent = await token(page, '--accent', 'background-color');

    await selected(page).hover();
    await expect(selected(page)).toHaveCSS('background-color', accent);

    const other = page.locator(
      '#projects .hv-session-row:not([data-selected])',
    );
    await other.hover();
    const hovered = await other.evaluate(
      (el) => getComputedStyle(el).backgroundColor,
    );
    expect(hovered).not.toBe(accent);
  });

  for (const density of ['normal', 'tight', 'compact']) {
    test(`${density}: attention and exited rows still read in the ink when selected`, async ({
      page,
    }) => {
      await boot(page);
      if (density !== 'normal') await setDensity(page, density);
      const sid = await selectedSid(page);
      const ink = await token(page, '--on-accent');
      const row = selected(page);
      // A title, so compact shows __sub as line 1.
      await page.evaluate((id) => {
        const s = window.__hive.state?.sessions.find((x) => x.id === id);
        if (!s) throw new Error('no mock session');
        s.title = 'Reviewing the plan';
        window.__hive.emit(
          'session:event',
          JSON.stringify({ kind: 'title', session: s }),
        );
      }, sid);

      // Waiting for input: the daemon's own state, which selection does
      // not clear (unlike a bell).
      await page.evaluate(
        (id) => window.__hive.setSessionState?.(id, 'waiting_input'),
        sid,
      );
      await expect(row).toHaveAttribute('data-state', 'attention');
      await expect(row.locator('.hv-session-row__name')).toHaveCSS(
        'color',
        ink,
      );
      await expect(row.locator('.hv-session-row__sub')).toHaveCSS('color', ink);

      await page.evaluate((id) => {
        const s = window.__hive.state?.sessions.find((x) => x.id === id);
        if (!s) throw new Error('no mock session');
        s.alive = false;
        window.__hive.emit(
          'session:event',
          JSON.stringify({ kind: 'updated', session: s }),
        );
      }, sid);
      await expect(row).toHaveAttribute('data-state', 'exited');
      const name = row.locator('.hv-session-row__name');
      await expect(name).toHaveCSS('color', ink);
      await expect(row.locator('.hv-session-row__sub')).toHaveCSS('color', ink);
      if (density !== 'compact') {
        await expect(name).toHaveCSS('text-decoration-line', 'line-through');
      }
    });
  }

  // The pulse is a border, never a tint over the fill: any tint lowers
  // the ink's contrast (an 18% ink tint took Solarized to 3.79:1).
  async function attentionBorder(page: Page) {
    const sid = await selectedSid(page);
    await page.evaluate(
      (id) => window.__hive.setSessionState?.(id, 'waiting_input'),
      sid,
    );
    const row = selected(page);
    await expect(row).toHaveAttribute('data-state', 'attention');
    return row.evaluate((el) => {
      const cs = getComputedStyle(el, '::after');
      return {
        anim: cs.animationName,
        opacity: cs.opacity,
        bg: cs.backgroundColor,
        shadow: cs.boxShadow,
      };
    });
  }

  for (const { id } of FIRST_PARTY) {
    test(`${id}: a selected row that wants attention pulses a border in the ink, leaving the fill alone`, async ({
      page,
    }) => {
      await boot(page, id);
      const after = await attentionBorder(page);
      expect(after.anim).toBe('hv-attn-tint');
      expect(parse(after.bg).a).toBe(0);
      expect(after.shadow).toContain(await token(page, '--on-accent'));
      expect(after.shadow).toMatch(/inset/);
      expect(after.shadow).toMatch(/\b2px\b/);
      // The ground the text actually sits on is still the plain fill, so
      // the AA check in the fill test holds while the row pulses.
      const row = selected(page);
      const [bg, ink] = await row.evaluate((el) => {
        const cs = getComputedStyle(el);
        return [cs.backgroundColor, cs.color];
      });
      expect(contrast(ink, bg)).toBeGreaterThanOrEqual(4.5);
      // The icon itself stays the attention colour, on its disc: "needs
      // you" keeps its own colour on the selected row too.
      await expect(row.locator('.hv-state-icon')).toHaveCSS(
        'color',
        await token(page, '--state-attention'),
      );
    });
  }

  test('with motion off the selected attention border is static and painted', async ({
    page,
  }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await boot(page);
    const after = await attentionBorder(page);
    expect(after.anim).toBe('none');
    expect(after.opacity).toBe('1');
    expect(parse(after.bg).a).toBe(0);
    expect(after.shadow).toContain(await token(page, '--on-accent'));
  });

  test('the plan pie stays visible on the fill, stale or live', async ({
    page,
  }) => {
    await boot(page);
    const sid = await selectedSid(page);
    await page.evaluate(
      (id) => window.__hive.setSessionPlan?.(id, 2, 4, 'Bash'),
      sid,
    );
    await page.evaluate(
      (id) => window.__hive.setSessionState?.(id, 'working', 'hook'),
      sid,
    );
    const pie = selected(page).locator('.hv-session-row__plan');
    await expect(pie).toBeVisible();
    const bg = await token(page, '--accent', 'background-color');
    const ring = async () =>
      pie.evaluate((el) => {
        // The outline is an inset box-shadow in --hv-plan-color.
        const m = getComputedStyle(el).boxShadow.match(
          /(rgba?\([^)]*\)|color\([^)]*\))/,
        );
        return m ? m[1] : '';
      });
    const live = await ring();
    expect(live).not.toBe('');
    expect(contrast(live, bg)).toBeGreaterThanOrEqual(3);

    // Stale: the hook tier went quiet. Unselected, this is --fg-subtle
    // !important — about 1.3:1 on the fill.
    await page.evaluate(
      (id) => window.__hive.setSessionState?.(id, 'working', 'heuristic'),
      sid,
    );
    await expect(pie).toHaveClass(/hv-session-row__plan--stale/);
    const stale = await ring();
    expect(stale).not.toBe(live);
    const s = parse(stale);
    const b = parse(bg);
    // Composite the 55% ink over the fill before measuring.
    const over = `rgb(${s.r * s.a + b.r * (1 - s.a)}, ${s.g * s.a + b.g * (1 - s.a)}, ${s.b * s.a + b.b * (1 - s.a)})`;
    expect(contrast(over, bg)).toBeGreaterThanOrEqual(2);
  });
});

test.describe('worktree group rail', () => {
  test('members sit behind a 1px rail in the group colour', async ({
    page,
  }) => {
    await boot(page);
    await seedGroup(page);
    const rail = await page
      .locator('.hv-worktree-group__rows')
      .first()
      .evaluate((el) => {
        const cs = getComputedStyle(el);
        const probe = document.createElement('span');
        probe.style.color = 'var(--session-color)';
        el.closest('.hv-worktree-group')?.appendChild(probe);
        const group = getComputedStyle(probe).color;
        probe.remove();
        return {
          width: cs.borderLeftWidth,
          style: cs.borderLeftStyle,
          color: cs.borderLeftColor,
          radius: cs.borderRadius,
          group,
        };
      });
    expect(rail.width).toBe('1px');
    expect(rail.style).toBe('solid');
    expect(rail.color).toBe(rail.group);
    expect(rail.radius).toBe('0px');

    const x = async (sel: string) => {
      const b = await page.locator(sel).first().boundingBox();
      if (!b) throw new Error(`${sel} has no box`);
      return b.x;
    };
    const grouped = await x(
      '.hv-worktree-group__rows > li.hv-session-row .hv-session-row__state',
    );
    const loose = await x(
      '.hv-project-card__rows > li.hv-session-row .hv-session-row__state',
    );
    expect(grouped - loose).toBeGreaterThanOrEqual(8);
  });

  test('the group has no raised ground of its own', async ({ page }) => {
    await boot(page);
    await seedGroup(page);
    const [panel, raised] = await page
      .locator('.hv-worktree-group')
      .first()
      .evaluate((el) => {
        const probe = document.createElement('span');
        probe.style.backgroundColor = 'var(--surface-raised)';
        el.appendChild(probe);
        const r = getComputedStyle(probe).backgroundColor;
        probe.remove();
        return [getComputedStyle(el).backgroundColor, r];
      });
    expect(panel).not.toBe(raised);
    expect(parse(panel).a).toBe(0);
  });

  test('ungrouped rows have no rail', async ({ page }) => {
    await boot(page);
    await seedGroup(page);
    const w = await page
      .locator('.hv-project-card__rows')
      .first()
      .evaluate((el) => getComputedStyle(el).borderLeftWidth);
    expect(w).toBe('0px');
  });

  test('the header speaks in the group colour', async ({ page }) => {
    await boot(page);
    await seedGroup(page);
    const [branch, muted] = await page
      .locator('.hv-worktree-group__branch')
      .first()
      .evaluate((el) => {
        const probe = document.createElement('span');
        probe.style.color = 'var(--fg-muted)';
        document.body.appendChild(probe);
        const m = getComputedStyle(probe).color;
        probe.remove();
        return [getComputedStyle(el).color, m];
      });
    expect(branch).not.toBe(muted);
  });
});

test.describe('sidebar edges', () => {
  test('the session list has no side gutters', async ({ page }) => {
    await boot(page);
    const pad = await page
      .locator('#projects')
      .evaluate((el) => [
        getComputedStyle(el).paddingLeft,
        getComputedStyle(el).paddingRight,
      ]);
    expect(pad).toEqual(['0px', '0px']);
  });

  test('the resizer sits outside the sidebar; the colour bar is reachable at rest', async ({
    page,
  }) => {
    await boot(page);
    const geo = await page.evaluate(() => {
      const sidebar = document.getElementById('sidebar');
      const handle = document.getElementById('sidebar-resizer');
      if (!sidebar || !handle) throw new Error('missing sidebar/resizer');
      return {
        inside: sidebar.contains(handle),
        sidebarRight: sidebar.getBoundingClientRect().right,
        handle: handle.getBoundingClientRect().toJSON(),
      };
    });
    expect(geo.inside).toBe(false);
    expect(geo.handle.width).toBeGreaterThan(0);
    expect(geo.handle.left).toBeGreaterThanOrEqual(geo.sidebarRight - 0.5);

    // An UNHOVERED row: move the pointer well away first, so the bar is
    // at its 3px resting width.
    await page.mouse.move(600, 5);
    const hit = await page
      .locator('#projects .hv-session-row .hv-session-row__colour')
      .first()
      .evaluate((bar) => {
        const r = bar.getBoundingClientRect();
        const el = document.elementFromPoint(
          r.left + r.width / 2,
          r.top + r.height / 2,
        );
        return !!el && (el === bar || bar.contains(el));
      });
    expect(hit).toBe(true);
  });

  test('a hidden sidebar leaves no resizer strip', async ({ page }) => {
    await boot(page);
    await page.keyboard.press(`${MOD}+s`);
    await expect(page.locator('#app')).toHaveClass(/sidebar-hidden/);
    await expect(page.locator('#sidebar-resizer')).toBeHidden();
  });
});

test.describe('project header', () => {
  test('carries a 2px rule in the project colour, at unchanged height', async ({
    page,
  }) => {
    await boot(page);
    const got = await page
      .locator('.hv-project-card__header')
      .first()
      .evaluate((el) => {
        const probe = document.createElement('span');
        probe.style.color = 'var(--project-color)';
        el.appendChild(probe);
        const project = getComputedStyle(probe).color;
        probe.remove();
        return {
          shadow: getComputedStyle(el).boxShadow,
          height: el.getBoundingClientRect().height,
          project,
        };
      });
    expect(got.shadow).toContain(got.project);
    expect(got.shadow).toMatch(/inset/);
    expect(got.shadow).toMatch(/\b0px 2px 0px\b/);
    // 26px content box + the 1px bottom hairline, as before.
    expect(got.height).toBeCloseTo(27, 0);
  });
});

test.describe('separators', () => {
  // p1: main, group feat/a, group feat/b — so p1 ENDS in a group — then p2.
  async function seed(page: Page) {
    await boot(page);
    // A group needs two sessions sharing one worktree.
    for (const branch of ['feat/a', 'feat/b']) {
      await page.evaluate(
        (b) => window.__hive.createSessionWithWorktree?.(`w-${b}`, b),
        branch,
      );
      const wt = await page.waitForFunction(
        (b) =>
          window.__hive.state?.sessions.find((s) => s.worktree_branch === b)
            ?.worktree_path,
        branch,
      );
      await page.evaluate(
        ([b, p]) => window.__hive.createSessionInWorktree?.(`w2-${b}`, p),
        [branch, (await wt.jsonValue()) as string],
      );
    }
    await expect(page.locator('.hv-worktree-group')).toHaveCount(2);
    await page.evaluate(async () => {
      const p = {
        id: 'p2',
        name: 'other',
        color: '#f80',
        cwd: '',
        order: 1,
        created: new Date().toISOString(),
      };
      window.__hive.state?.projects.push(p);
      window.__hive.emit(
        'project:event',
        JSON.stringify({ kind: 'added', project: p }),
      );
      await window.__hive.addSession?.('t1', undefined, 'p2');
    });
    await expect(page.locator('#projects > li.hv-project-card')).toHaveCount(2);
  }

  const geom = (page: Page) =>
    page.evaluate(() => {
      const r = (el: Element) => el.getBoundingClientRect();
      const groups = [...document.querySelectorAll('.hv-worktree-group')];
      const cards = [
        ...document.querySelectorAll('#projects > li.hv-project-card'),
      ];
      return {
        groups: groups.map((g) => {
          const cs = getComputedStyle(g);
          return {
            top: r(g).top,
            bottom: r(g).bottom,
            margin: [cs.marginTop, cs.marginBottom],
            borderTop: cs.borderTopWidth,
            borderBottom: cs.borderBottomWidth,
          };
        }),
        cards: cards.map((c) => ({
          top: r(c).top,
          bottom: r(c).bottom,
          marginTop: getComputedStyle(c).marginTop,
        })),
      };
    });

  test('a worktree group has no margin; neighbouring groups share one hairline', async ({
    page,
  }) => {
    await seed(page);
    const g = await geom(page);
    for (const grp of g.groups) expect(grp.margin).toEqual(['0px', '0px']);
    const [a, b] = g.groups;
    expect(b.top).toBeCloseTo(a.bottom, 1);
    expect(b.borderTop).toBe('0px');
  });

  test("a group ending a project gives its hairline up to the next project's rule", async ({
    page,
  }) => {
    await seed(page);
    const g = await geom(page);
    const [p1, p2] = g.cards;
    const lastGroup = g.groups[1];
    expect(lastGroup.borderBottom).toBe('0px');
    expect(p1.bottom).toBeCloseTo(lastGroup.bottom, 1);
    expect(p2.marginTop).toBe('0px');
    expect(p2.top).toBeCloseTo(p1.bottom, 1);
  });
});
