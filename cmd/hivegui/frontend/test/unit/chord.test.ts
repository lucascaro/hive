import { describe, expect, it } from 'vitest';
import {
  chordMatches,
  chordsFor,
  parseChord,
  type KeyEventLike,
} from '../../src/lib/chord.js';

function ev(o: Partial<KeyEventLike> = {}): KeyEventLike {
  return {
    key: 'k',
    code: 'KeyK',
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    ...o,
  };
}

const hits = (chord: string, isMac: boolean, e: Partial<KeyEventLike>) =>
  chordMatches(parseChord(chord, isMac), ev(e));

describe('parseChord', () => {
  it('Mod is ⌘ on macOS and Ctrl elsewhere', () => {
    expect(hits('Mod+K', true, { metaKey: true })).toBe(true);
    expect(hits('Mod+K', true, { ctrlKey: true })).toBe(false);
    expect(hits('Mod+K', false, { ctrlKey: true })).toBe(true);
    expect(hits('Mod+K', false, { metaKey: true })).toBe(false);
  });

  it('rejects ⌘ and Ctrl together, like cmdOrCtrl', () => {
    expect(hits('Mod+K', true, { metaKey: true, ctrlKey: true })).toBe(false);
    expect(hits('Mod+K', false, { metaKey: true, ctrlKey: true })).toBe(false);
  });

  it('is exact: an unnamed modifier must be off', () => {
    expect(hits('Mod+K', true, { metaKey: true, altKey: true })).toBe(false);
    expect(hits('Mod+K', true, { metaKey: true, shiftKey: true })).toBe(false);
    expect(hits('Mod+Shift+K', true, { metaKey: true })).toBe(false);
  });

  it('`?` makes one modifier "don\'t care"', () => {
    expect(hits('Mod+Shift?+K', true, { metaKey: true })).toBe(true);
    expect(hits('Mod+Shift?+K', true, { metaKey: true, shiftKey: true })).toBe(
      true,
    );
    expect(hits('Mod+Shift?+K', true, { metaKey: true, altKey: true })).toBe(
      false,
    );
  });

  it('`Any+` makes every unnamed modifier "don\'t care"', () => {
    const esc = { key: 'Escape', code: 'Escape' };
    expect(hits('Any+Escape', true, esc)).toBe(true);
    expect(hits('Any+Escape', true, { ...esc, shiftKey: true })).toBe(true);
    expect(
      hits('Any+Escape', false, { ...esc, metaKey: true, ctrlKey: true }),
    ).toBe(true);
    expect(hits('Escape', true, { ...esc, shiftKey: true })).toBe(false);
  });

  it('matches e.key case-insensitively, so caps lock changes nothing', () => {
    expect(hits('Mod+K', true, { metaKey: true, key: 'K' })).toBe(true);
    expect(hits('Shift?+R', true, { key: 'R' })).toBe(true);
  });

  it('[Code] matches the physical key whatever e.key says', () => {
    expect(
      hits('Ctrl+[Minus]', true, { ctrlKey: true, key: 'Dead', code: 'Minus' }),
    ).toBe(true);
    expect(
      hits('Ctrl+[Minus]', true, { ctrlKey: true, key: '-', code: 'Equal' }),
    ).toBe(false);
  });

  it('binds "+" itself, and named keys', () => {
    expect(
      hits('Mod+Shift?++', true, { metaKey: true, shiftKey: true, key: '+' }),
    ).toBe(true);
    expect(
      hits('Mod+Shift+ArrowUp', false, {
        ctrlKey: true,
        shiftKey: true,
        key: 'ArrowUp',
      }),
    ).toBe(true);
  });

  it('throws on a malformed chord rather than binding nothing', () => {
    expect(() => parseChord('Cmd+K', true)).toThrow(/bad token/);
    expect(() => parseChord('Mod+', true)).toThrow(/no key/);
  });
});

describe('chordsFor', () => {
  it('reads a string, a list, or the platform half of a pair', () => {
    expect(chordsFor('Mod+K', true)).toEqual(['Mod+K']);
    expect(chordsFor(['A', 'B'], false)).toEqual(['A', 'B']);
    const pair = { mac: 'Ctrl+-', other: ['Ctrl+Alt+-'] };
    expect(chordsFor(pair, true)).toEqual(['Ctrl+-']);
    expect(chordsFor(pair, false)).toEqual(['Ctrl+Alt+-']);
    expect(chordsFor({ other: 'Ctrl+Shift+F' }, true)).toEqual([]);
  });
});
