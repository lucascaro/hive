import { describe, it, expect, beforeEach } from 'vitest';
import {
  acquireWebglSlot,
  releaseWebglSlot,
  activeWebglSlots,
  _resetWebglBudget,
  recordWebglLoss,
} from '../../src/lib/webgl-budget.js';

describe('webgl context budget', () => {
  beforeEach(() => _resetWebglBudget());

  it('grants slots up to the budget, then refuses', () => {
    expect(acquireWebglSlot(3)).toBe(true);
    expect(acquireWebglSlot(3)).toBe(true);
    expect(acquireWebglSlot(3)).toBe(true);
    expect(acquireWebglSlot(3)).toBe(false); // over budget → DOM renderer
    expect(activeWebglSlots()).toBe(3);
  });

  it('release frees a slot for the next tile', () => {
    acquireWebglSlot(1);
    expect(acquireWebglSlot(1)).toBe(false);
    releaseWebglSlot();
    expect(acquireWebglSlot(1)).toBe(true);
  });

  it('over-release floors at 0 (double dispose is safe)', () => {
    releaseWebglSlot();
    releaseWebglSlot();
    expect(activeWebglSlots()).toBe(0);
    expect(acquireWebglSlot(1)).toBe(true); // still only 1 real slot
  });
});

describe('recordWebglLoss (context-loss storm guard)', () => {
  const MAX = 3;
  const WIN = 10000;

  it('does not storm at or below the threshold', () => {
    const s = { start: 0, count: 0 };
    expect(recordWebglLoss(s, 1000, MAX, WIN).stormed).toBe(false); // 1
    expect(recordWebglLoss(s, 1100, MAX, WIN).stormed).toBe(false); // 2
    expect(recordWebglLoss(s, 1200, MAX, WIN).stormed).toBe(false); // 3
  });

  it('storms once losses exceed the threshold within the window', () => {
    const s = { start: 0, count: 0 };
    for (let i = 0; i < MAX; i++) recordWebglLoss(s, 1000 + i, MAX, WIN);
    const r = recordWebglLoss(s, 1000 + MAX, MAX, WIN); // 4th
    expect(r.count).toBe(4);
    expect(r.stormed).toBe(true);
  });

  it('resets the counter after the window elapses (slow flapping is fine)', () => {
    const s = { start: 0, count: 0 };
    recordWebglLoss(s, 1000, MAX, WIN);
    recordWebglLoss(s, 2000, MAX, WIN);
    recordWebglLoss(s, 3000, MAX, WIN);
    // Next loss is past the window → counter restarts, no storm.
    const r = recordWebglLoss(s, 3000 + WIN + 1, MAX, WIN);
    expect(r.count).toBe(1);
    expect(r.stormed).toBe(false);
  });
});
