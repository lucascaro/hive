// @vitest-environment jsdom
//
// The boot overlay (src/app/dom.ts). It is visible from the first
// paint and only the arrival of a session list may retire it: a black
// pane, or worse a premature "No sessions yet", is what the user saw
// when the daemon was merely slow to come up.
import { describe, it, expect, beforeEach } from 'vitest';

async function loadDom() {
  document.body.innerHTML = `
    <div id="terms"></div>
    <ul id="projects"></ul>
    <div id="status"></div>
    <div id="boot-state">
      <div class="boot-state-card">
        <span class="phase-spinner"></span>
        <span id="boot-state-text">Starting hive…</span>
        <button id="boot-state-retry" class="hidden" type="button">Retry</button>
      </div>
    </div>
  `;
  return await import('../../src/app/dom.js');
}

describe('boot overlay', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
  });

  it('shows the given message', async () => {
    const { setBootState } = await loadDom();
    setBootState('Waiting for the hive daemon…');
    const el = document.getElementById('boot-state') as HTMLElement;
    expect(el.classList.contains('hidden')).toBe(false);
    expect(document.getElementById('boot-state-text')?.textContent).toBe(
      'Waiting for the hive daemon…',
    );
  });

  it('hides on null', async () => {
    const { setBootState } = await loadDom();
    setBootState(null);
    const el = document.getElementById('boot-state') as HTMLElement;
    expect(el.classList.contains('hidden')).toBe(true);
  });

  it('offers a retry instead of a spinner once it gives up', async () => {
    const { setBootState } = await loadDom();
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
    expect(retry.classList.contains('hidden')).toBe(false);
    expect(spinner.classList.contains('hidden')).toBe(true);
    retry.click();
    expect(clicked).toBe(1);

    // And a plain wait puts the spinner back, retry gone.
    setBootState('Waiting for the hive daemon…');
    expect(retry.classList.contains('hidden')).toBe(true);
    expect(spinner.classList.contains('hidden')).toBe(false);
  });

  it('is inert when the markup is absent', async () => {
    const { setBootState } = await loadDom();
    document.getElementById('boot-state')?.remove();
    expect(() => setBootState('anything')).not.toThrow();
  });
});
