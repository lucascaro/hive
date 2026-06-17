import { describe, it, expect } from 'vitest';
import { wheelToScrollLines, shouldScrollViewport } from '../../src/lib/wheel-scroll.js';

// The GUI takes over wheel handling (capture phase + preventDefault) so
// EVERY wheel scroll flows through this math — if it returns 0, the
// terminal cannot scroll at all. These cases pin the cross-platform
// normalization that keeps that from happening on WebKit builds (the
// system WKWebView version follows the macOS version) that deliver
// wheel events differently than the developer's main machine.
const OPTS = { linesPerPixel: 1 / 14, maxLinesPerEvent: 8 };

describe('wheelToScrollLines', () => {
  describe('pixel deltas (deltaMode 0 — the working-machine path)', () => {
    it('converts a pixel delta to a rounded line count', () => {
      expect(wheelToScrollLines({ deltaY: 28, deltaMode: 0 }, OPTS)).toBe(2);
    });

    it('preserves direction for sub-line deltas (rounds to ±1, never 0)', () => {
      expect(wheelToScrollLines({ deltaY: 5, deltaMode: 0 }, OPTS)).toBe(1);
      expect(wheelToScrollLines({ deltaY: -5, deltaMode: 0 }, OPTS)).toBe(-1);
    });

    it('caps a momentum-sized delta at maxLinesPerEvent', () => {
      expect(wheelToScrollLines({ deltaY: 400, deltaMode: 0 }, OPTS)).toBe(8);
      expect(wheelToScrollLines({ deltaY: -400, deltaMode: 0 }, OPTS)).toBe(-8);
    });
  });

  describe('line deltas (deltaMode 1 — the unscrollable-machine bug)', () => {
    // A mouse wheel under some WebKit builds reports deltaMode 1 with a
    // small line count (e.g. 3). The old pixel math did round(3 / 14) = 0
    // and the terminal would not scroll at all. deltaY here is ALREADY a
    // line count, so it must move that many lines.
    it('treats deltaY as a line count, not pixels', () => {
      expect(wheelToScrollLines({ deltaY: 3, deltaMode: 1 }, OPTS)).toBe(3);
      expect(wheelToScrollLines({ deltaY: -1, deltaMode: 1 }, OPTS)).toBe(-1);
    });

    it('still caps a large line delta', () => {
      expect(wheelToScrollLines({ deltaY: 50, deltaMode: 1 }, OPTS)).toBe(8);
    });
  });

  describe('page deltas (deltaMode 2)', () => {
    it('scrolls a capped amount in the wheel direction', () => {
      expect(wheelToScrollLines({ deltaY: 1, deltaMode: 2 }, OPTS)).toBe(8);
      expect(wheelToScrollLines({ deltaY: -1, deltaMode: 2 }, OPTS)).toBe(-8);
    });
  });

  describe('legacy wheelDeltaY fallback', () => {
    // Some WebKit builds leave the standard deltaY at 0 but populate the
    // deprecated wheelDeltaY (opposite sign, pixel-scale). Without this
    // fallback the wheel is a complete no-op on those machines.
    it('uses wheelDeltaY (opposite sign) when deltaY is 0', () => {
      // wheelDeltaY +120 means scroll UP → negative line count.
      expect(wheelToScrollLines({ deltaY: 0, deltaMode: 0, wheelDeltaY: 120 }, OPTS)).toBe(-8);
      expect(wheelToScrollLines({ deltaY: 0, deltaMode: 0, wheelDeltaY: -120 }, OPTS)).toBe(8);
    });

    it('prefers the standard deltaY when it is usable', () => {
      expect(wheelToScrollLines({ deltaY: 28, deltaMode: 0, wheelDeltaY: 120 }, OPTS)).toBe(2);
    });
  });

  describe('no-op guards', () => {
    it('returns 0 for a zero delta with no legacy fallback', () => {
      expect(wheelToScrollLines({ deltaY: 0, deltaMode: 0 }, OPTS)).toBe(0);
    });

    it('returns 0 for a non-finite delta (never scrollLines(NaN))', () => {
      expect(wheelToScrollLines({ deltaY: undefined, deltaMode: 0 }, OPTS)).toBe(0);
      expect(wheelToScrollLines({ deltaY: NaN, deltaMode: 0 }, OPTS)).toBe(0);
    });
  });
});

describe('shouldScrollViewport', () => {
  // The takeover is correct ONLY in the normal buffer with mouse reporting
  // off. Everywhere else the wheel must reach xterm/the app — this is the
  // "scrolls in pi, not in Claude" regression.
  it('takes over in the normal buffer with mouse reporting off (pi / plain shell)', () => {
    expect(shouldScrollViewport({ bufferType: 'normal', mouseTrackingMode: 'none' })).toBe(true);
    expect(shouldScrollViewport({ bufferType: 'normal', mouseTrackingMode: undefined })).toBe(true);
  });

  it('does NOT take over in the alternate buffer (full-screen TUIs)', () => {
    expect(shouldScrollViewport({ bufferType: 'alternate', mouseTrackingMode: 'none' })).toBe(false);
  });

  it('does NOT take over when mouse tracking is active (Claude, vim)', () => {
    expect(shouldScrollViewport({ bufferType: 'normal', mouseTrackingMode: 'vt200' })).toBe(false);
    expect(shouldScrollViewport({ bufferType: 'normal', mouseTrackingMode: 'any' })).toBe(false);
    expect(shouldScrollViewport({ bufferType: 'alternate', mouseTrackingMode: 'any' })).toBe(false);
  });
});
