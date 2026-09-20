// The Help modal and the sidebar button that opens it.
//
// Modelled on whats-new.test.tsx's harness — same header scaffold, same
// portal-into-the-real-root mount. What this suite is about is the wiring
// the plan's success criteria name: the button's position, the close path's
// focus return, the handoff to the ⌘/ overlay, and the fact that the links
// leave through the bridge rather than navigating the webview.
import { act, render, cleanup } from '@testing-library/react';
import { createPortal } from 'react-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { resetStore } from '../../src/store/store.js';

const openURL = vi.fn((_url: string) => Promise.resolve());
// Only OpenURL is reached from this modal; the rest of the bridge is not
// imported by anything in this tree.
vi.mock('../../src/bridge.js', () => ({
  OpenURL: (url: string) => openURL(url),
}));

// Both dialog roots: the handoff test needs the ⌘/ overlay's root to exist
// so the overlay it hands off to has somewhere to render.
const HTML = `
  <div id="app">
    <header><span class="brand">Hive</span>
      <button id="new-project-btn" type="button" class="hv-icon-btn" data-size="22"></button>
    </header>
    <ul id="projects"></ul>
    <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    <div id="terms"></div><div id="minimized-tray"></div><div id="empty-state"></div>
    <div id="help-modal" class="hv-dialog hidden" role="dialog" aria-modal="true"
         aria-labelledby="help-modal-title"></div>
    <div id="help-overlay" class="hv-dialog hidden" role="dialog" aria-modal="true"
         aria-labelledby="help-overlay-title"></div>
  </div>`;

async function mount() {
  document.body.innerHTML = HTML;
  const { SidebarHeaderControls } = await import(
    '../../src/components/Sidebar.js'
  );
  const { Help } = await import('../../src/components/modals/Help.js');
  const { HelpOverlay } = await import(
    '../../src/components/modals/HelpOverlay.js'
  );
  const root = document.getElementById('help-modal') as HTMLElement;
  const overlayRoot = document.getElementById('help-overlay') as HTMLElement;
  return render(
    <>
      <SidebarHeaderControls />
      {createPortal(<Help root={root} />, root)}
      {createPortal(<HelpOverlay root={overlayRoot} />, overlayRoot)}
    </>,
  );
}

const helpBtn = () => document.getElementById('help-btn') as HTMLButtonElement;
const dialog = () => document.getElementById('help-modal') as HTMLElement;
const overlay = () => document.getElementById('help-overlay') as HTMLElement;
const open = () => document.getElementById('help-modal') !== null;

beforeEach(() => {
  localStorage.clear();
  openURL.mockClear();
  resetStore();
});
afterEach(cleanup);

describe('the sidebar help button', () => {
  it('renders fourth in the header, after the gift', async () => {
    await mount();
    const ids = [...document.querySelectorAll('header button')].map(
      (b) => b.id,
    );
    expect(ids).toEqual([
      'new-project-btn',
      'check-updates-btn',
      'whats-new-btn',
      'help-btn',
    ]);
  });

  it('carries the accessible name and the 22px icon-button size', async () => {
    await mount();
    expect(helpBtn().getAttribute('aria-label')).toBe('Help');
    expect(helpBtn().getAttribute('title')).toBe('Help');
    expect(helpBtn().dataset.size).toBe('22');
  });

  it('renders nothing on a scaffold with no sidebar header', async () => {
    document.body.innerHTML = '<div id="app"></div>';
    const { SidebarHeaderControls } = await import(
      '../../src/components/Sidebar.js'
    );
    expect(() => render(<SidebarHeaderControls />)).not.toThrow();
    expect(document.getElementById('help-btn')).toBeNull();
  });
});

describe('the Help modal', () => {
  it('is hidden until the button is clicked', async () => {
    await mount();
    expect(dialog().classList.contains('hidden')).toBe(true);
    await act(async () => {
      helpBtn().click();
    });
    expect(dialog().classList.contains('hidden')).toBe(false);
    expect(document.getElementById('help-modal-title')?.textContent).toBe(
      'Help',
    );
  });

  it('closes on Escape AND hands focus back to the terminal', async () => {
    // The spy is the point. app/modals/help.ts defaults its deps to no-ops,
    // so a test that asserted only "the dialog closed" would pass on an
    // implementation that never returns focus — the terminal would stay
    // dead after every close.
    const focusActiveTerm = vi.fn();
    const { initHelp, closeHelp } = await import(
      '../../src/app/modals/help.js'
    );
    initHelp({ setFocusedTile: () => {}, focusActiveTerm });
    await mount();
    await act(async () => {
      helpBtn().click();
    });
    expect(dialog().classList.contains('hidden')).toBe(false);

    await act(async () => {
      closeHelp();
    });
    expect(dialog().classList.contains('hidden')).toBe(true);
    expect(focusActiveTerm).toHaveBeenCalledTimes(1);
    // Ordering: closeHelp flushes the close synchronously, so by the time
    // focusActiveTerm ran the dialog was already hidden. If it were the
    // other way round the terminal would take focus behind a visible
    // backdrop and lose it again on the next paint.
    expect(dialog().classList.contains('hidden')).toBe(true);
  });

  it('explains the core concepts', async () => {
    await mount();
    await act(async () => {
      helpBtn().click();
    });
    const terms = [...dialog().querySelectorAll('.help-concepts dt')].map(
      (el) => el.textContent,
    );
    expect(terms).toEqual(['Project', 'Session', 'Worktree', 'Agent']);
  });

  it('shows the shortcuts binding and hands off to the overlay', async () => {
    await mount();
    await act(async () => {
      helpBtn().click();
    });
    const row = document.getElementById(
      'help-shortcuts-row',
    ) as HTMLButtonElement;
    // The binding rides next to the action it triggers, per AGENTS.md.
    expect(row.querySelector('.hv-kbd')?.textContent).toMatch(/\/$/);

    await act(async () => {
      row.click();
    });
    expect(dialog().classList.contains('hidden')).toBe(true);
    expect(overlay().classList.contains('hidden')).toBe(false);
  });

  it('opens links through the bridge, never as a webview navigation', async () => {
    await mount();
    await act(async () => {
      helpBtn().click();
    });
    // An <a href> inside the dialog would navigate the Wails webview out of
    // the app with no way back, so there must not be one.
    expect(dialog().querySelectorAll('a[href]').length).toBe(0);

    const links = [
      ...dialog().querySelectorAll<HTMLButtonElement>('.help-links .help-row'),
    ];
    expect(links.map((b) => b.textContent)).toEqual([
      'README',
      'Documentation',
      'Report an issue',
      'Releases',
    ]);
    await act(async () => {
      links[2].click();
    });
    expect(openURL).toHaveBeenCalledWith(
      'https://github.com/lucascaro/hive/issues/new',
    );
  });

  it('opens from the command palette path, not only the button', async () => {
    // main.tsx's palette entry calls openHelp() directly; a component-local
    // open flag the palette cannot reach would leave the modal shut.
    await mount();
    const { openHelp } = await import('../../src/app/modals/help.js');
    await act(async () => {
      openHelp();
    });
    expect(dialog().classList.contains('hidden')).toBe(false);
  });

  it('is a no-op when it is already open', async () => {
    await mount();
    const { openHelp } = await import('../../src/app/modals/help.js');
    await act(async () => {
      openHelp();
      openHelp();
    });
    expect(open()).toBe(true);
    expect(
      document.querySelectorAll('#help-modal .hv-dialog__panel').length,
    ).toBe(1);
  });
});
