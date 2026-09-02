// @vitest-environment jsdom
//
// The status bar (src/components/StatusBar.tsx, mounted on #status).
// Rendering is React's job here; the flash/persistent arbitration is
// NOT — lib/status.ts's createStatus still owns FLASH_MIN_MS and the
// guarantee that a set() during an active flash is never lost. These
// tests go through app/dom.ts's setStatus/flashStatus/setModeHint, the
// seam every call site in app/ still imports.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { act, render } from '@testing-library/react';
import { StatusBar } from '../../src/components/StatusBar.js';
import { resetStore } from '../../src/store/store.js';

let domSetStatus: typeof import('../../src/app/dom.js').setStatus;
let domFlashStatus: typeof import('../../src/app/dom.js').flashStatus;
let domSetModeHint: typeof import('../../src/app/dom.js').setModeHint;
let root: HTMLElement;

// Each of these writes the store outside a React event handler, so it
// must be wrapped in act() for StatusBar's useAppStore subscription to
// flush synchronously before the assertion that follows.
function setStatus(...args: Parameters<typeof domSetStatus>): void {
  act(() => domSetStatus(...args));
}
function flashStatus(...args: Parameters<typeof domFlashStatus>): void {
  act(() => domFlashStatus(...args));
}
function setModeHint(...args: Parameters<typeof domSetModeHint>): void {
  act(() => domSetModeHint(...args));
}

beforeAll(async () => {
  // app/dom.ts owns #terms at module scope (mustEl throws if it is
  // missing) — this seam pulls that module in even though this file
  // never touches terminals.
  document.body.innerHTML = '<div id="terms"></div>';
  ({
    setStatus: domSetStatus,
    flashStatus: domFlashStatus,
    setModeHint: domSetModeHint,
  } = await import('../../src/app/dom.js'));
});

beforeEach(() => {
  resetStore();
  // A fresh container per test, nested one level under document.body:
  // RTL's cleanup() removes a render() container whose parentNode IS
  // document.body, which would rip #status out of the document between
  // tests and turn every later document.getElementById lookup into a
  // silent null. Wrapping it in #app (removed and rebuilt every test,
  // so nothing leaks) sidesteps that.
  document.getElementById('app')?.remove();
  const app = document.createElement('div');
  app.id = 'app';
  document.body.appendChild(app);
  root = document.createElement('div');
  root.id = 'status';
  app.appendChild(root);
  render(<StatusBar root={root} />, { container: root });
});

describe('status bar', () => {
  it('renders setStatus into #status-text', () => {
    setStatus('connected');
    expect(document.getElementById('status-text')?.textContent).toBe(
      'connected',
    );
  });

  it('puts .error on the container for an error status and clears it on a non-error one', () => {
    setStatus('control disconnected', true);
    expect(root.classList.contains('error')).toBe(true);

    setStatus('connected');
    expect(root.classList.contains('error')).toBe(false);
  });

  it('flashStatus shows the transient text and auto-reverts to the persistent one when its timer expires', () => {
    vi.useFakeTimers();
    try {
      setStatus('connected');
      flashStatus('copied');
      expect(document.getElementById('status-text')?.textContent).toBe(
        'copied',
      );

      // Info flashes revert after FLASH_INFO_MS (2500ms).
      act(() => vi.advanceTimersByTime(2500));
      expect(document.getElementById('status-text')?.textContent).toBe(
        'connected',
      );
    } finally {
      vi.useRealTimers();
    }
  });

  it('does not lose a setStatus during an active flash — it lands after the flash ends', () => {
    // This is the guarantee lib/status.ts exists for: an error flash
    // isn't wiped by nav feedback landing a frame later, but a set()
    // must not be lost either — it renders once the flash expires.
    vi.useFakeTimers();
    try {
      setStatus('connected');
      flashStatus('creating session…');
      setStatus('session s1');
      // The flash is still on screen — set() did not overwrite it.
      expect(document.getElementById('status-text')?.textContent).toBe(
        'creating session…',
      );

      act(() => vi.advanceTimersByTime(2500));
      expect(document.getElementById('status-text')?.textContent).toBe(
        'session s1',
      );
    } finally {
      vi.useRealTimers();
    }
  });

  it('setModeHint renders kbd + label pairs into #status-hint and replaces them wholesale', () => {
    setModeHint([
      { key: '⌘G', label: 'grid' },
      { key: '⇧⌘K', label: 'actions' },
    ]);
    let hint = document.getElementById('status-hint') as HTMLElement;
    let kbds = hint.querySelectorAll('kbd.hv-kbd');
    let labels = hint.querySelectorAll('.hv-status__hint-label');
    expect(Array.from(kbds).map((k) => k.textContent)).toEqual(['⌘G', '⇧⌘K']);
    expect(Array.from(labels).map((l) => l.textContent)).toEqual([
      'grid',
      'actions',
    ]);

    setModeHint([{ key: '⌘↑↓←→', label: 'move' }]);
    hint = document.getElementById('status-hint') as HTMLElement;
    kbds = hint.querySelectorAll('kbd.hv-kbd');
    labels = hint.querySelectorAll('.hv-status__hint-label');
    expect(Array.from(kbds).map((k) => k.textContent)).toEqual(['⌘↑↓←→']);
    expect(Array.from(labels).map((l) => l.textContent)).toEqual(['move']);
  });
});
