import { describe, it, expect } from 'vitest';
import {
  shouldRequestReplay,
  REPLAY_COL_THRESHOLD,
  REPLAY_DEBOUNCE_MS,
} from '../../src/lib/scrollback.js';

describe('shouldRequestReplay', () => {
  it('returns true on grid → single (large widen)', () => {
    expect(shouldRequestReplay(40, 200)).toBe(true);
  });

  it('returns true on single → grid (large shrink)', () => {
    expect(shouldRequestReplay(200, 40)).toBe(true);
  });

  it('returns true exactly at the threshold', () => {
    expect(shouldRequestReplay(80, 80 + REPLAY_COL_THRESHOLD)).toBe(true);
    expect(shouldRequestReplay(80, 80 - REPLAY_COL_THRESHOLD)).toBe(true);
  });

  it('returns false below the threshold (kerning jitter)', () => {
    expect(shouldRequestReplay(80, 81)).toBe(false);
    expect(shouldRequestReplay(80, 83)).toBe(false);
    expect(shouldRequestReplay(80, 80)).toBe(false);
  });

  it('returns false when prevCols is missing (first measurement)', () => {
    expect(shouldRequestReplay(undefined, 80)).toBe(false);
    expect(shouldRequestReplay(0, 80)).toBe(false);
  });

  it('returns false when nextCols is zero (hidden tile)', () => {
    expect(shouldRequestReplay(80, 0)).toBe(false);
  });

  it('accepts a custom threshold', () => {
    expect(shouldRequestReplay(80, 82, 2)).toBe(true);
    expect(shouldRequestReplay(80, 81, 2)).toBe(false);
  });
});

describe('debounce timing constant', () => {
  it('is small enough for live use', () => {
    expect(REPLAY_DEBOUNCE_MS).toBeGreaterThan(0);
    expect(REPLAY_DEBOUNCE_MS).toBeLessThan(1000);
  });
});
