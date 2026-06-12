import { describe, it, expect, beforeEach } from 'vitest';
import {
  createStatus, FLASH_ERROR_MS, FLASH_INFO_MS, FLASH_MIN_MS,
} from '../../src/lib/status.js';

// Manual timer/clock harness — createStatus takes injected timers, so
// no vi.useFakeTimers needed and the semantics are explicit.
function harness() {
  const rendered = []; // [{ text, isError }]
  let clock = 0;
  let nextId = 1;
  const timers = new Map(); // id -> { at, fn }
  const ctl = createStatus({
    render: (text, isError) => rendered.push({ text, isError }),
    setTimer: (fn, ms) => {
      const id = nextId++;
      timers.set(id, { at: clock + ms, fn });
      return id;
    },
    clearTimer: (id) => timers.delete(id),
    now: () => clock,
  });
  const advance = (ms) => {
    clock += ms;
    for (const [id, t] of [...timers]) {
      if (t.at <= clock) {
        timers.delete(id);
        t.fn();
      }
    }
  };
  const last = () => rendered[rendered.length - 1];
  return { ctl, advance, last, rendered };
}

describe('createStatus', () => {
  let h;
  beforeEach(() => { h = harness(); });

  it('set renders immediately when no flash is active', () => {
    h.ctl.set('connected');
    expect(h.last()).toEqual({ text: 'connected', isError: false });
  });

  it('error flash renders, then reverts to the persistent slot after FLASH_ERROR_MS', () => {
    h.ctl.set('connected');
    h.ctl.flash('copy failed: boom', true);
    expect(h.last()).toEqual({ text: 'copy failed: boom', isError: true });
    h.advance(FLASH_ERROR_MS - 1);
    expect(h.last()).toEqual({ text: 'copy failed: boom', isError: true });
    h.advance(1);
    expect(h.last()).toEqual({ text: 'connected', isError: false });
  });

  it('info flash reverts after the shorter FLASH_INFO_MS', () => {
    h.ctl.set('connected');
    h.ctl.flash('creating session…');
    h.advance(FLASH_INFO_MS);
    expect(h.last()).toEqual({ text: 'connected', isError: false });
  });

  it('set during a young flash is deferred, then rendered at flash expiry', () => {
    h.ctl.flash('close failed: boom', true);
    h.advance(FLASH_MIN_MS - 1);
    h.ctl.set('session two'); // nav feedback must not wipe a fresh error
    expect(h.last()).toEqual({ text: 'close failed: boom', isError: true });
    h.advance(FLASH_ERROR_MS - (FLASH_MIN_MS - 1));
    expect(h.last()).toEqual({ text: 'session two', isError: false });
  });

  it('set after FLASH_MIN_MS replaces the flash immediately', () => {
    h.ctl.flash('close failed: boom', true);
    h.advance(FLASH_MIN_MS);
    h.ctl.set('session two');
    expect(h.last()).toEqual({ text: 'session two', isError: false });
    // The flash timer was cancelled — nothing re-renders later.
    const renders = h.rendered.length;
    h.advance(FLASH_ERROR_MS * 2);
    expect(h.rendered.length).toBe(renders);
  });

  it('a newer flash replaces an active one and resets the revert timer', () => {
    h.ctl.set('connected');
    h.ctl.flash('first failed', true);
    h.advance(FLASH_ERROR_MS - 100);
    h.ctl.flash('second failed', true);
    expect(h.last()).toEqual({ text: 'second failed', isError: true });
    h.advance(200); // first flash's deadline passes — must not revert yet
    expect(h.last()).toEqual({ text: 'second failed', isError: true });
    h.advance(FLASH_ERROR_MS);
    expect(h.last()).toEqual({ text: 'connected', isError: false });
  });

  it('persistent error state survives an info flash', () => {
    h.ctl.set('control disconnected', true);
    h.ctl.flash('font 14px');
    h.advance(FLASH_INFO_MS);
    expect(h.last()).toEqual({ text: 'control disconnected', isError: true });
  });
});
