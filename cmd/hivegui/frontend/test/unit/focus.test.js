import { describe, it, expect } from 'vitest';
import {
  decideFocusAction,
  ACTION_CLEAR,
  ACTION_PRESERVE,
  ACTION_FOCUS,
} from '../../src/lib/focus.js';

const knownTermIds = new Set(['s1', 's2']);

function snap(overrides = {}) {
  return {
    id: 's1',
    modalOpen: false,
    activeTag: 'BODY',
    activeClasses: '',
    knownTermIds,
    ...overrides,
  };
}

describe('decideFocusAction', () => {
  it('clears when id is null', () => {
    expect(decideFocusAction(snap({ id: null })).kind).toBe(ACTION_CLEAR);
  });

  it('clears when a modal is open', () => {
    expect(decideFocusAction(snap({ modalOpen: true })).kind).toBe(ACTION_CLEAR);
  });

  it('clears when id is not a known SessionTerm', () => {
    expect(decideFocusAction(snap({ id: 'ghost' })).kind).toBe(ACTION_CLEAR);
  });

  it('preserves when a real INPUT owns the keyboard', () => {
    const a = decideFocusAction(snap({ activeTag: 'INPUT', activeClasses: 'tile-name-input' }));
    expect(a.kind).toBe(ACTION_PRESERVE);
  });

  it('preserves when a real TEXTAREA (non-xterm) owns the keyboard', () => {
    const a = decideFocusAction(snap({ activeTag: 'TEXTAREA', activeClasses: 'something-else' }));
    expect(a.kind).toBe(ACTION_PRESERVE);
  });

  it('focuses through when the xterm helper-textarea is already active', () => {
    const a = decideFocusAction(snap({ activeTag: 'TEXTAREA', activeClasses: 'xterm-helper-textarea' }));
    expect(a).toEqual({ kind: ACTION_FOCUS, id: 's1' });
  });

  it('focuses when activeElement is BODY (the post-blur single → grid state)', () => {
    expect(decideFocusAction(snap({ activeTag: 'BODY' }))).toEqual({ kind: ACTION_FOCUS, id: 's1' });
  });

  it('accepts a DOMTokenList-shaped activeClasses', () => {
    const tokens = { contains: (n) => n === 'xterm-helper-textarea' };
    const a = decideFocusAction(snap({ activeTag: 'TEXTAREA', activeClasses: tokens }));
    expect(a.kind).toBe(ACTION_FOCUS);
  });

  it('accepts an array-shaped knownTermIds', () => {
    expect(decideFocusAction(snap({ knownTermIds: ['s1'] })).kind).toBe(ACTION_FOCUS);
    expect(decideFocusAction(snap({ id: 'sZ', knownTermIds: ['s1'] })).kind).toBe(ACTION_CLEAR);
  });

  it('modal open beats every other signal', () => {
    const a = decideFocusAction(snap({
      modalOpen: true,
      activeTag: 'TEXTAREA',
      activeClasses: 'xterm-helper-textarea',
    }));
    expect(a.kind).toBe(ACTION_CLEAR);
  });

  it('clears when the command palette is the open modal', () => {
    // focusSnapshot() in main.js ORs launcher/editor/palette visibility
    // into modalOpen; decideFocusAction only sees the resulting flag.
    // This locks in that any palette-open snapshot yields ACTION_CLEAR
    // even when a tile would otherwise be a valid focus target.
    const a = decideFocusAction(snap({
      modalOpen: true,
      activeTag: 'INPUT',
      activeClasses: 'command-palette-input',
    }));
    expect(a.kind).toBe(ACTION_CLEAR);
  });
});
