// @vitest-environment jsdom
//
// A reorder is a SEQUENCE of daemon moves now (a shared-worktree group moves
// as a block), and the indices in it were computed against a simulated list.
// Two sequences in flight at once therefore interleave into an order neither
// gesture asked for — which is what key auto-repeat produces if nothing stops
// it. This pins the guard.
import { beforeEach, describe, expect, it, vi } from 'vitest';

const calls: Array<[string, number]> = [];
let resolveNext: (() => void) | null = null;

vi.mock('../../src/bridge.js', () => ({
  UpdateSession: (id: string, _n: string, _c: string, order: number) => {
    calls.push([id, order]);
    return new Promise<void>((res) => {
      resolveNext = () => res();
    });
  },
}));
vi.mock('../../src/app/dom.js', () => ({
  reportFailure: () => () => {},
}));

const { runReorder } = await import('../../src/app/reorder-runner.js');

describe('runReorder', () => {
  beforeEach(() => {
    calls.length = 0;
    resolveNext = null;
  });

  it('applies its ops in order, one at a time', async () => {
    const done = runReorder([
      { id: 'a', order: 0 },
      { id: 'b', order: 1 },
    ]);
    // Only the first op is out; the second waits on it.
    expect(calls).toEqual([['a', 0]]);
    resolveNext?.();
    await Promise.resolve();
    await Promise.resolve();
    expect(calls).toEqual([
      ['a', 0],
      ['b', 1],
    ]);
    resolveNext?.();
    await done;
  });

  it('drops a second reorder while one is in flight', async () => {
    const first = runReorder([
      { id: 'a', order: 0 },
      { id: 'b', order: 1 },
    ]);
    // The auto-repeat press: computed against state the first has not
    // finished writing, so it must not start.
    await runReorder([{ id: 'c', order: 2 }]);
    expect(calls).toEqual([['a', 0]]);
    resolveNext?.();
    await Promise.resolve();
    await Promise.resolve();
    resolveNext?.();
    await first;
    expect(calls.map(([id]) => id)).not.toContain('c');
  });

  it('accepts a new reorder once the previous one has finished', async () => {
    const first = runReorder([{ id: 'a', order: 0 }]);
    resolveNext?.();
    await first;
    const second = runReorder([{ id: 'c', order: 2 }]);
    expect(calls).toEqual([
      ['a', 0],
      ['c', 2],
    ]);
    resolveNext?.();
    await second;
  });

  it('ignores an empty op list', async () => {
    await runReorder([]);
    expect(calls).toEqual([]);
  });
});
