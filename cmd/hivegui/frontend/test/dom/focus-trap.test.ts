// @vitest-environment jsdom
//
// The shared focus trap (src/lib/focus-trap.ts). Browser-level
// behaviour lives in test/e2e/focus-traps.spec.ts — jsdom has no real
// focus model, so these cover the decision logic: which elements
// count, and where focus is sent at the boundaries.
import { describe, it, expect, beforeEach } from 'vitest';
import { trapFocus, focusableWithin } from '../../src/lib/focus-trap.js';

function mount(html: string): HTMLElement {
  document.body.innerHTML = `<div id="box">${html}</div><button id="outside">out</button>`;
  return document.getElementById('box') as HTMLElement;
}

function tab(shift = false): KeyboardEvent {
  return new window.KeyboardEvent('keydown', {
    key: 'Tab',
    shiftKey: shift,
    bubbles: true,
    cancelable: true,
  });
}

const el = (id: string) => document.getElementById(id) as HTMLElement;

beforeEach(() => {
  document.body.innerHTML = '';
});

describe('focusableWithin', () => {
  it('finds buttons, inputs and explicit tabindex, in document order', () => {
    const box = mount(`
      <button id="b1">1</button>
      <input id="i1"/>
      <span id="s1" tabindex="0">span</span>
    `);
    expect(focusableWithin(box).map((e) => e.id)).toEqual(['b1', 'i1', 's1']);
  });

  it('skips disabled controls', () => {
    const box = mount(
      '<button id="b1">1</button><button id="b2" disabled>2</button>',
    );
    expect(focusableWithin(box).map((e) => e.id)).toEqual(['b1']);
  });

  it('skips tabindex="-1"', () => {
    const box = mount(
      '<button id="b1">1</button><span id="s" tabindex="-1">x</span>',
    );
    expect(focusableWithin(box).map((e) => e.id)).toEqual(['b1']);
  });

  // Visibility follows this app's convention rather than layout, so the
  // rule behaves identically in jsdom and in the browser.
  it('skips elements hidden by the .hidden class', () => {
    const box = mount(
      '<button id="b1">1</button><div class="hidden"><button id="b2">2</button></div>',
    );
    expect(focusableWithin(box).map((e) => e.id)).toEqual(['b1']);
  });

  it('skips elements carrying the hidden attribute', () => {
    const box = mount(
      '<button id="b1">1</button><button id="b2" hidden>2</button>',
    );
    expect(focusableWithin(box).map((e) => e.id)).toEqual(['b1']);
  });

  it('never reaches outside the container', () => {
    const box = mount('<button id="b1">1</button>');
    expect(focusableWithin(box).map((e) => e.id)).toEqual(['b1']);
  });
});

describe('trapFocus', () => {
  it('ignores keys other than Tab', () => {
    const box = mount('<button id="b1">1</button>');
    const ev = new window.KeyboardEvent('keydown', {
      key: 'a',
      cancelable: true,
    });
    expect(trapFocus(box, ev)).toBe(false);
    expect(ev.defaultPrevented).toBe(false);
  });

  // The case that shipped broken: a modal opens over a terminal, so
  // focus is outside it, and the first Tab has to pull focus in rather
  // than continue through the page.
  it('pulls focus in when it started outside the container', () => {
    const box = mount('<button id="b1">1</button><button id="b2">2</button>');
    el('outside').focus();
    const ev = tab();
    expect(trapFocus(box, ev)).toBe(true);
    expect(ev.defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(el('b1'));
  });

  it('enters at the last element when Shift is held', () => {
    const box = mount('<button id="b1">1</button><button id="b2">2</button>');
    el('outside').focus();
    trapFocus(box, tab(true));
    expect(document.activeElement).toBe(el('b2'));
  });

  it('wraps forward from the last element to the first', () => {
    const box = mount('<button id="b1">1</button><button id="b2">2</button>');
    el('b2').focus();
    const ev = tab();
    expect(trapFocus(box, ev)).toBe(true);
    expect(document.activeElement).toBe(el('b1'));
  });

  it('wraps backward from the first element to the last', () => {
    const box = mount('<button id="b1">1</button><button id="b2">2</button>');
    el('b1').focus();
    const ev = tab(true);
    expect(trapFocus(box, ev)).toBe(true);
    expect(document.activeElement).toBe(el('b2'));
  });

  // Interior moves belong to the browser: a form's fields must walk
  // naturally, so the trap only acts at the boundaries.
  it('leaves interior moves to the browser', () => {
    const box = mount(
      '<button id="b1">1</button><button id="b2">2</button><button id="b3">3</button>',
    );
    el('b2').focus();
    const ev = tab();
    expect(trapFocus(box, ev)).toBe(false);
    expect(ev.defaultPrevented).toBe(false);
    // Untouched — the browser would move it.
    expect(document.activeElement).toBe(el('b2'));
  });

  it('pins focus when the container has a single focusable', () => {
    const box = mount('<button id="only">x</button>');
    el('only').focus();
    expect(trapFocus(box, tab())).toBe(true);
    expect(document.activeElement).toBe(el('only'));
    expect(trapFocus(box, tab(true))).toBe(true);
    expect(document.activeElement).toBe(el('only'));
  });

  // An empty container still must not let Tab walk out behind it.
  it('swallows Tab when there is nothing focusable', () => {
    const box = mount('<p>nothing here</p>');
    const ev = tab();
    expect(trapFocus(box, ev)).toBe(true);
    expect(ev.defaultPrevented).toBe(true);
  });

  it('treats a disabled last element as not the boundary', () => {
    const box = mount(
      '<button id="b1">1</button><button id="b2">2</button><button id="b3" disabled>3</button>',
    );
    el('b2').focus();
    // b2 is the last ENABLED one, so this wraps.
    expect(trapFocus(box, tab())).toBe(true);
    expect(document.activeElement).toBe(el('b1'));
  });
});
