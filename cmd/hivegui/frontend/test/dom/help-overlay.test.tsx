// @vitest-environment jsdom
//
// Covers the keyboard-shortcuts help overlay (⌘/)
// (src/components/modals/HelpOverlay.tsx, opened/closed/toggled through
// src/app/modals/help-overlay.ts): the groups render from
// shortcutGroups() rather than a copy baked into the component (the two
// must never drift, since the palette's shortcut column reads the same
// source), and toggleHelpOverlay is the ONE entry point the macOS menu
// item drives both open and close through — a regression that makes
// toggle open-only would leave the native ⌘/ accelerator unable to
// close the overlay it opened.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { act, render } from '@testing-library/react';
import { resetStore } from '../../src/store/store.js';
import { isMac } from '../../src/lib/platform.js';
import { shortcutGroups } from '../../src/lib/shortcuts.js';

// #app wraps the root for the same reason every other modal fixture
// does: RTL's cleanup() removes a render() container whose parentNode
// IS document.body, and #help-overlay is nested one level down in
// index.html.
const MARKUP = `
  <div id="app"><div id="help-overlay" class="hv-dialog hidden" role="dialog"
    aria-modal="true" aria-labelledby="help-overlay-title"></div></div>`;

type HelpOverlayModule = typeof import('../../src/app/modals/help-overlay.js');
let openHelpOverlay: HelpOverlayModule['openHelpOverlay'];
let closeHelpOverlay: HelpOverlayModule['closeHelpOverlay'];
let toggleHelpOverlay: HelpOverlayModule['toggleHelpOverlay'];
let initHelpOverlay: HelpOverlayModule['initHelpOverlay'];
let HelpOverlay: typeof import('../../src/components/modals/HelpOverlay.js')['HelpOverlay'];

const setFocusedTile = vi.fn();
const focusActiveTerm = vi.fn();

function el(id: string): HTMLElement {
  return document.getElementById(id) as HTMLElement;
}
function root() {
  return el('help-overlay');
}
const open = () => act(() => openHelpOverlay());
const close = () => act(() => closeHelpOverlay());
const toggle = () => act(() => toggleHelpOverlay());

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  ({ HelpOverlay } = await import(
    '../../src/components/modals/HelpOverlay.js'
  ));
  ({ openHelpOverlay, closeHelpOverlay, toggleHelpOverlay, initHelpOverlay } =
    await import('../../src/app/modals/help-overlay.js'));
  initHelpOverlay({ setFocusedTile, focusActiveTerm });
});

beforeEach(() => {
  setFocusedTile.mockClear();
  focusActiveTerm.mockClear();
  resetStore();
  render(<HelpOverlay root={root()} />, { container: root() });
});

describe('rendering', () => {
  it('is hidden until opened', () => {
    expect(root().classList.contains('hidden')).toBe(true);
    expect(root().querySelector('#help-overlay-groups')).toBeNull();
  });

  it('renders one section per shortcut group, matching shortcutGroups(isMac) exactly', () => {
    open();
    const groups = shortcutGroups({ isMac });
    const groupsEl = el('help-overlay-groups');
    expect(groupsEl).toBeTruthy();
    const sections = [...groupsEl.querySelectorAll('section')];
    expect(sections).toHaveLength(groups.length);

    sections.forEach((sec, i) => {
      const group = groups[i];
      expect(sec.querySelector('h4')?.textContent).toBe(group.title);
      const dts = [...sec.querySelectorAll('dl > dt')];
      const dds = [...sec.querySelectorAll('dl > dd')];
      expect(dts).toHaveLength(group.items.length);
      expect(dds).toHaveLength(group.items.length);
      group.items.forEach((item, j) => {
        const kbd = dts[j].querySelector('kbd.hv-kbd');
        expect(kbd?.textContent).toBe(item.keys);
        expect(dds[j].textContent).toBe(item.label);
      });
    });
  });

  it('focuses the close button on open', () => {
    open();
    expect(document.activeElement).toBe(el('help-overlay-close'));
  });
});

describe('toggleHelpOverlay', () => {
  it('opens when closed', () => {
    expect(root().classList.contains('hidden')).toBe(true);
    toggle();
    expect(root().classList.contains('hidden')).toBe(false);
  });

  it('closes when open — the macOS menu item drives both through this one entry point', () => {
    open();
    expect(root().classList.contains('hidden')).toBe(false);
    toggle();
    expect(root().classList.contains('hidden')).toBe(true);
  });
});

describe('closing', () => {
  it('hides and empties the root', () => {
    open();
    expect(el('help-overlay-groups')).toBeTruthy();
    close();
    expect(root().classList.contains('hidden')).toBe(true);
    expect(root().querySelector('#help-overlay-groups')).toBeNull();
    expect(focusActiveTerm).toHaveBeenCalled();
  });
});
