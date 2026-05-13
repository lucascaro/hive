import { describe, it, expect, vi } from 'vitest';
import {
  shouldRefreshOnVisibility,
  recoverFromContextLoss,
  bindDprWatcher,
} from '../../src/lib/renderer-recovery.js';

function makeFakeMql() {
  const listeners = new Set();
  return {
    addEventListener: vi.fn((evt, fn) => { if (evt === 'change') listeners.add(fn); }),
    removeEventListener: vi.fn((evt, fn) => { if (evt === 'change') listeners.delete(fn); }),
    fire() { for (const fn of [...listeners]) fn(); },
    listenerCount() { return listeners.size; },
  };
}

describe('shouldRefreshOnVisibility', () => {
  it('refreshes only when becoming visible', () => {
    expect(shouldRefreshOnVisibility('visible')).toBe(true);
  });
  it('does not refresh while hidden / prerendering', () => {
    expect(shouldRefreshOnVisibility('hidden')).toBe(false);
    expect(shouldRefreshOnVisibility('prerender')).toBe(false);
    expect(shouldRefreshOnVisibility(undefined)).toBe(false);
  });
});

describe('recoverFromContextLoss', () => {
  it('disposes, reattaches, and skips refresh when reattach succeeds', () => {
    const deps = {
      dispose: vi.fn(),
      reattach: vi.fn(() => true),
      refresh: vi.fn(),
    };
    const result = recoverFromContextLoss(deps);
    expect(deps.dispose).toHaveBeenCalledTimes(1);
    expect(deps.reattach).toHaveBeenCalledTimes(1);
    expect(deps.refresh).not.toHaveBeenCalled();
    expect(result).toEqual({ reattached: true });
  });

  it('falls back to refresh when reattach returns false', () => {
    // Reattach failure means the GL context cap is still saturated and
    // we land on the DOM renderer; force a repaint so stale pixels
    // from the lost context aren't left frozen on the canvas.
    const deps = {
      dispose: vi.fn(),
      reattach: vi.fn(() => false),
      refresh: vi.fn(),
    };
    const result = recoverFromContextLoss(deps);
    expect(deps.refresh).toHaveBeenCalledTimes(1);
    expect(result).toEqual({ reattached: false });
  });

  it('treats a throwing reattach as a failed reattach', () => {
    const deps = {
      dispose: vi.fn(),
      reattach: vi.fn(() => { throw new Error('no WebGL2'); }),
      refresh: vi.fn(),
    };
    const result = recoverFromContextLoss(deps);
    expect(deps.refresh).toHaveBeenCalledTimes(1);
    expect(result).toEqual({ reattached: false });
  });

  it('swallows a throwing dispose so reattach is still attempted', () => {
    // Dispose can throw on an already-disposed addon (xterm.js raises
    // on double-dispose). The recovery path must keep going.
    const deps = {
      dispose: vi.fn(() => { throw new Error('already disposed'); }),
      reattach: vi.fn(() => true),
      refresh: vi.fn(),
    };
    const result = recoverFromContextLoss(deps);
    expect(deps.reattach).toHaveBeenCalledTimes(1);
    expect(result.reattached).toBe(true);
  });

  it('swallows a throwing refresh on the fallback path', () => {
    // Refresh can throw if the terminal was disposed mid-recovery.
    // Recovery must not propagate that into the event handler.
    const deps = {
      dispose: vi.fn(),
      reattach: vi.fn(() => false),
      refresh: vi.fn(() => { throw new Error('disposed'); }),
    };
    expect(() => recoverFromContextLoss(deps)).not.toThrow();
  });

  it('coerces non-boolean reattach return to reattached: false', () => {
    // Defends against a future reattach() that accidentally returns
    // the addon instance instead of true/false.
    const deps = {
      dispose: vi.fn(),
      reattach: vi.fn(() => ({ some: 'addon' })),
      refresh: vi.fn(),
    };
    const result = recoverFromContextLoss(deps);
    expect(result.reattached).toBe(false);
    expect(deps.refresh).toHaveBeenCalledTimes(1);
  });
});

describe('bindDprWatcher', () => {
  it('binds an initial change listener against the current DPR', () => {
    const mql = makeFakeMql();
    const matchMedia = vi.fn(() => mql);
    const onChange = vi.fn();
    const watcher = bindDprWatcher({
      matchMedia,
      getDpr: () => 2,
      onChange,
    });
    expect(watcher).not.toBeNull();
    expect(matchMedia).toHaveBeenCalledWith('(resolution: 2dppx)');
    expect(mql.listenerCount()).toBe(1);
    expect(onChange).not.toHaveBeenCalled();
  });

  it('rebinds against the new DPR on each change and keeps firing onChange', () => {
    // A `(resolution: Xdppx)` MQL only fires once when DPR moves away
    // from X. The watcher must rebind so subsequent DPR transitions
    // continue to trigger renderer refreshes.
    const mqls = [makeFakeMql(), makeFakeMql(), makeFakeMql()];
    let i = 0;
    const matchMedia = vi.fn(() => mqls[i++]);
    let dpr = 1;
    const onChange = vi.fn();
    const watcher = bindDprWatcher({
      matchMedia,
      getDpr: () => dpr,
      onChange,
    });
    expect(matchMedia).toHaveBeenNthCalledWith(1, '(resolution: 1dppx)');

    // First DPR transition: 1 → 2.
    dpr = 2;
    mqls[0].fire();
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(matchMedia).toHaveBeenNthCalledWith(2, '(resolution: 2dppx)');
    // The stale MQL must be removed so it doesn't leak listeners.
    expect(mqls[0].listenerCount()).toBe(0);
    expect(mqls[1].listenerCount()).toBe(1);

    // Second DPR transition: 2 → 3. This is the regression case —
    // before the rebind fix, no further events fired here.
    dpr = 3;
    mqls[1].fire();
    expect(onChange).toHaveBeenCalledTimes(2);
    expect(matchMedia).toHaveBeenNthCalledWith(3, '(resolution: 3dppx)');
    expect(mqls[1].listenerCount()).toBe(0);
    expect(mqls[2].listenerCount()).toBe(1);

    watcher.teardown();
    expect(mqls[2].listenerCount()).toBe(0);
  });

  it('teardown removes the currently-bound listener', () => {
    const mql = makeFakeMql();
    const watcher = bindDprWatcher({
      matchMedia: () => mql,
      getDpr: () => 1,
      onChange: () => {},
    });
    expect(mql.listenerCount()).toBe(1);
    watcher.teardown();
    expect(mql.listenerCount()).toBe(0);
    // Idempotent.
    expect(() => watcher.teardown()).not.toThrow();
  });

  it('returns null when matchMedia throws (unsupported platform)', () => {
    const watcher = bindDprWatcher({
      matchMedia: () => { throw new Error('not supported'); },
      getDpr: () => 1,
      onChange: () => {},
    });
    expect(watcher).toBeNull();
  });

  it('swallows a throwing onChange so the listener chain keeps working', () => {
    const mqls = [makeFakeMql(), makeFakeMql()];
    let i = 0;
    const onChange = vi.fn(() => { throw new Error('host blew up'); });
    const watcher = bindDprWatcher({
      matchMedia: () => mqls[i++],
      getDpr: () => 1,
      onChange,
    });
    expect(() => mqls[0].fire()).not.toThrow();
    expect(onChange).toHaveBeenCalledTimes(1);
    // Rebind still happened despite onChange throwing.
    expect(mqls[1].listenerCount()).toBe(1);
    watcher.teardown();
  });
});
