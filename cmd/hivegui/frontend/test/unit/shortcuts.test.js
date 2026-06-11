import { describe, it, expect } from 'vitest';
import { shortcutGroups, paletteShortcuts, footerHints } from '../../src/lib/shortcuts.js';

describe('shortcutGroups', () => {
  it('renders mac glyphs on mac and Ctrl+ words elsewhere', () => {
    const mac = shortcutGroups({ isMac: true });
    const other = shortcutGroups({ isMac: false });
    const macKeys = mac.flatMap((g) => g.items.map((i) => i.keys)).join(' ');
    const otherKeys = other.flatMap((g) => g.items.map((i) => i.keys)).join(' ');
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
        expect(new Set(keys).size, `${g.title} (isMac=${isMac}): ${keys}`).toBe(keys.length);
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

  it('mac-only clear-line entry appears only on mac', () => {
    const labels = (groups) => groups.flatMap((g) => g.items.map((i) => i.label));
    expect(labels(shortcutGroups({ isMac: true }))).toContain('Clear input line');
    expect(labels(shortcutGroups({ isMac: false }))).not.toContain('Clear input line');
  });

  it('separates arrow-key word labels off mac', () => {
    const keys = shortcutGroups({ isMac: false })
      .flatMap((g) => g.items.map((i) => i.keys))
      .join(' ');
    expect(keys).toContain('Ctrl+Up/Down/Left/Right');
    expect(keys).toContain('Ctrl+Shift+Up/Down/Left/Right');
    expect(keys).toContain('Up/Down / Tab');
    expect(keys).toContain('Left/Right');
    expect(keys).not.toMatch(/UpDown|LeftRight/);
    // Mac glyphs stay run together — the conventional rendering.
    const macKeys = shortcutGroups({ isMac: true })
      .flatMap((g) => g.items.map((i) => i.keys))
      .join(' ');
    expect(macKeys).toContain('⌘↑↓←→');
  });
});

describe('footerHints', () => {
  it('matches the static mac footer text in index.html', () => {
    expect(footerHints({ isMac: true })).toBe(
      '⌘N project · ⌘T session · ⌘W close · ⌘G grid · ⇧⌘K commands · ⌘/ help',
    );
  });

  it('uses Ctrl+ words off mac', () => {
    const f = footerHints({ isMac: false });
    expect(f).toContain('Ctrl+T session');
    expect(f).toContain('Ctrl+Shift+K commands');
    expect(f).toContain('Ctrl+/ help');
    expect(f).not.toContain('⌘');
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
