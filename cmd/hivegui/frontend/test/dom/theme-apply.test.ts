import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  readTheme,
  applyTheme,
  applyOverrides,
  DEFAULT_THEME,
  THEME_KEY,
  THEME_DARK_KEY,
  THEME_LIGHT_KEY,
  DEFAULT_PAIR,
  readPair,
  resolveTheme,
} from '../../src/theme/theme';

describe('readTheme', () => {
  it('reads a valid preset from storage', () => {
    const data: Record<string, string | null> = { [THEME_KEY]: 'hive-light' };
    const result = readTheme({
      getItem: (k: string) => data[k] ?? null,
      length: 0,
      clear: () => {},
      key: () => null,
      removeItem: () => {},
      setItem: () => {},
    });
    expect(result).toBe('hive-light');
  });

  it('returns DEFAULT_THEME when storage has null', () => {
    const data: Record<string, string | null> = {};
    const result = readTheme({
      getItem: (k: string) => data[k] ?? null,
      length: 0,
      clear: () => {},
      key: () => null,
      removeItem: () => {},
      setItem: () => {},
    });
    expect(result).toBe(DEFAULT_THEME);
  });

  it('returns DEFAULT_THEME when storage has the string "null"', () => {
    const data: Record<string, string | null> = { 'hive.theme': 'null' };
    const result = readTheme({
      getItem: (k: string) => data[k] ?? null,
      length: 0,
      clear: () => {},
      key: () => null,
      removeItem: () => {},
      setItem: () => {},
    });
    expect(result).toBe(DEFAULT_THEME);
  });

  it('returns DEFAULT_THEME when storage has garbage', () => {
    const data: Record<string, string | null> = {
      'hive.theme': '<script>alert("xss")</script>',
    };
    const result = readTheme({
      getItem: (k: string) => data[k] ?? null,
      length: 0,
      clear: () => {},
      key: () => null,
      removeItem: () => {},
      setItem: () => {},
    });
    expect(result).toBe(DEFAULT_THEME);
  });

  it('returns DEFAULT_THEME when Storage.getItem throws', () => {
    const result = readTheme({
      getItem: () => {
        throw new Error('access denied');
      },
      length: 0,
      clear: () => {},
      key: () => null,
      removeItem: () => {},
      setItem: () => {},
    });
    expect(result).toBe(DEFAULT_THEME);
  });

  it('returns DEFAULT_THEME when localStorage property access throws', () => {
    // Regression test for: readTheme's old signature had storage: Storage = localStorage
    // as default parameter, evaluated outside the try/catch. If localStorage access
    // itself throws (e.g., locked-down webview, private-browsing mode), the exception
    // escaped readTheme. The fix moves the access inside try.
    const originalDescriptor = Object.getOwnPropertyDescriptor(
      globalThis,
      'localStorage',
    );
    try {
      Object.defineProperty(globalThis, 'localStorage', {
        configurable: true,
        get() {
          throw new Error('localStorage access denied');
        },
      });
      const result = readTheme();
      expect(result).toBe(DEFAULT_THEME);
    } finally {
      if (originalDescriptor) {
        Object.defineProperty(globalThis, 'localStorage', originalDescriptor);
      } else {
        delete (globalThis as any).localStorage;
      }
    }
  });
});

describe('applyTheme', () => {
  let originalMatchMedia: any;

  beforeEach(() => {
    originalMatchMedia = window.matchMedia;
  });

  afterEach(() => {
    window.matchMedia = originalMatchMedia;
  });

  it('resolves "system" to hive-dark when dark mode is preferred', () => {
    window.matchMedia = ((query: string) => ({
      matches: query === '(prefers-color-scheme: dark)',
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => true,
    })) as any;

    applyTheme('system');
    expect(document.documentElement.dataset.theme).toBe('hive-dark');
  });

  it('resolves "system" to hive-light when light mode is preferred', () => {
    window.matchMedia = ((query: string) => ({
      matches: query === '(prefers-color-scheme: light)',
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => true,
    })) as any;

    applyTheme('system');
    expect(document.documentElement.dataset.theme).toBe('hive-light');
  });

  it('never writes the literal string "system" to data-theme', () => {
    window.matchMedia = ((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => true,
    })) as any;

    applyTheme('system');
    expect(document.documentElement.dataset.theme).not.toBe('system');
    expect(['classic', 'hive-dark', 'hive-light']).toContain(
      document.documentElement.dataset.theme,
    );
  });

  it('writes concrete presets without resolution', () => {
    applyTheme('hive-light');
    expect(document.documentElement.dataset.theme).toBe('hive-light');

    applyTheme('hive-dark');
    expect(document.documentElement.dataset.theme).toBe('hive-dark');

    applyTheme('classic');
    expect(document.documentElement.dataset.theme).toBe('classic');
  });

  it('defaults to hive-dark when matchMedia is unavailable', () => {
    window.matchMedia = undefined as any;
    applyTheme('system');
    expect(document.documentElement.dataset.theme).toBe('hive-dark');
  });
});

describe('applyOverrides', () => {
  // ':root:root' rather than ':root' — themes.css's preset blocks are
  // :root[data-theme="…"] (0,2,0), which a plain :root (0,1,0) loses to
  // regardless of order, so every user override was silently ignored.
  it('writes overrides into the static style element, replacing not appending', () => {
    document.head.innerHTML = '<style id="theme-overrides"></style>';
    applyOverrides('--accent: red;');
    const el = document.getElementById('theme-overrides');
    expect(el?.textContent).toBe(':root:root {\n  --accent: red;\n}');
    applyOverrides('--accent: blue;');
    expect(el?.textContent).toBe(':root:root {\n  --accent: blue;\n}');
    applyOverrides('');
    expect(el?.textContent).toBe('');
  });
});

// A "system" choice resolves to a PAIR of presets, one per OS scheme, and
// the pair is the user's to pick — Dracula when dark, GitHub Light when
// light. Each half validates on its own: one garbage key must not drag
// the other back to its default.
describe('readPair', () => {
  const storage = (data: Record<string, string>): Storage => ({
    getItem: (k: string) => data[k] ?? null,
    length: 0,
    clear: () => {},
    key: () => null,
    removeItem: () => {},
    setItem: () => {},
  });

  it('returns hive-dark / hive-light when nothing is stored', () => {
    expect(readPair(storage({}))).toEqual(DEFAULT_PAIR);
    expect(DEFAULT_PAIR).toEqual({ dark: 'hive-dark', light: 'hive-light' });
  });

  it('reads a valid preset for each half', () => {
    expect(
      readPair(
        storage({
          [THEME_DARK_KEY]: 'dracula',
          [THEME_LIGHT_KEY]: 'github-light',
        }),
      ),
    ).toEqual({ dark: 'dracula', light: 'github-light' });
  });

  it('falls back per half, not as a pair', () => {
    expect(
      readPair(
        storage({ [THEME_DARK_KEY]: 'nord', [THEME_LIGHT_KEY]: 'bogus' }),
      ),
    ).toEqual({ dark: 'nord', light: 'hive-light' });
  });

  it('never accepts "system" as a half — that would recurse', () => {
    expect(
      readPair(
        storage({ [THEME_DARK_KEY]: 'system', [THEME_LIGHT_KEY]: 'system' }),
      ),
    ).toEqual(DEFAULT_PAIR);
  });

  it('returns the defaults when storage throws', () => {
    expect(
      readPair({
        ...storage({}),
        getItem: () => {
          throw new Error('denied');
        },
      }),
    ).toEqual(DEFAULT_PAIR);
  });
});

describe('resolveTheme with a pair', () => {
  it('stamps the dark half when dark is preferred', () => {
    expect(
      resolveTheme('system', true, { dark: 'dracula', light: 'github-light' }),
    ).toBe('dracula');
  });

  it('stamps the light half when light is preferred', () => {
    expect(
      resolveTheme('system', false, { dark: 'dracula', light: 'github-light' }),
    ).toBe('github-light');
  });

  it('ignores the pair for an explicit preset', () => {
    expect(
      resolveTheme('classic', true, { dark: 'dracula', light: 'github-light' }),
    ).toBe('classic');
  });
});

describe('applyTheme reads the stored pair', () => {
  let originalMatchMedia: any;
  beforeEach(() => {
    originalMatchMedia = window.matchMedia;
    localStorage.clear();
  });
  afterEach(() => {
    window.matchMedia = originalMatchMedia;
    localStorage.clear();
  });

  it('resolves "system" through hive.theme.dark when dark is preferred', () => {
    window.matchMedia = ((query: string) => ({
      matches: query === '(prefers-color-scheme: dark)',
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => true,
    })) as any;
    localStorage.setItem(THEME_DARK_KEY, 'dracula');
    localStorage.setItem(THEME_LIGHT_KEY, 'github-light');
    applyTheme('system');
    expect(document.documentElement.dataset.theme).toBe('dracula');
  });

  it('resolves "system" through hive.theme.light when light is preferred', () => {
    window.matchMedia = ((query: string) => ({
      matches: query === '(prefers-color-scheme: light)',
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => true,
    })) as any;
    localStorage.setItem(THEME_DARK_KEY, 'dracula');
    localStorage.setItem(THEME_LIGHT_KEY, 'github-light');
    applyTheme('system');
    expect(document.documentElement.dataset.theme).toBe('github-light');
  });
});

// A half picked under a store that refuses writes is applied for the
// session but never lands in storage. The OS-change listener re-applies
// 'system' with no pair of its own, so it must get the pair applyTheme
// was LAST HANDED — not a fresh read of the store the write never
// reached. Module-level memory, so each test takes a fresh module.
describe('the last applied pair outlives the store', () => {
  const mq = (dark: boolean) =>
    ((query: string) => ({
      matches: dark === (query === '(prefers-color-scheme: dark)'),
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => true,
    })) as any;
  let originalMatchMedia: any;
  beforeEach(() => {
    originalMatchMedia = window.matchMedia;
    localStorage.clear();
    vi.resetModules();
  });
  afterEach(() => {
    window.matchMedia = originalMatchMedia;
    localStorage.clear();
  });
  const fresh = () => import('../../src/theme/theme');

  it('a later apply without a pair reuses the pair it was last handed', async () => {
    const t = await fresh();
    window.matchMedia = mq(true);
    t.applyTheme('system', document, { dark: 'nord', light: 'github-light' });
    expect(document.documentElement.dataset.theme).toBe('nord');
    // The OS flips; storage still holds nothing.
    window.matchMedia = mq(false);
    t.applyTheme('system');
    expect(document.documentElement.dataset.theme).toBe('github-light');
  });

  it('currentPair falls back to storage before any pair is applied', async () => {
    const t = await fresh();
    localStorage.setItem(t.THEME_DARK_KEY, 'dracula');
    expect(t.currentPair()).toEqual({ dark: 'dracula', light: 'hive-light' });
  });

  it('currentPair prefers the last applied pair over storage', async () => {
    const t = await fresh();
    localStorage.setItem(t.THEME_DARK_KEY, 'dracula');
    t.applyTheme('system', document, { dark: 'nord', light: 'github-light' });
    expect(t.currentPair()).toEqual({ dark: 'nord', light: 'github-light' });
  });
});
