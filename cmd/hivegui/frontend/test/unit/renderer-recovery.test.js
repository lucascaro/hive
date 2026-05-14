import { describe, it, expect, vi } from 'vitest';
import {
  shouldRefreshOnVisibility,
  shouldRunAtlasTick,
  recoverFromContextLoss,
  bindDprWatcher,
  bindFocusRefresh,
  bindAtlasInterval,
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

describe('shouldRunAtlasTick', () => {
  it('only ticks when the document is visible', () => {
    expect(shouldRunAtlasTick('visible')).toBe(true);
    expect(shouldRunAtlasTick('hidden')).toBe(false);
    expect(shouldRunAtlasTick('prerender')).toBe(false);
    expect(shouldRunAtlasTick(undefined)).toBe(false);
  });
});

function makeFakeWindow() {
  const listeners = new Map(); // evt -> Set<fn>
  return {
    addEventListener: vi.fn((evt, fn) => {
      if (!listeners.has(evt)) listeners.set(evt, new Set());
      listeners.get(evt).add(fn);
    }),
    removeEventListener: vi.fn((evt, fn) => {
      listeners.get(evt)?.delete(fn);
    }),
    fire(evt) { for (const fn of [...(listeners.get(evt) || [])]) fn(); },
    listenerCount(evt) { return listeners.get(evt)?.size || 0; },
  };
}

describe('bindFocusRefresh', () => {
  it('invokes onRefresh on window focus events', () => {
    const win = makeFakeWindow();
    const onRefresh = vi.fn();
    const watcher = bindFocusRefresh({
      addEventListener: win.addEventListener,
      removeEventListener: win.removeEventListener,
      onRefresh,
    });
    expect(watcher).not.toBeNull();
    expect(win.listenerCount('focus')).toBe(1);
    expect(onRefresh).not.toHaveBeenCalled();
    win.fire('focus');
    expect(onRefresh).toHaveBeenCalledTimes(1);
    win.fire('focus');
    expect(onRefresh).toHaveBeenCalledTimes(2);
  });

  it('teardown removes the focus listener and is idempotent', () => {
    const win = makeFakeWindow();
    const watcher = bindFocusRefresh({
      addEventListener: win.addEventListener,
      removeEventListener: win.removeEventListener,
      onRefresh: () => {},
    });
    expect(win.listenerCount('focus')).toBe(1);
    watcher.teardown();
    expect(win.listenerCount('focus')).toBe(0);
    expect(() => watcher.teardown()).not.toThrow();
    expect(win.listenerCount('focus')).toBe(0);
  });

  it('swallows a throwing onRefresh so future focus events still fire', () => {
    const win = makeFakeWindow();
    const onRefresh = vi.fn(() => { throw new Error('host blew up'); });
    const watcher = bindFocusRefresh({
      addEventListener: win.addEventListener,
      removeEventListener: win.removeEventListener,
      onRefresh,
    });
    expect(() => win.fire('focus')).not.toThrow();
    expect(() => win.fire('focus')).not.toThrow();
    expect(onRefresh).toHaveBeenCalledTimes(2);
    watcher.teardown();
  });

  it('returns null when addEventListener throws (unsupported platform)', () => {
    const watcher = bindFocusRefresh({
      addEventListener: () => { throw new Error('not supported'); },
      removeEventListener: () => {},
      onRefresh: () => {},
    });
    expect(watcher).toBeNull();
  });
});

describe('bindAtlasInterval', () => {
  it('schedules an interval and fires onRefresh only when visible', () => {
    let tick = null;
    const setIntervalFake = vi.fn((fn, _ms) => { tick = fn; return 42; });
    const clearIntervalFake = vi.fn();
    let state = 'visible';
    const onRefresh = vi.fn();
    const watcher = bindAtlasInterval({
      setInterval: setIntervalFake,
      clearInterval: clearIntervalFake,
      getVisibilityState: () => state,
      onRefresh,
    }, 90_000);
    expect(watcher).not.toBeNull();
    expect(setIntervalFake).toHaveBeenCalledWith(expect.any(Function), 90_000);

    // Visible tick: fires.
    tick();
    expect(onRefresh).toHaveBeenCalledTimes(1);

    // Hidden tick: skipped — refreshing a hidden tab wastes GPU and
    // fights browser throttling.
    state = 'hidden';
    tick();
    expect(onRefresh).toHaveBeenCalledTimes(1);

    // Prerender tick: skipped.
    state = 'prerender';
    tick();
    expect(onRefresh).toHaveBeenCalledTimes(1);

    // Back to visible: fires again.
    state = 'visible';
    tick();
    expect(onRefresh).toHaveBeenCalledTimes(2);
  });

  it('teardown clears the interval and is idempotent', () => {
    const clearIntervalFake = vi.fn();
    const watcher = bindAtlasInterval({
      setInterval: () => 99,
      clearInterval: clearIntervalFake,
      getVisibilityState: () => 'visible',
      onRefresh: () => {},
    }, 1_000);
    watcher.teardown();
    expect(clearIntervalFake).toHaveBeenCalledWith(99);
    // Idempotent: teardown a second time is a no-op.
    watcher.teardown();
    expect(clearIntervalFake).toHaveBeenCalledTimes(1);
  });

  it('swallows a throwing onRefresh so future ticks still fire', () => {
    let tick = null;
    const onRefresh = vi.fn(() => { throw new Error('refresh blew up'); });
    bindAtlasInterval({
      setInterval: (fn) => { tick = fn; return 1; },
      clearInterval: () => {},
      getVisibilityState: () => 'visible',
      onRefresh,
    }, 1_000);
    expect(() => tick()).not.toThrow();
    expect(() => tick()).not.toThrow();
    expect(onRefresh).toHaveBeenCalledTimes(2);
  });

  it('returns null when setInterval throws', () => {
    const watcher = bindAtlasInterval({
      setInterval: () => { throw new Error('no timers'); },
      clearInterval: () => {},
      getVisibilityState: () => 'visible',
      onRefresh: () => {},
    }, 1_000);
    expect(watcher).toBeNull();
  });
});
