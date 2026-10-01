// Spec 477: comparing chords (collisions) and turning them into native
// menu accelerators.
import { describe, expect, it } from 'vitest';
import { chordsOverlap, toMenuAccelerator } from '../../src/lib/chord.js';
import { chordLabel, withKey } from '../../src/lib/chord-label.js';

describe('chordsOverlap', () => {
  const both = (a: string, b: string, isMac: boolean) =>
    chordsOverlap(a, b, isMac) && chordsOverlap(b, a, isMac);
  it("a don't-care modifier overlaps either state", () => {
    expect(both('Mod+Shift?+1', 'Mod+Shift+1', true)).toBe(true);
    expect(both('Mod+Shift?+1', 'Mod+1', true)).toBe(true);
    expect(both('Any+Escape', 'Mod+Escape', false)).toBe(true);
  });
  it('compares a [Code] key as the key it types', () => {
    expect(both('Mod+J', 'Mod+[KeyJ]', true)).toBe(true);
    expect(both('Ctrl+-', 'Ctrl+[Minus]', true)).toBe(true);
    expect(both('Mod+3', 'Mod+[Digit3]', false)).toBe(true);
  });
  it('a different modifier set does not overlap', () => {
    expect(both('Mod+J', 'Mod+Shift+J', true)).toBe(false);
    expect(both('Mod+J', 'Ctrl+J', true)).toBe(false);
    expect(both('Mod+T', 'Mod+Y', true)).toBe(false);
  });
  it('Mod is Ctrl off macOS', () => {
    expect(both('Mod+T', 'Ctrl+T', false)).toBe(true);
    expect(both('Mod+T', 'Ctrl+T', true)).toBe(false);
  });
  it('a shifted character overlaps its unshifted key (AppKit matches that)', () => {
    expect(both('Mod+?', 'Mod+/', true)).toBe(true);
    expect(both('Ctrl+_', 'Ctrl+-', true)).toBe(true);
  });
});

describe('toMenuAccelerator', () => {
  it.each([
    ['Mod+T', 'cmdorctrl+t'],
    ['Mod+Shift+T', 'cmdorctrl+shift+t'],
    ['Mod+Alt+T', 'cmdorctrl+optionoralt+t'],
    ['Mod+Ctrl+T', 'cmdorctrl+ctrl+t'],
    ['Mod+Shift?+=', 'cmdorctrl+='],
    ['Mod+Shift?++', 'cmdorctrl+plus'],
    ['Mod+ArrowUp', 'cmdorctrl+up'],
    ['Mod+Shift+ArrowDown', 'cmdorctrl+shift+down'],
    ['Mod+ArrowLeft', 'cmdorctrl+left'],
    ['Mod+ArrowRight', 'cmdorctrl+right'],
    ['Mod+Shift+Backspace', 'cmdorctrl+shift+backspace'],
    ['Mod+Enter', 'cmdorctrl+return'],
    ['Mod+[KeyJ]', 'cmdorctrl+j'],
    ['Mod+[Digit1]', 'cmdorctrl+1'],
    ['Mod+[Minus]', 'cmdorctrl+-'],
    ['Mod+[Backquote]', 'cmdorctrl+`'],
    ['Mod+[BracketLeft]', 'cmdorctrl+['],
    ['Mod+[Comma]', 'cmdorctrl+,'],
    ['Mod+[Slash]', 'cmdorctrl+/'],
  ])('%s → %s', (chord, want) => {
    expect(toMenuAccelerator(chord)).toBe(want);
  });
  it('only ⌘ chords go in the menu', () => {
    expect(toMenuAccelerator('Ctrl+T')).toBe('');
    expect(toMenuAccelerator('Ctrl+[Backquote]')).toBe('');
    expect(toMenuAccelerator('Alt+T')).toBe('');
  });
});

describe('chordLabel', () => {
  it.each([
    ['Mod+Shift+K', true, '⇧⌘K'],
    ['Mod+Shift+K', false, 'Ctrl+Shift+K'],
    ['Ctrl+Alt+Shift+-', false, 'Ctrl+Alt+Shift+-'],
    ['Ctrl+[Backquote]', true, '⌃`'],
    ['Mod+[KeyJ]', true, '⌘J'],
    ['Mod+Shift?+=', true, '⌘='],
    ['Mod+Shift?++', true, '⌘+'],
    ['Mod+Shift+Backspace', true, '⇧⌘⌫'],
    ['Mod+ArrowDown', false, 'Ctrl+Down'],
    ['Ctrl+Alt+Shift+Mod+T', true, '⌃⌥⇧⌘T'],
  ] as const)('%s (mac=%s) → %s', (chord, isMac, want) => {
    expect(chordLabel(chord, isMac)).toBe(want);
  });
  it('withKey drops the parentheses for a command with no key', () => {
    expect(withKey('Worktrees', '⌘E')).toBe('Worktrees (⌘E)');
    expect(withKey('Worktrees', '')).toBe('Worktrees');
  });
});
