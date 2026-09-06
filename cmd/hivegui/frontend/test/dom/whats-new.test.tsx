// The What's New modal and the sidebar gift that opens it.
// Unit-level grouping/ordering rules live in test/unit/whats-new.test.ts;
// this suite is about the wiring — button, dot, open, close, read receipt.
import { act, render, cleanup } from '@testing-library/react';
import { createPortal } from 'react-dom';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  SEEN_KEY,
  latestVersion,
  UNRELEASED,
} from '../../src/lib/whats-new.js';
import { resetStore } from '../../src/store/store.js';

// The store hydrates the seen version in initialData(), so a test that wants
// a pre-existing receipt has to write it BEFORE resetting, not before mount.
function seedSeen(version: string) {
  localStorage.setItem(SEEN_KEY, version);
  resetStore();
}

// The sidebar header the controls portal into, plus the dialog root
// index.html owns. app/dom.ts resolves #terms/#status at import time, so the
// markup has to exist before the components are imported.
const HTML = `
  <div id="app">
    <header><span class="brand">Hive</span>
      <button id="new-project-btn" type="button" class="hv-icon-btn" data-size="22"></button>
    </header>
    <ul id="projects"></ul>
    <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    <div id="terms"></div><div id="minimized-tray"></div><div id="empty-state"></div>
    <div id="whats-new" class="hv-dialog hidden" role="dialog" aria-modal="true"
         aria-labelledby="whats-new-title"></div>
  </div>`;

async function mount() {
  document.body.innerHTML = HTML;
  const { SidebarHeaderControls } = await import(
    '../../src/components/Sidebar.js'
  );
  const { WhatsNew } = await import('../../src/components/modals/WhatsNew.js');
  const root = document.getElementById('whats-new') as HTMLElement;
  // Mirrors components/App.tsx: the modal is portalled INTO its own root,
  // so querying #whats-new is querying what the app actually renders.
  return render(
    <>
      <SidebarHeaderControls />
      {createPortal(<WhatsNew root={root} />, root)}
    </>,
  );
}

const giftBtn = () =>
  document.getElementById('whats-new-btn') as HTMLButtonElement;
const dialog = () => document.getElementById('whats-new') as HTMLElement;

beforeEach(() => {
  localStorage.clear();
  resetStore();
});
afterEach(cleanup);

describe('the sidebar gift', () => {
  it('renders third in the header, after new-project and check-updates', async () => {
    await mount();
    const ids = [...document.querySelectorAll('header button')].map(
      (b) => b.id,
    );
    expect(ids).toEqual([
      'new-project-btn',
      'check-updates-btn',
      'whats-new-btn',
    ]);
  });

  it('says "unread" in its accessible name, not only in the dot', async () => {
    // The dot is a CSS ::after and says nothing to a screen reader, so the
    // one bit it carries has to be in the name too.
    await mount();
    expect(giftBtn().getAttribute('aria-label')).toBe("What's new — unread");
    expect(giftBtn().getAttribute('title')).toBe("What's new — unread");
    expect(giftBtn().dataset.size).toBe('22');
  });

  it('drops "unread" from the name once read', async () => {
    seedSeen(latestVersion() ?? '');
    await mount();
    expect(giftBtn().getAttribute('aria-label')).toBe("What's new");
  });

  it('shows the unread dot when nothing has been read', async () => {
    await mount();
    expect(giftBtn().classList.contains('hv-unread')).toBe(true);
  });

  it('shows no dot once the latest version has been read', async () => {
    seedSeen(latestVersion() ?? '');
    await mount();
    expect(giftBtn().classList.contains('hv-unread')).toBe(false);
  });

  it('clears the dot on click without a reload, and records the read', async () => {
    await mount();
    await act(async () => {
      giftBtn().click();
    });
    // Same render pass: a localStorage read at render would leave the dot up
    // until a reload.
    expect(giftBtn().classList.contains('hv-unread')).toBe(false);
    expect(localStorage.getItem(SEEN_KEY)).toBe(latestVersion());
  });

  it('clears the dot when opened from the command palette, not the gift', async () => {
    // The palette calls openWhatsNew() directly. Component-local state the
    // palette cannot reach would record the read and leave the dot up until
    // a reload — the whole reason the seen version lives in the store.
    await mount();
    expect(giftBtn().classList.contains('hv-unread')).toBe(true);
    const { openWhatsNew } = await import('../../src/app/modals/whats-new.js');
    await act(async () => {
      openWhatsNew();
    });
    expect(giftBtn().classList.contains('hv-unread')).toBe(false);
    expect(giftBtn().getAttribute('aria-label')).toBe("What's new");
    expect(localStorage.getItem(SEEN_KEY)).toBe(latestVersion());
  });

  it('renders nothing on a scaffold with no sidebar header', async () => {
    document.body.innerHTML = '<div id="app"></div>';
    const { SidebarHeaderControls } = await import(
      '../../src/components/Sidebar.js'
    );
    expect(() => render(<SidebarHeaderControls />)).not.toThrow();
    expect(document.getElementById('whats-new-btn')).toBeNull();
  });
});

describe("the What's New modal", () => {
  it('is hidden until the gift is clicked', async () => {
    await mount();
    expect(dialog().classList.contains('hidden')).toBe(true);
    await act(async () => {
      giftBtn().click();
    });
    expect(dialog().classList.contains('hidden')).toBe(false);
  });

  it('lists releases newest first, with each feature and its blurb', async () => {
    await mount();
    await act(async () => {
      giftBtn().click();
    });
    const headings = [
      ...dialog().querySelectorAll('.whats-new-release h4'),
    ].map((h) => h.textContent);
    // Unreleased leads (this very feature lives there until a release stamps
    // it), the newest stamped release follows, and "Coming next" is last.
    expect(headings[0]).toBe(UNRELEASED);
    expect(headings[1]).toBe(latestVersion());
    expect(headings.at(-1)).toBe('Coming next');

    const items = [...dialog().querySelectorAll('.whats-new-release li')];
    expect(items.length).toBeGreaterThan(0);
    const undoClose = items.find((li) =>
      li.textContent?.startsWith('Undo a close'),
    );
    expect(undoClose?.textContent).toContain('reopens a closed session');
  });

  it('closes on Escape', async () => {
    await mount();
    await act(async () => {
      giftBtn().click();
    });
    await act(async () => {
      dialog().dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      );
    });
    expect(dialog().classList.contains('hidden')).toBe(true);
  });
});
