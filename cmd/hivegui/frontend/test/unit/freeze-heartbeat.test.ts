import { describe, it, expect } from 'vitest';
import { classifyBeat, jsHeapMB } from '../../src/lib/freeze-heartbeat.js';

const base = {
  nominalMs: 1000,
  visible: true,
  beat: 1,
  aliveEvery: 0,
  state: null,
};

describe('classifyBeat', () => {
  it('logs a STALL when the visible thread was blocked past 2x nominal', () => {
    const line = classifyBeat({ ...base, gap: 5000 });
    expect(line).toMatch(/^hb STALL gap=5000ms/);
  });

  it('stays silent on a normal-cadence tick', () => {
    expect(classifyBeat({ ...base, gap: 1010 })).toBeNull();
  });

  it('does NOT treat a large gap while hidden as a stall (timers throttle)', () => {
    expect(classifyBeat({ ...base, gap: 60000, visible: false })).toBeNull();
  });

  it('emits a periodic alive line carrying window state', () => {
    const line = classifyBeat({
      ...base,
      gap: 1000,
      beat: 15,
      aliveEvery: 15,
      state: { vis: 'visible', focus: 1, terms: 12 },
    });
    expect(line).toMatch(/^hb alive vis=1 /);
    expect(line).toContain('terms=12');
  });

  it('a stall wins over the alive cadence and includes state', () => {
    const line = classifyBeat({
      ...base,
      gap: 9000,
      beat: 15,
      aliveEvery: 15,
      state: { view: 'grid-all' },
    });
    expect(line).toMatch(/^hb STALL/);
    expect(line).toContain('view=grid-all');
  });
});

describe('jsHeapMB', () => {
  it('returns rounded MB when performance.memory is exposed', () => {
    expect(jsHeapMB({ memory: { usedJSHeapSize: 100 * 1024 * 1024 } })).toBe(
      100,
    );
  });
  it('returns null when the engine does not expose memory (WebKit)', () => {
    expect(jsHeapMB({})).toBeNull();
    expect(jsHeapMB(null)).toBeNull();
  });
});
