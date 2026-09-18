// @vitest-environment jsdom
//
// The find highlight colours (spec 431), from src/theme/theme.ts.
//
// Both rules here were found by looking at the real terminal, not by
// reasoning, and both break silently:
//   - ordinary matches must carry NO fill. The active match is a second
//     decoration on the same cells, and the renderer lets the ordinary
//     fill win — every match looked identical.
//   - the active match is shown as a SELECTION, which paints over its
//     decoration, so while the box is open the selection must be the
//     accent or the active match renders as a pale 30% tint.
import { afterEach, describe, expect, it } from 'vitest';
import { findDecorations, findTermTheme } from '../../src/theme/theme.js';

function setTokens(tokens: Record<string, string>) {
  for (const [k, v] of Object.entries(tokens)) {
    document.documentElement.style.setProperty(k, v);
  }
}

afterEach(() => {
  document.documentElement.removeAttribute('style');
});

describe('findDecorations', () => {
  it('fills only the active match, in the theme accent', () => {
    setTokens({ '--accent': '#c47a12', '--term-bg': '#ffffff' });
    const d = findDecorations();
    expect(d.activeMatchBackground).toBe('#c47a12');
    // The regression: a matchBackground covers the active fill.
    expect('matchBackground' in d).toBe(false);
    expect(d.matchBorder).toBe('#c47a12');
  });

  // The addon only accepts #RRGGBB.
  it('normalizes a short hex accent', () => {
    setTokens({ '--accent': '#fa0' });
    expect(findDecorations().activeMatchBackground).toBe('#ffaa00');
  });

  // The addon only reports result counts when decorations are enabled,
  // so an unusable accent must fall back — never drop the decorations,
  // which would turn every search into 0/0.
  it('falls back rather than returning nothing for a non-hex accent', () => {
    setTokens({ '--accent': 'rebeccapurple' });
    const d = findDecorations();
    expect(d).toBeDefined();
    expect(d.activeMatchBackground).toMatch(/^#[0-9a-f]{6}$/);
  });
});

describe('findTermTheme', () => {
  it('makes the selection the accent while the box is open', () => {
    setTokens({ '--accent': '#c47a12', '--on-accent': '#1a1b22' });
    const t = findTermTheme();
    // The terminal is unfocused while the box has focus, so the
    // INACTIVE selection colour is the one that actually shows.
    expect(t.selectionInactiveBackground).toBe('#c47a12');
    expect(t.selectionBackground).toBe('#c47a12');
    expect(t.selectionForeground).toBe('#1a1b22');
  });
});
