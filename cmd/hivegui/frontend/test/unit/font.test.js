import { describe, it, expect } from 'vitest';
import {
  clampFont, DEFAULT_FONT_SIZE, MIN_FONT_SIZE, MAX_FONT_SIZE,
} from '../../src/lib/font.js';

describe('clampFont', () => {
  it('passes valid sizes through', () => {
    expect(clampFont(14)).toBe(14);
    expect(clampFont(20)).toBe(20);
  });
  it('clamps to bounds', () => {
    expect(clampFont(2)).toBe(MIN_FONT_SIZE);
    expect(clampFont(99)).toBe(MAX_FONT_SIZE);
  });
  it('rounds non-integers', () => {
    expect(clampFont(14.7)).toBe(15);
    expect(clampFont(13.4)).toBe(13);
  });
  it('returns the default for NaN / non-finite', () => {
    expect(clampFont(NaN)).toBe(DEFAULT_FONT_SIZE);
    expect(clampFont(Infinity)).toBe(DEFAULT_FONT_SIZE);
    expect(clampFont(undefined)).toBe(DEFAULT_FONT_SIZE);
  });
});
