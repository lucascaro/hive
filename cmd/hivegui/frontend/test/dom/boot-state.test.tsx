// @vitest-environment jsdom
//
// The boot overlay (src/components/BootState.tsx, mounted on #boot-state
// exactly as index.html's markup does). It is visible from the first
// paint and only the arrival of a session list may retire it: a black
// pane, or worse a premature "No sessions yet", is what the user saw
// when the daemon was merely slow to come up.
//
// Driven through app/dom.ts's setBootState — that seam is what main.ts
// and every retry path still call, and it must keep forwarding to the
// store unchanged.
import { describe, it, expect, beforeAll, beforeEach } from 'vitest';
import { act, render } from '@testing-library/react';
import { BootState } from '../../src/components/BootState.js';
import { resetStore } from '../../src/store/store.js';

let domSetBootState: typeof import('../../src/app/dom.js').setBootState;
let root: HTMLElement;

// The store write happens outside a React event handler, so it must be
// wrapped in act() for the subscriber (BootState's useAppStore) to flush
// synchronously before the assertion that follows.
function setBootState(...args: Parameters<typeof domSetBootState>): void {
  act(() => domSetBootState(...args));
}

beforeAll(async () => {
  // app/dom.ts owns #terms at module scope (mustEl throws if it is
  // missing) — this seam pulls that module in even though this file
  // never touches terminals. Kept out of #app on purpose: RTL's
  // cleanup() removes any container whose parentNode is document.body,
  // and #terms must survive every test since dom.js only sets it up
  // once.
  document.body.innerHTML = '<div id="terms"></div>';
  ({ setBootState: domSetBootState } = await import('../../src/app/dom.js'));
});

beforeEach(() => {
  resetStore();
  // A fresh container per test, nested one level under document.body:
  // RTL's cleanup() removes a render() container whose parentNode IS
  // document.body, which would rip #boot-state out of the document
  // between tests and turn every later document.getElementById lookup
  // into a silent null. Wrapping it in #app (removed and rebuilt every
  // test, so nothing leaks) sidesteps that.
  document.getElementById('app')?.remove();
  const app = document.createElement('div');
  app.id = 'app';
  document.body.appendChild(app);
  root = document.createElement('div');
  root.id = 'boot-state';
  app.appendChild(root);
  render(<BootState root={root} />, { container: root });
});

describe('boot overlay', () => {
  it('shows the given message', () => {
    setBootState('Waiting for the hive daemon…');
    expect(root.classList.contains('hidden')).toBe(false);
    expect(document.getElementById('boot-state-text')?.textContent).toBe(
      'Waiting for the hive daemon…',
    );
  });

  it('hides on null', () => {
    setBootState(null);
    expect(root.classList.contains('hidden')).toBe(true);
  });

  it('offers a retry instead of a spinner once it gives up', () => {
    let clicked = 0;
    setBootState('Could not reach the hive daemon.', {
      retry: () => {
        clicked += 1;
      },
    });
    const retry = document.getElementById(
      'boot-state-retry',
    ) as HTMLButtonElement;
    const spinner = document.querySelector('.phase-spinner') as HTMLElement;
    expect(retry.hidden).toBe(false);
    expect(spinner.classList.contains('hidden')).toBe(true);
    retry.click();
    expect(clicked).toBe(1);

    // And a plain wait puts the spinner back. The retry button is not
    // merely hidden here — a card offering Retry has stopped waiting, so
    // BootState unmounts it entirely once onRetry goes away.
    setBootState('Waiting for the hive daemon…');
    expect(document.getElementById('boot-state-retry')).toBeNull();
    expect(spinner.classList.contains('hidden')).toBe(false);
  });

  // The old imperative setBootState touched the DOM directly and had to
  // tolerate #boot-state being absent (a load-order race). It no longer
  // does: this seam is now a pure store write, and the component (if
  // mounted) reacts to it — so "does not throw with no markup" is no
  // longer a meaningful case to port; there is nothing left for the
  // absence of markup to break.
});
