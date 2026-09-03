// @vitest-environment jsdom
//
// The sidebar header's "Check for updates" button (spec 323). Before it
// the only manual trigger was the macOS app menu's "Check for Updates…"
// item — invisible on every other platform, undiscoverable on that one.
//
// The button is built at runtime by initBanners() from the iconButton()
// primitive rather than written into index.html, so these tests are the
// only place its markup, its accessible name and its placement next to
// #new-project-btn are pinned down.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { appStore as AppStore } from '../../src/store/store.js';

const bridge = vi.hoisted(() => ({
  Confirm: vi.fn(() => Promise.resolve(true)),
  RestartDaemon: vi.fn(() => Promise.resolve()),
  CheckForUpdate: vi.fn(() => Promise.resolve(null)),
  StartUpdate: vi.fn(() => Promise.resolve()),
  ApplyUpdateAndRestart: vi.fn(() => Promise.resolve()),
  OpenURL: vi.fn(() => Promise.resolve()),
  EventsOn: vi.fn(),
}));
vi.mock('../../src/bridge.js', () => bridge);

const settle = () => new Promise((r) => setTimeout(r, 0));

// A fresh module registry per test: initBanners() guards against a
// double append, and manualUpdateCheck()'s in-flight flag is module
// state that the "two rapid clicks" case has to start clean.
// `store` is re-read from the fresh registry on every init: vi.resetModules()
// gives banners.ts a NEW store module, and a top-level import here would hold
// the stale instance whose banner state nothing ever writes.
let store: typeof AppStore;

async function init(withHeader: boolean) {
  document.body.innerHTML = `
    <div id="app">
      <div id="terms"></div>
      <aside id="sidebar">
        <header>
          <span class="brand">Hive</span>
          ${withHeader ? '<button id="new-project-btn" type="button" class="hv-icon-btn" data-size="22"></button>' : ''}
        </header>
        <ul id="projects"></ul>
      </aside>
      <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    </div>`;
  // A fresh registry means a fresh store; no reset needed.
  ({ appStore: store } = await import('../../src/store/store.js'));
  const mod = await import('../../src/app/banners.js');
  mod.initBanners();
  // wireUpdateBanner() fires a boot-time CheckForUpdate(); clear it so
  // the click assertions below count only the clicks.
  await settle();
  bridge.CheckForUpdate.mockClear();
  return mod;
}

function btn(): HTMLButtonElement {
  const el = document.getElementById('check-updates-btn');
  if (!el) throw new Error('missing #check-updates-btn');
  return el as HTMLButtonElement;
}

const updateBannerText = () => store.getState().banners.update.text;

beforeEach(() => {
  vi.resetModules();
  bridge.CheckForUpdate.mockReset().mockResolvedValue(null);
  bridge.EventsOn.mockClear();
});

afterEach(() => {
  document.body.innerHTML = '';
});

describe('sidebar check-for-updates button', () => {
  it('is added to the sidebar header next to New project', async () => {
    await init(true);
    const el = btn();
    expect(el.classList.contains('hv-icon-btn')).toBe(true);
    expect(el.dataset.size).toBe('22');
    expect(el.getAttribute('aria-label')).toBe('Check for updates');
    expect(el.title).toBe('Check for updates');
    expect(document.getElementById('new-project-btn')?.nextElementSibling).toBe(
      el,
    );
    // The icon comes from the shared sprite, never inline SVG paths.
    const use = el.querySelector('svg.hv-icon use');
    expect(use?.getAttribute('href')).toBe('#hv-download');
  });

  it('is a no-op when the sidebar header is not mounted', async () => {
    // update-banner.test.tsx and restart-hive.test.tsx both call
    // initBanners() on a scaffold with no header; it must not throw.
    await expect(init(false)).resolves.toBeDefined();
    expect(document.getElementById('check-updates-btn')).toBeNull();
  });

  it('is not appended twice when initBanners runs again', async () => {
    const mod = await init(true);
    mod.initBanners();
    expect(document.querySelectorAll('#check-updates-btn')).toHaveLength(1);
  });

  it('runs a manual update check on click', async () => {
    await init(true);
    btn().click();
    expect(updateBannerText()).toBe('Checking for updates…');
    expect(bridge.CheckForUpdate).toHaveBeenCalledTimes(1);
    await settle();
  });

  it('does not fire parallel checks on rapid clicks', async () => {
    await init(true);
    btn().click();
    btn().click();
    expect(bridge.CheckForUpdate).toHaveBeenCalledTimes(1);
    await settle();
  });

  it('surfaces a failed check in the update banner', async () => {
    await init(true);
    bridge.CheckForUpdate.mockRejectedValueOnce(new Error('network down'));
    btn().click();
    await settle();
    expect(updateBannerText()).toMatch(/^Update check failed:/);
  });
});
