import { describe, it, expect } from 'vitest';
import { xtermTheme } from '../../src/theme/theme';

describe('xtermTheme', () => {
  it('reads --term-bg/--term-fg/--accent from the root element', () => {
    document.documentElement.style.setProperty('--term-bg', '#0b0c10');
    document.documentElement.style.setProperty('--term-fg', '#dfe1ea');
    document.documentElement.style.setProperty('--accent', '#ffb454');
    document.documentElement.style.setProperty('--on-accent', '#15120a');
    const t = xtermTheme(document);
    expect(t.background).toBe('#0b0c10');
    expect(t.foreground).toBe('#dfe1ea');
    expect(t.cursor).toBe('#ffb454');
    expect(t.cursorAccent).toBe('#15120a');
  });
});
