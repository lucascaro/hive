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
    <div id="boot-state"><span id="boot-state-text">Starting hive…</span></div>
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

  it('is inert when the markup is absent', async () => {
    const { setBootState } = await loadDom();
    document.getElementById('boot-state')?.remove();
    expect(() => setBootState('anything')).not.toThrow();
  });
});
