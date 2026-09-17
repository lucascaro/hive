import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createAttentionDwell } from '../../src/app/attention-dwell.js';

function setup() {
  const world = {
    active: 'pi-1' as string | null,
    focused: true,
    wants: new Set(['pi-1']),
  };
  const clear = vi.fn();
  const dwell = createAttentionDwell({
    delayMs: 3000,
    activeId: () => world.active,
    hasFocus: () => world.focused,
    eligible: (id) => world.wants.has(id),
    clear,
    setTimer: (fn, ms) => setTimeout(fn, ms),
    clearTimer: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
  });
  return { world, clear, dwell };
}

describe('createAttentionDwell', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('clears after the delay when the active session wants attention and the window has focus', () => {
    const { clear, dwell } = setup();
    dwell.poke();
    vi.advanceTimersByTime(2999);
    expect(clear).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(clear).toHaveBeenCalledWith('pi-1');
  });

  it('does nothing without focus', () => {
    const { world, clear, dwell } = setup();
    world.focused = false;
    dwell.poke();
    vi.advanceTimersByTime(10_000);
    expect(clear).not.toHaveBeenCalled();
  });

  it('does nothing for a session that is not eligible (no attention, or not Pi)', () => {
    const { world, clear, dwell } = setup();
    world.wants.clear();
    dwell.poke();
    vi.advanceTimersByTime(10_000);
    expect(clear).not.toHaveBeenCalled();
  });

  it('cancel (window blur) before the delay prevents the clear', () => {
    const { clear, dwell } = setup();
    dwell.poke();
    vi.advanceTimersByTime(1000);
    dwell.cancel();
    vi.advanceTimersByTime(10_000);
    expect(clear).not.toHaveBeenCalled();
  });

  it('a switch before firing prevents the clear', () => {
    const { world, clear, dwell } = setup();
    dwell.poke();
    vi.advanceTimersByTime(1000);
    world.active = 'other';
    vi.advanceTimersByTime(10_000);
    expect(clear).not.toHaveBeenCalled();
  });

  it('a repeat poke for the same session does not restart the delay', () => {
    const { clear, dwell } = setup();
    dwell.poke();
    vi.advanceTimersByTime(2000);
    dwell.poke();
    vi.advanceTimersByTime(1000);
    expect(clear).toHaveBeenCalledTimes(1);
  });

  it('an answer before firing cancels, and a new request waits the full delay', () => {
    const { world, clear, dwell } = setup();
    dwell.poke();
    vi.advanceTimersByTime(2000);
    world.wants.clear(); // the user answered
    dwell.poke();
    vi.advanceTimersByTime(900);
    world.wants.add('pi-1'); // asked again
    dwell.poke();
    vi.advanceTimersByTime(2999);
    expect(clear).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(clear).toHaveBeenCalledTimes(1);
  });
});
