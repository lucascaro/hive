import { describe, it, expect } from 'vitest';
import { detectMac, cmdOrCtrl } from '../../src/lib/platform.js';

describe('detectMac', () => {
  it('matches macOS user agents', () => {
    expect(detectMac({ platform: 'MacIntel' })).toBe(true);
    expect(detectMac({ platform: 'iPhone' })).toBe(true);
    expect(detectMac({ userAgentData: { platform: 'macOS' } })).toBe(true);
  });
  it('rejects non-mac platforms', () => {
    expect(detectMac({ platform: 'Win32' })).toBe(false);
    expect(detectMac({ platform: 'Linux x86_64' })).toBe(false);
    expect(detectMac(null)).toBe(false);
    expect(detectMac({})).toBe(false);
  });
});

describe('cmdOrCtrl', () => {
  it('on mac fires for Cmd but not Ctrl', () => {
    expect(cmdOrCtrl({ metaKey: true, ctrlKey: false }, true)).toBe(true);
    expect(cmdOrCtrl({ metaKey: false, ctrlKey: true }, true)).toBe(false);
  });
  it('on PC fires for Ctrl but not Cmd', () => {
    expect(cmdOrCtrl({ metaKey: false, ctrlKey: true }, false)).toBe(true);
    expect(cmdOrCtrl({ metaKey: true, ctrlKey: false }, false)).toBe(false);
  });
  it('rejects the cross-platform combo (both modifiers down)', () => {
    // Prevents accidental firing when both modifiers are held.
    expect(cmdOrCtrl({ metaKey: true, ctrlKey: true }, true)).toBe(false);
    expect(cmdOrCtrl({ metaKey: true, ctrlKey: true }, false)).toBe(false);
  });
});
