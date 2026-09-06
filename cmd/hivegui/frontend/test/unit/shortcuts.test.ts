import { describe, it, expect } from 'vitest';
import {
  shortcutGroups,
  paletteShortcuts,
  type ShortcutGroup,
} from '../../src/lib/shortcuts.js';

describe('shortcutGroups', () => {
  it('renders mac glyphs on mac and Ctrl+ words elsewhere', () => {
    const mac = shortcutGroups({ isMac: true });
    const other = shortcutGroups({ isMac: false });
    const macKeys = mac.flatMap((g) => g.items.map((i) => i.keys)).join(' ');
    const otherKeys = other
      .flatMap((g) => g.items.map((i) => i.keys))
      .join(' ');
    expect(macKeys).toContain('⌘T');
    expect(macKeys).toContain('⇧⌘T');
    expect(otherKeys).toContain('Ctrl+T');
    expect(otherKeys).toContain('Ctrl+Shift+T');
    expect(otherKeys).not.toContain('⌘');
  });

  it('has no duplicate key combos within a group', () => {
    for (const isMac of [true, false]) {
      for (const g of shortcutGroups({ isMac })) {
        const keys = g.items.map((i) => i.keys);
        expect(new Set(keys).size, `${g.title} (isMac=${isMac}): ${keys}`).toBe(
          keys.length,
        );
      }
    }
  });

  it('every item has keys and a label', () => {
    for (const g of shortcutGroups({ isMac: true })) {
      for (const i of g.items) {
        expect(i.keys).toBeTruthy();
        expect(i.label).toBeTruthy();
      }
    }
  });

  it('mac-only line-edit entries appear only on mac', () => {
    const labels = (groups: ShortcutGroup[]) =>
      groups.flatMap((g) => g.items.map((i) => i.label));
    expect(labels(shortcutGroups({ isMac: true }))).toContain(
      'Delete to start of line',
    );
    expect(labels(shortcutGroups({ isMac: true }))).toContain(
      'Delete to end of line',
    );
    expect(labels(shortcutGroups({ isMac: false }))).not.toContain(
      'Delete to start of line',
    );
    expect(labels(shortcutGroups({ isMac: false }))).not.toContain(
      'Delete to end of line',
    );
  });

  it('labels the forward-delete key per platform, not as the literal "delete"', () => {
    // KEYS had no `delete` entry until #362, so keyLabel() fell through
    // its `k ? … : key` default and rendered the raw string.
    const keysFor = (isMac: boolean) =>
      shortcutGroups({ isMac })
        .flatMap((g) => g.items)
        .filter((i) => i.label === 'Delete the character after the cursor')
        .map((i) => i.keys);
    expect(keysFor(true)).toEqual(['⌦']);
    expect(keysFor(false)).toEqual(['Del']);
  });

  it('does not contradict itself about what ⌘/Ctrl + ←/→ send to the terminal', () => {
    // The Sessions row and the "Inside a terminal" row describe the same
    // chord off mac (Ctrl+←/→). macLineEditSeq is mac-gated, so off mac
    // the chord falls through to xterm as word movement — the Sessions row
    // must not claim start/end of line there.
    const gridRow = (isMac: boolean) =>
      shortcutGroups({ isMac })
        .flatMap((g) => g.items)
        .find((i) => i.label.startsWith('Grid: move between tiles —'))?.label;
    expect(gridRow(true)).toContain('start / end of line');
    expect(gridRow(false)).toContain('move by word');
    expect(gridRow(false)).not.toContain('start / end of line');
  });

  it('separates arrow-key word labels off mac', () => {
    const keys = shortcutGroups({ isMac: false })
      .flatMap((g) => g.items.map((i) => i.keys))
      .join(' ');
    // The vertical and horizontal arrows are advertised separately: they
    // no longer do the same thing (⌘←/→ reach the terminal in focused
    // mode, and reorder is ⇧⌘↑/↓ only).
    expect(keys).toContain('Ctrl+Up/Down');
    expect(keys).toContain('Ctrl+Left/Right');
    expect(keys).toContain('Ctrl+Shift+Up/Down');
    expect(keys).not.toContain('Ctrl+Shift+Left/Right');
    expect(keys).toContain('Up/Down / Tab');
    expect(keys).not.toMatch(/UpDown|LeftRight/);
    // Mac glyphs stay run together — the conventional rendering.
    const macKeys = shortcutGroups({ isMac: true })
      .flatMap((g) => g.items.map((i) => i.keys))
      .join(' ');
    expect(macKeys).toContain('⌘↑↓');
    expect(macKeys).toContain('⌘←→');
    expect(macKeys).toContain('⇧⌘↑↓');
  });
});

describe('paletteShortcuts', () => {
  it('matches the palette glyphs the mac UI has always shown', () => {
    const m = paletteShortcuts({ isMac: true });
    // Pin against the previous hardcoded literals so the refactor to a
    // shared module cannot change what users see.
    expect(m['new-project']).toBe('⌘N');
    expect(m['new-session']).toBe('⌘T');
    expect(m['new-session-worktree']).toBe('⇧⌘T');
    expect(m['duplicate-session']).toBe('⌘P');
    expect(m['delete-project']).toBe('⇧⌘⌫');
    expect(m['close-session']).toBe('⌘W');
    expect(m['new-window']).toBe('⇧⌘N');
    expect(m['open-os-terminal']).toBe('⌃`');
    expect(m['close-window']).toBe('⇧⌘W');
    expect(m['toggle-sidebar']).toBe('⌘S');
    expect(m['toggle-project-grid']).toBe('⌘G');
    expect(m['toggle-all-grid']).toBe('⇧⌘G');
    expect(m['zoom-in']).toBe('⌘=');
    expect(m['zoom-out']).toBe('⌘-');
    expect(m['zoom-reset']).toBe('⌘0');
    expect(m['next-session']).toBe('⌘↓');
    expect(m['prev-session']).toBe('⌘↑');
    expect(m['move-forward']).toBe('⇧⌘↓');
    expect(m['move-backward']).toBe('⇧⌘↑');
    expect(m['next-project']).toBe('⌘]');
    expect(m['prev-project']).toBe('⌘[');
    expect(m['restart-session']).toBe('');
    expect(m['switch-1']).toBe('⌘1');
    expect(m['switch-9']).toBe('⌘9');
    expect(m['keyboard-shortcuts']).toBe('⌘/');
  });

  it('uses Ctrl+ words off mac', () => {
    const m = paletteShortcuts({ isMac: false });
    expect(m['new-session']).toBe('Ctrl+T');
    expect(m['new-session-worktree']).toBe('Ctrl+Shift+T');
    expect(m['delete-project']).toBe('Ctrl+Shift+Backspace');
    expect(m['next-session']).toBe('Ctrl+Down');
    expect(m['open-os-terminal']).toBe('Ctrl+`');
  });
});
