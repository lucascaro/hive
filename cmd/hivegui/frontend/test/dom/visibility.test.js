// @vitest-environment jsdom
import { describe, it, expect } from 'vitest';
import { shouldFitTerminal } from '../../src/lib/visibility.js';

describe('shouldFitTerminal — visibility-gate for xterm fit()', () => {
  it('fits a visible non-zero tile', () => {
    expect(shouldFitTerminal({ visible: true, width: 800, height: 400 })).toBe(true);
  });
  it('does not fit a hidden tile', () => {
    expect(shouldFitTerminal({ visible: false, width: 800, height: 400 })).toBe(false);
  });
  it('does not fit a zero-size tile (the canvas-resize regression)', () => {
    expect(shouldFitTerminal({ visible: true, width: 0, height: 400 })).toBe(false);
    expect(shouldFitTerminal({ visible: true, width: 800, height: 0 })).toBe(false);
  });
  it('rejects NaN / Infinity', () => {
    expect(shouldFitTerminal({ visible: true, width: NaN, height: 400 })).toBe(false);
    expect(shouldFitTerminal({ visible: true, width: Infinity, height: 400 })).toBe(false);
  });

  it('integration: reads visibility flags from a real DOM node', () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    // jsdom doesn't lay out, but we can stub the relevant properties.
    Object.defineProperty(host, 'clientWidth', { value: 800 });
    Object.defineProperty(host, 'clientHeight', { value: 600 });
    host.style.display = 'block';
    const snapshot = {
      visible: host.style.display !== 'none',
      width: host.clientWidth,
      height: host.clientHeight,
    };
    expect(shouldFitTerminal(snapshot)).toBe(true);

    host.style.display = 'none';
    snapshot.visible = host.style.display !== 'none';
    expect(shouldFitTerminal(snapshot)).toBe(false);
  });
});
