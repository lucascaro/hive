import { describe, it, expect } from 'vitest';
import {
  resolveTheme,
  DEFAULT_THEME,
  PRESETS,
  OVERRIDES_KEY,
  sanitizeOverrides,
  readOverrides,
  writeOverrides,
} from '../../src/theme/theme';

describe('resolveTheme', () => {
  it('defaults to classic when nothing is stored', () => {
    expect(resolveTheme(null, true)).toBe(DEFAULT_THEME);
  });
  it('maps system to hive-dark / hive-light by OS preference', () => {
    expect(resolveTheme('system', true)).toBe('hive-dark');
    expect(resolveTheme('system', false)).toBe('hive-light');
  });
  it('passes known presets through and rejects garbage', () => {
    expect(resolveTheme('hive-light', true)).toBe('hive-light');
    expect(resolveTheme('<script>', true)).toBe(DEFAULT_THEME);
  });
});

describe('PRESETS', () => {
  it('lists every selectable theme exactly once, System first', () => {
    expect(PRESETS[0].id).toBe('system');
    const ids = PRESETS.map((p) => p.id);
    expect(ids).toEqual(['system', 'hive-dark', 'hive-light', 'classic']);
    expect(new Set(ids).size).toBe(ids.length);
    expect(PRESETS.every((p) => p.label.length > 0)).toBe(true);
  });

  it('every non-system preset resolves to itself', () => {
    for (const p of PRESETS) {
      if (p.id === 'system') continue;
      expect(resolveTheme(p.id, true)).toBe(p.id);
    }
  });
});

describe('sanitizeOverrides', () => {
  it('keeps well-formed custom property declarations', () => {
    const r = sanitizeOverrides('--accent: #7aa2f7; --text-md: 14px;');
    expect(r.css).toBe('--accent: #7aa2f7;\n  --text-md: 14px;');
    expect(r.rejected).toEqual([]);
  });

  it('accepts newline-separated input and normalises spacing', () => {
    const r = sanitizeOverrides('  --fg:#fff\n--bg:  #000  ');
    expect(r.css).toBe('--fg: #fff;\n  --bg: #000;');
    expect(r.rejected).toEqual([]);
  });

  it('rejects non-custom properties', () => {
    const r = sanitizeOverrides('color: red; --accent: blue;');
    expect(r.rejected).toEqual(['color: red']);
    expect(r.css).toBe('--accent: blue;');
  });

  it('rejects anything that could escape the :root block', () => {
    for (const bad of [
      '--x: red } body { display: none',
      '--x: url(http://evil/a.png)',
      '--x: expression(alert(1))',
      '@import "http://evil/x.css"',
      '--x: </style><script>alert(1)</script>',
      // Not url(): any function that can reach the network is egress,
      // which is why the function list is an allowlist.
      '--bg: image-set("https://evil/x.png")',
      '--bg: -webkit-image-set("https://evil/x.png")',
      '--bg: src("https://evil/x.png")',
    ]) {
      const r = sanitizeOverrides(bad);
      expect(r.css, bad).toBe('');
      expect(r.rejected.length, bad).toBeGreaterThan(0);
    }
  });

  it('rejects uppercase and empty property names, and empty values', () => {
    const r = sanitizeOverrides('--Accent: red; --: red; --ok:;');
    expect(r.css).toBe('');
    expect(r.rejected).toHaveLength(3);
  });

  // An open paren swallows the appended ';', the rest of the block and
  // its closing brace: one half-typed line used to silently wipe every
  // override, and report nothing because the line itself parsed.
  it('rejects unbalanced parentheses instead of emitting them', () => {
    for (const bad of ['--accent: rgb(', '--accent: rgb(0,0,0))', '--a: )']) {
      const r = sanitizeOverrides(bad);
      expect(r.css, bad).toBe('');
      expect(r.rejected, bad).toEqual([bad.trim()]);
    }
  });

  it('keeps the CSS functions a token legitimately needs', () => {
    const ok =
      '--accent: rgb(122 162 247); --gap: calc(var(--space-2) * 2); --mix: color-mix(in srgb, black 50%, transparent)';
    expect(sanitizeOverrides(ok).rejected).toEqual([]);
  });

  it('is a no-op on empty input', () => {
    expect(sanitizeOverrides('')).toEqual({ css: '', rejected: [] });
    expect(sanitizeOverrides('   \n  ')).toEqual({ css: '', rejected: [] });
  });
});

describe('overrides storage', () => {
  it('round-trips through storage and re-sanitises on read', () => {
    const store = new Map<string, string>();
    const storage = {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
    } as unknown as Storage;
    writeOverrides('--accent: red; color: blue;', storage);
    expect(store.get(OVERRIDES_KEY)).toBe('--accent: red;');
    // Hand-edited store: read must not trust it either.
    store.set(OVERRIDES_KEY, '--a: 1; } body {');
    expect(readOverrides(storage)).toBe('--a: 1;');
  });

  it('survives a storage that throws', () => {
    const storage = {
      getItem() {
        throw new Error('denied');
      },
      setItem() {
        throw new Error('denied');
      },
    } as unknown as Storage;
    expect(readOverrides(storage)).toBe('');
    expect(() => writeOverrides('--a: 1;', storage)).not.toThrow();
  });
});
