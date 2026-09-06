// The What's New modal's empty-list branches.
//
// Its own file because these need the lib mocked: WhatsNewBody calls
// groupByVersion() / plannedOf() with no arguments, so with the real module
// loaded it can only ever render the live site/features.json. vi.mock is
// hoisted per file, so mocking it in the main suite would take every other
// test's data with it.
import { act, cleanup, render } from '@testing-library/react';
import { createPortal } from 'react-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../src/lib/whats-new.js', async (importOriginal) => {
  const real =
    await importOriginal<typeof import('../../src/lib/whats-new.js')>();
  return {
    ...real,
    // Shipped entries but nothing planned: the case that would render a
    // "Coming next" heading over an empty list if the guard were dropped.
    groupByVersion: () => [
      { version: '2.6.0', entries: [{ title: 'a thing', status: 'shipped' }] },
    ],
    plannedOf: () => [],
  };
});

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

async function openModal() {
  document.body.innerHTML = HTML;
  const { resetStore } = await import('../../src/store/store.js');
  resetStore();
  const { SidebarHeaderControls } = await import(
    '../../src/components/Sidebar.js'
  );
  const { WhatsNew } = await import('../../src/components/modals/WhatsNew.js');
  const root = document.getElementById('whats-new') as HTMLElement;
  render(
    <>
      <SidebarHeaderControls />
      {createPortal(<WhatsNew root={root} />, root)}
    </>,
  );
  await act(async () => {
    (document.getElementById('whats-new-btn') as HTMLButtonElement).click();
  });
  return root;
}

beforeEach(() => {
  localStorage.clear();
});
afterEach(cleanup);

describe("the What's New modal with nothing planned", () => {
  it('renders no "Coming next" section at all', async () => {
    const root = await openModal();
    // Not "an empty one" — no heading, no section. A stray heading over
    // nothing is what the `planned.length > 0` guard exists to prevent.
    expect(root.querySelectorAll('.whats-new-planned')).toHaveLength(0);
    const headings = [...root.querySelectorAll('.whats-new-release h4')].map(
      (h) => h.textContent,
    );
    expect(headings).toEqual(['2.6.0']);
  });

  it('still renders the releases above it', async () => {
    const root = await openModal();
    expect(
      root.querySelector('.whats-new-release li strong')?.textContent,
    ).toBe('a thing');
  });
});
