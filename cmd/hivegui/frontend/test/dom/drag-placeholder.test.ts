import { describe, it, expect, beforeEach, vi } from 'vitest';
import { beginDrag, moveTo, endDrag } from '../../src/lib/drag-placeholder';

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
      .map((id) => `<li id="${id}" class="hv-session-row"></li>`)
      .join('') +
    '</ul>';
});

const row = (id: string) => document.getElementById(id) as HTMLElement;

describe('drag placeholder', () => {
  it('inserts the spacer above or below the target', () => {
    beginDrag(row('a'));
    moveTo(row('c'), true);
    expect(ids()).toEqual(['a', 'b', '·', 'c']);

    moveTo(row('c'), false);
    expect(ids()).toEqual(['a', 'b', 'c', '·']);
  });

  it('moves the one spacer instead of creating a second', () => {
    beginDrag(row('a'));
    moveTo(row('b'), true);
    moveTo(row('c'), true);
    expect(document.querySelectorAll('.hv-drop-placeholder')).toHaveLength(1);
    expect(ids()).toEqual(['a', 'b', '·', 'c']);
  });

  // sidebar.ts's domShape() reads the sidebar back with this selector pair to
  // decide whether an in-place update is safe. A spacer wearing either class
  // would read as a phantom row and desync the shape comparison.
  it('wears neither row class', () => {
    beginDrag(row('a'));
    moveTo(row('b'), true);
    const ph = document.querySelector('.hv-drop-placeholder') as HTMLElement;
    expect(ph.classList.contains('hv-session-row')).toBe(false);
    expect(ph.classList.contains('hv-project-card')).toBe(false);
    expect(ph.getAttribute('aria-hidden')).toBe('true');
  });

  it('takes the dragged element out of the flow, but not before the drag image is snapshotted', () => {
    vi.useFakeTimers();
    try {
      beginDrag(row('a'));
      // Hiding the source inside the dragstart handler cancels the drag.
      expect(row('a').classList.contains('dragging')).toBe(false);
      vi.runAllTimers();
      expect(row('a').classList.contains('dragging')).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it('restores the element and removes the spacer on endDrag', () => {
    beginDrag(row('a'));
    moveTo(row('c'), true);
    endDrag();
    expect(ids()).toEqual(['a', 'b', 'c']);
    expect(row('a').classList.contains('dragging')).toBe(false);
  });

  it('is idempotent — drop and dragend both fire', () => {
    beginDrag(row('a'));
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
    beginDrag(row('a'));
    row('a').classList.remove('dragging');
    moveTo(row('b'), true);
    expect(row('a').classList.contains('dragging')).toBe(true);
  });
});
