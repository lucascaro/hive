// @vitest-environment jsdom
//
// Covers the command palette (src/components/modals/CommandPalette.tsx,
// opened through the openCommandPalette/closeCommandPalette pair in
// src/app/modals/command-palette.ts): the filter, the keyboard
// navigation and the one behavior that is easy to silently drop in a
// port — activation is DEFERRED past the close (`setTimeout(…, 0)`) so
// a command that opens another modal doesn't have that modal's focus
// stolen back by the palette's own close-time refocus. There is no
// registerModal() here anymore (registry.ts is gone with this phase),
// so this file is also what pins the outside-mousedown-closes and
// reopen-resets-state contracts down.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';
import { resetStore } from '../../src/store/store.js';

// #app wraps the root for the same reason settings.test.tsx wraps its
// dialog root: RTL's cleanup() removes a render() container whose
// parentNode IS document.body, and #command-palette is nested one level
// down in index.html.
const MARKUP = `
  <div id="app"><div id="command-palette" class="hidden" role="dialog"
    aria-label="Command palette"></div></div>`;

type CommandPaletteModule =
  typeof import('../../src/app/modals/command-palette.js');
let openCommandPalette: CommandPaletteModule['openCommandPalette'];
let closeCommandPalette: CommandPaletteModule['closeCommandPalette'];
let initCommandPalette: CommandPaletteModule['initCommandPalette'];
// Imported after the markup exists: the component takes `root` as a
// prop rather than resolving it itself, but command-palette.ts still
// needs the element for closeCommandPalette's blur.
let CommandPalette: typeof import('../../src/components/modals/CommandPalette.js')['CommandPalette'];

const focusActiveTerm = vi.fn();

// Three rows: one with a shortcut, one without (the empty-span case),
// and one whose shortcut is the ONLY thing a query can match on — the
// case that would silently pass if filtering only ever looked at the
// name.
const runAlpha = vi.fn();
const runBeta = vi.fn();
const runZoom = vi.fn();
const COMMANDS = [
  { id: 'alpha', name: 'Alpha One', shortcut: '⌘A', run: runAlpha },
  { id: 'beta', name: 'Beta Two', shortcut: '', run: runBeta },
  { id: 'zoom', name: 'Zoom Reset', shortcut: '⌘0', run: runZoom },
];

function el(id: string): HTMLElement {
  return document.getElementById(id) as HTMLElement;
}
function palette() {
  return el('command-palette');
}
function searchBox() {
  return el('command-palette-input') as HTMLInputElement;
}
function rows() {
  return [...el('command-palette-list').querySelectorAll('.palette-item')];
}
function names() {
  return rows().map((r) => r.querySelector('.palette-name')?.textContent);
}
function selected() {
  return palette().querySelector('.palette-item[data-selected] .palette-name')
    ?.textContent;
}
function type(value: string) {
  fireEvent.change(searchBox(), { target: { value } });
}
// The palette owns Escape/arrows/Tab/Enter via a plain listener on its
// root, so events must bubble through the root exactly as a real
// keystroke landing in the focused search box would.
function press(key: string, init: KeyboardEventInit = {}) {
  act(() => {
    palette().dispatchEvent(
      new KeyboardEvent('keydown', {
        key,
        bubbles: true,
        cancelable: true,
        ...init,
      }),
    );
  });
}
const open = () => act(() => openCommandPalette());
const close = () => act(() => closeCommandPalette());

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  ({ CommandPalette } = await import(
    '../../src/components/modals/CommandPalette.js'
  ));
  ({ openCommandPalette, closeCommandPalette, initCommandPalette } =
    await import('../../src/app/modals/command-palette.js'));
  initCommandPalette({ commands: COMMANDS, focusActiveTerm });
});

beforeEach(() => {
  runAlpha.mockClear();
  runBeta.mockClear();
  runZoom.mockClear();
  focusActiveTerm.mockClear();
  resetStore();
  render(<CommandPalette root={palette()} />, { container: palette() });
});

describe('open / close', () => {
  it('is hidden until opened, then shows the input and list and focuses the input', () => {
    expect(palette().classList.contains('hidden')).toBe(true);
    open();
    expect(palette().classList.contains('hidden')).toBe(false);
    expect(searchBox()).toBeTruthy();
    expect(el('command-palette-list')).toBeTruthy();
    expect(document.activeElement).toBe(searchBox());
  });

  it('lists every command with an empty query', () => {
    open();
    expect(names()).toEqual(['Alpha One', 'Beta Two', 'Zoom Reset']);
  });

  it('renders the shortcut through kbd.hv-kbd, and leaves the span empty when there is none', () => {
    open();
    const [alphaRow, betaRow] = rows();
    const alphaKbd = alphaRow.querySelector('.palette-shortcut kbd.hv-kbd');
    expect(alphaKbd?.textContent).toBe('⌘A');
    const betaShortcut = betaRow.querySelector('.palette-shortcut');
    expect(betaShortcut?.querySelector('kbd')).toBeNull();
    expect(betaShortcut?.textContent).toBe('');
  });
});

describe('filtering', () => {
  it('narrows by a case-insensitive substring of the name', () => {
    open();
    type('ALPHA');
    expect(names()).toEqual(['Alpha One']);
  });

  it('also matches a case-insensitive substring of the shortcut', () => {
    open();
    // '0' appears in no command's name, only in Zoom Reset's shortcut.
    type('0');
    expect(names()).toEqual(['Zoom Reset']);
  });

  it('lists everything again once the query is cleared', () => {
    open();
    type('alpha');
    type('');
    expect(names()).toEqual(['Alpha One', 'Beta Two', 'Zoom Reset']);
  });
});

describe('selection', () => {
  it('marks the selected row with data-selected and wraps with ArrowDown/ArrowUp', () => {
    open();
    expect(selected()).toBe('Alpha One');
    press('ArrowDown');
    expect(selected()).toBe('Beta Two');
    press('ArrowDown');
    expect(selected()).toBe('Zoom Reset');
    press('ArrowDown');
    expect(selected()).toBe('Alpha One');
    press('ArrowUp');
    expect(selected()).toBe('Zoom Reset');
  });

  it('Tab and Shift+Tab move the selection the same way as the arrows', () => {
    open();
    press('Tab');
    expect(selected()).toBe('Beta Two');
    press('Tab', { shiftKey: true });
    expect(selected()).toBe('Alpha One');
  });

  it('is a no-op on an empty result list', () => {
    open();
    type('nope-nothing-matches');
    expect(rows()).toHaveLength(0);
    expect(() => {
      press('ArrowDown');
      press('ArrowUp');
      press('Tab');
    }).not.toThrow();
    expect(rows()).toHaveLength(0);
  });

  it('mouseenter moves the selection', () => {
    open();
    fireEvent.mouseEnter(rows()[2]);
    expect(selected()).toBe('Zoom Reset');
  });

  it('a click activates that row', () => {
    open();
    fireEvent.mouseEnter(rows()[1]);
    fireEvent.click(rows()[1]);
    expect(palette().classList.contains('hidden')).toBe(true);
  });
});

describe('activation ordering', () => {
  // The whole reason activation is deferred: a command that opens
  // another modal must not have its focus stolen back by the palette's
  // own close, so the palette has to be visibly closed BEFORE run()
  // executes.
  it('closes the palette before running the selected command', async () => {
    open();
    let hiddenWhenRun: boolean | null = null;
    runAlpha.mockImplementationOnce(() => {
      hiddenWhenRun = palette().classList.contains('hidden');
    });
    press('Enter');
    // Still pending immediately after the synchronous close.
    expect(runAlpha).not.toHaveBeenCalled();
    expect(palette().classList.contains('hidden')).toBe(true);
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    expect(runAlpha).toHaveBeenCalledTimes(1);
    expect(hiddenWhenRun).toBe(true);
  });
});

describe('outside interaction', () => {
  it('closes on a mousedown outside the palette', () => {
    open();
    act(() => {
      document.body.dispatchEvent(
        new MouseEvent('mousedown', { bubbles: true }),
      );
    });
    expect(palette().classList.contains('hidden')).toBe(true);
  });

  it('does not close on a mousedown inside the palette', () => {
    open();
    act(() => {
      searchBox().dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });
    expect(palette().classList.contains('hidden')).toBe(false);
  });
});

describe('reopening', () => {
  it('resets the query and the selection', () => {
    open();
    type('zoom');
    press('ArrowDown');
    close();
    open();
    expect(searchBox().value).toBe('');
    expect(names()).toEqual(['Alpha One', 'Beta Two', 'Zoom Reset']);
    expect(selected()).toBe('Alpha One');
  });
});

describe('closing', () => {
  it('blurs the search box before handing focus back to the terminal', () => {
    open();
    expect(document.activeElement).toBe(searchBox());
    close();
    // lib/focus.ts's focusActiveTerm() bails whenever activeElement is
    // still an INPUT, so a close that forgot to blur first would
    // silently strand the keyboard on the (now hidden) search box.
    expect(document.activeElement).not.toBe(searchBox());
    expect(focusActiveTerm).toHaveBeenCalled();
  });
});
