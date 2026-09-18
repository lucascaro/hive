// @vitest-environment jsdom
//
// The sidebar header's "Check for updates" button (spec 323). Before it
// the only manual trigger was the macOS app menu's "Check for Updates…"
// item — invisible on every other platform, undiscoverable on that one.
//
// The button is rendered by components/Sidebar.tsx › SidebarHeaderControls
// rather than written into index.html, so these tests are the only place
// its markup, its accessible name and its placement next to
// #new-project-btn are pinned down. (It was built by initBanners() from
// the imperative iconButton() until that primitive was deleted with the
// rest of src/ui/.)
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render } from '@testing-library/react';
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

// A fresh module registry per test: manualUpdateCheck()'s in-flight flag
// is module state that the "two rapid clicks" case has to start clean.
// `store` is re-read from the fresh registry on every mount:
// vi.resetModules() gives banners.ts a NEW store module, and a top-level
// import here would hold the stale instance whose banner state nothing
// ever writes.
let store: typeof AppStore;

async function mount(withHeader: boolean) {
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
  const { SidebarHeaderControls } = await import(
    '../../src/components/Sidebar.js'
  );
  return render(<SidebarHeaderControls />);
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
  it('is rendered into the sidebar header next to New project', async () => {
    await mount(true);
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

  it('fills in the New project button’s icon', async () => {
    // index.html owns that button (initProjectEditor wires its click and
    // the launcher uses it as a focus fallback); only the icon is React's.
    await mount(true);
    expect(
      document
        .getElementById('new-project-btn')
        ?.querySelector('svg.hv-icon use')
        ?.getAttribute('href'),
    ).toBe('#hv-plus');
  });

  it('renders nothing when the sidebar header is not mounted', async () => {
    // The dom scaffolds in update-banner.test.tsx and restart-hive.test.tsx
    // have no sidebar header; a missing one must render nothing rather
    // than throw.
    await expect(mount(false)).resolves.toBeDefined();
    expect(document.getElementById('check-updates-btn')).toBeNull();
  });

  it('stays a single button across a re-render', async () => {
    const { rerender } = await mount(true);
    const { SidebarHeaderControls } = await import(
      '../../src/components/Sidebar.js'
    );
    rerender(<SidebarHeaderControls />);
    expect(document.querySelectorAll('#check-updates-btn')).toHaveLength(1);
  });

  it('runs a manual update check on click', async () => {
    await mount(true);
    btn().click();
    expect(updateBannerText()).toBe('Checking for updates…');
    expect(bridge.CheckForUpdate).toHaveBeenCalledTimes(1);
    await settle();
  });

  it('does not fire parallel checks on rapid clicks', async () => {
    await mount(true);
    btn().click();
    btn().click();
    expect(bridge.CheckForUpdate).toHaveBeenCalledTimes(1);
    await settle();
  });

  it('surfaces a failed check in the update banner', async () => {
    await mount(true);
    bridge.CheckForUpdate.mockRejectedValueOnce(new Error('network down'));
    btn().click();
    await settle();
    expect(updateBannerText()).toMatch(/^Update check failed:/);
  });
});

// #436: a background check reports here, as a dot, instead of raising the
// update banner. The dot is the same `hv-unread` ::after the What's New
// gift uses, so the accessible name has to carry it in words.
describe('check-for-updates pip', () => {
  const AVAILABLE = {
    available: true,
    canApply: true,
    current: '2.4.0',
    latest: '2.5.0',
    stage: 'available',
    channel: 'release',
  };

  async function initBanners() {
    const banners = await import('../../src/app/banners.js');
    await act(async () => {
      banners.initBanners();
      await settle();
    });
  }

  function emit(event: string, payload: unknown) {
    act(() => {
      for (const call of bridge.EventsOn.mock.calls) {
        if (call[0] === event) (call[1] as (p: unknown) => void)(payload);
      }
    });
  }

  it('shows the unread dot and says so when an update is pending', async () => {
    await mount(true);
    expect(btn().classList.contains('hv-unread')).toBe(false);
    act(() => store.setState({ updatePending: true }));
    expect(btn().classList.contains('hv-unread')).toBe(true);
    expect(btn().getAttribute('aria-label')).toBe(
      'Check for updates — update available',
    );
    act(() => store.setState({ updatePending: false }));
    expect(btn().classList.contains('hv-unread')).toBe(false);
    expect(btn().getAttribute('aria-label')).toBe('Check for updates');
  });

  it('a background update:available puts the dot on the button, not a banner', async () => {
    await mount(true);
    await initBanners();
    emit('update:available', AVAILABLE);
    expect(btn().classList.contains('hv-unread')).toBe(true);
    expect(store.getState().banners.update.visible).toBe(false);
  });

  it('the boot poll sets the dot, and clears it when nothing is pending', async () => {
    bridge.CheckForUpdate.mockResolvedValueOnce(AVAILABLE as never);
    await mount(true);
    await initBanners();
    expect(btn().classList.contains('hv-unread')).toBe(true);
    expect(store.getState().banners.update.visible).toBe(false);

    // A fresh boot on the installed version: the poll reports nothing
    // pending and the dot goes.
    vi.resetModules();
    bridge.CheckForUpdate.mockResolvedValueOnce({
      available: false,
      current: '2.5.0',
    } as never);
    await mount(true);
    await act(() => store.setState({ updatePending: true }));
    await initBanners();
    expect(btn().classList.contains('hv-unread')).toBe(false);
  });

  it('clicking the dotted button shows the available version', async () => {
    await mount(true);
    await initBanners();
    emit('update:available', AVAILABLE);
    bridge.CheckForUpdate.mockResolvedValueOnce(AVAILABLE as never);
    await act(async () => {
      btn().click();
      await settle();
    });
    expect(store.getState().banners.update.visible).toBe(true);
    expect(updateBannerText()).toContain('2.5.0');
  });
});
