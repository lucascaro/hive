import { describe, it, expect } from 'vitest';
import { resolveTheme, DEFAULT_THEME } from '../../src/theme/theme';

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
