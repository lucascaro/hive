import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import {
  readTheme,
  applyTheme,
  DEFAULT_THEME,
  THEME_KEY,
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
