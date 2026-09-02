import { describe, it, expect, beforeEach, vi } from 'vitest';
import { beginDrag, moveTo, endDrag } from '../../src/lib/drag-placeholder';

const noop = () => {};
const dropEvent = () =>
  new Event('drop', { bubbles: true, cancelable: true }) as DragEvent;

// jsdom has no layout, so getBoundingClientRect() reports 0 — that is fine
// here. These tests own the DOM contract (where the spacer lands, that it is
// unique, that it cleans up); the *visual* contract — that the list's height
// really does not change — is only meaningful in a real browser and lives in
// test/e2e/ordering.spec.ts.

const list = () => document.querySelector('ul') as HTMLUListElement;
const ids = () =>
  Array.from(list().children).map((el) =>
    el.classList.contains('hv-drop-placeholder') ? '·' : el.id,
  );

beforeEach(() => {
  endDrag();
  document.body.innerHTML =
    '<ul>' +
    ['a', 'b', 'c']
      .map(
        (id) => `<li id="${id}" data-sid="${id}" class="hv-session-row"></li>`,
      )
      .join('') +
    '</ul>';
});

const row = (id: string) => document.getElementById(id) as HTMLElement;

describe('drag placeholder', () => {
  it('reserves the box in the same tick it hides the row', () => {
    vi.useFakeTimers();
    try {
      beginDrag(row('a'), noop);
      // Nothing has changed yet: hiding the source inside the dragstart
      // handler cancels the drag, so the swap is deferred a tick.
      expect(row('a').classList.contains('dragging')).toBe(false);
      expect(document.querySelector('.hv-drop-placeholder')).toBeNull();
      vi.runAllTimers();
      // Spacer in and row out together — the list never spends a frame short.
      expect(row('a').classList.contains('dragging')).toBe(true);
      expect(ids()).toEqual(['·', 'a', 'b', 'c']);
    } finally {
      vi.useRealTimers();
    }
  });

  it('inserts the spacer above or below the target', () => {
    beginDrag(row('a'), noop);
    moveTo(row('c'), true);
    expect(ids()).toEqual(['a', 'b', '·', 'c']);

    moveTo(row('c'), false);
    expect(ids()).toEqual(['a', 'b', 'c', '·']);
  });

  it('moves the one spacer instead of creating a second', () => {
    beginDrag(row('a'), noop);
    moveTo(row('b'), true);
    moveTo(row('c'), true);
    expect(document.querySelectorAll('.hv-drop-placeholder')).toHaveLength(1);
    expect(ids()).toEqual(['a', 'b', '·', 'c']);
  });

  // sidebar.ts's domShape() reads the sidebar back with this selector pair to
  // decide whether an in-place update is safe. A spacer wearing either class
  // would read as a phantom row and desync the shape comparison.
  it('wears neither row class', () => {
    beginDrag(row('a'), noop);
    moveTo(row('b'), true);
    const ph = document.querySelector('.hv-drop-placeholder') as HTMLElement;
    expect(ph.classList.contains('hv-session-row')).toBe(false);
    expect(ph.classList.contains('hv-project-card')).toBe(false);
    expect(ph.getAttribute('aria-hidden')).toBe('true');
  });

  it('restores the element and removes the spacer on endDrag', () => {
    beginDrag(row('a'), noop);
    moveTo(row('c'), true);
    endDrag();
    expect(ids()).toEqual(['a', 'b', 'c']);
    expect(row('a').classList.contains('dragging')).toBe(false);
  });

  it('is idempotent — drop and dragend both fire', () => {
    beginDrag(row('a'), noop);
    moveTo(row('b'), true);
    endDrag();
    expect(() => endDrag()).not.toThrow();
    expect(ids()).toEqual(['a', 'b', 'c']);
  });

  it('ignores moveTo outside a drag', () => {
    moveTo(row('b'), true);
    expect(document.querySelector('.hv-drop-placeholder')).toBeNull();
  });

  // renderSidebar() clears projectsUL.innerHTML wholesale; a poll landing
  // mid-drag would otherwise leave the drag chrome lost for the rest of the
  // gesture.
  it('re-asserts the hidden state on every move', () => {
    beginDrag(row('a'), noop);
    row('a').classList.remove('dragging');
    moveTo(row('b'), true);
    expect(row('a').classList.contains('dragging')).toBe(true);
  });

  // The regression this file gained on review: an "insert above" spacer is
  // placed where the cursor already is, so the release lands on the SPACER,
  // not on any row. A spacer that is not itself a drop target loses the drop.
  it('accepts the drop itself and resolves the slot it sits in', () => {
    const drops: [string, boolean][] = [];
    beginDrag(row('a'), (t, above) => drops.push([t.id, above]));
    moveTo(row('c'), true);
    const ph = document.querySelector('.hv-drop-placeholder') as HTMLElement;

    const over = new Event('dragover', { bubbles: true, cancelable: true });
    ph.dispatchEvent(over);
    expect(over.defaultPrevented).toBe(true);

    ph.dispatchEvent(dropEvent());
    expect(drops).toEqual([['c', true]]);
    expect(document.querySelector('.hv-drop-placeholder')).toBeNull();
  });

  it('resolves a trailing slot against the row it follows', () => {
    const drops: [string, boolean][] = [];
    beginDrag(row('a'), (t, above) => drops.push([t.id, above]));
    moveTo(row('c'), false);
    const ph = document.querySelector('.hv-drop-placeholder') as HTMLElement;
    ph.dispatchEvent(dropEvent());
    expect(drops).toEqual([['c', false]]);
  });

  // renderSidebar() clears projectsUL.innerHTML wholesale. Holding the
  // original node would leave us re-hiding a detached element for the rest of
  // the gesture, so the module re-reads the row by its data-sid.
  it('recovers the dragged row after a mid-drag rebuild', () => {
    beginDrag(row('a'), noop);
    moveTo(row('b'), true);
    const html = list().innerHTML;
    list().innerHTML = html.replace(
      /<li class="hv-drop-placeholder"[^>]*><\/li>/,
      '',
    );
    moveTo(row('c'), true);
    expect(row('a').classList.contains('dragging')).toBe(true);
    expect(document.querySelectorAll('.hv-drop-placeholder')).toHaveLength(1);
  });
});
