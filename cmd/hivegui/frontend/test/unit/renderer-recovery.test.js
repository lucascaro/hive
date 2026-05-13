import { describe, it, expect, vi } from 'vitest';
import {
  shouldRefreshOnVisibility,
  recoverFromContextLoss,
} from '../../src/lib/renderer-recovery.js';

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
