// @vitest-environment jsdom
//
// Spec 477, criteria 1 + 2: after a keymap change — no reload — the new
// key runs the command, the old one does not, and every surface that
// shows the shortcut shows the new key and never the old one.
import { act, render } from '@testing-library/react';
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { chordLabel } from '../../src/lib/chord-label.js';
import { isMac } from '../../src/lib/platform.js';
import { modeHints } from '../../src/lib/status.js';
import { emptyStateModel } from '../../src/lib/empty-state.js';
import {
  openModal,
  resetStore,
  setKeymap,
  setPluginUI,
  type Keymap,
} from '../../src/store/store.js';
import { matchBinding, scopeById } from '../../src/app/key-scopes.js';
import { paletteCommands } from '../../src/app/modals/command-palette.js';

const half = isMac ? 'mac' : 'other';
const km = (o: Record<string, string[]>): Keymap => ({ [half]: o });
const NEW = chordLabel('Mod+Y', isMac);
const OLD = chordLabel('Mod+T', isMac);

const press = (key: string, o: Partial<KeyboardEvent> = {}) =>
  new KeyboardEvent('keydown', {
    key,
    code: /^[a-z]$/i.test(key) ? `Key${key.toUpperCase()}` : key,
    [isMac ? 'metaKey' : 'ctrlKey']: true,
    ...o,
  });
const runs = (scope: string, e: KeyboardEvent) =>
  matchBinding(scopeById(scope), e, isMac)?.command;

const MARKUP = `
  <div id="app"><div id="help-overlay" class="hv-dialog hidden" role="dialog"
    aria-modal="true" aria-labelledby="help-overlay-title"></div></div>`;

let HelpOverlay: typeof import('../../src/components/modals/HelpOverlay.js')['HelpOverlay'];
let openHelpOverlay: () => void;

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  ({ HelpOverlay } = await import(
    '../../src/components/modals/HelpOverlay.js'
  ));
  const m = await import('../../src/app/modals/help-overlay.js');
  m.initHelpOverlay({ setFocusedTile: vi.fn(), focusActiveTerm: vi.fn() });
  openHelpOverlay = m.openHelpOverlay;
  // A palette row for the command, as app/commands.ts registers it.
  const { registerCommandSource } = await import(
    '../../src/app/command-registry.js'
  );
  registerCommandSource(
    () => [{ id: 'new-session', title: 'New Session', run: () => {} }],
    'core',
  );
});

beforeEach(() => {
  resetStore();
});

describe('dispatch follows the keymap', () => {
  it('the new key runs the command and the old one does not', () => {
    expect(runs('app', press('t'))).toBe('new-session');
    setKeymap(km({ 'new-session': ['Mod+Y'] }));
    expect(runs('app', press('y'))).toBe('new-session');
    expect(runs('app', press('t'))).toBeUndefined();
  });
  it('a modal closes on the key its opener moved to', () => {
    setKeymap(km({ worktrees: ['Mod+Shift+Y'] }));
    openModal({ id: 'worktrees', projectId: 'p', projectName: 'P' });
    expect(runs('worktrees', press('Y', { shiftKey: true }))).toBe(
      'worktrees.close',
    );
    expect(runs('worktrees', press('e'))).toBeUndefined();
  });
  it('a plugin command follows its override', () => {
    setPluginUI('p', {
      status: 'active',
      contrib: {
        commands: [
          {
            id: 'go',
            title: 'Go',
            run: () => {},
            keys: { key: 'o', shift: true },
          },
        ],
      },
    });
    expect(runs('plugins', press('O', { shiftKey: true, code: 'KeyO' }))).toBe(
      'plugin:p:go',
    );
    setKeymap(km({ 'plugin:p:go': ['Mod+Alt+O'] }));
    expect(
      runs('plugins', press('O', { shiftKey: true, code: 'KeyO' })),
    ).toBeUndefined();
    expect(runs('plugins', press('o', { altKey: true, code: 'KeyO' }))).toBe(
      'plugin:p:go',
    );
  });
});

describe('every surface shows the new key', () => {
  it('command palette', () => {
    const row = () => paletteCommands().find((c) => c.id === 'new-session');
    expect(row()?.shortcut).toBe(OLD);
    setKeymap(km({ 'new-session': ['Mod+Y'] }));
    expect(row()?.shortcut).toBe(NEW);
  });
  it('help overlay, while it is open', () => {
    const root = document.getElementById('help-overlay') as HTMLElement;
    render(<HelpOverlay root={root} />, { container: root });
    act(() => openHelpOverlay());
    const row = () =>
      [...root.querySelectorAll('dt, kbd, .hv-kbd')].map((n) => n.textContent);
    expect(row()).toContain(OLD);
    act(() => setKeymap(km({ 'new-session': ['Mod+Y'] })));
    expect(row()).toContain(NEW);
    expect(row()).not.toContain(OLD);
  });
  it('status bar hints', () => {
    const k = km({ 'toggle-project-grid': ['Mod+Y'] });
    expect(modeHints('single', isMac, k)[0]).toEqual({
      key: NEW,
      label: 'grid',
    });
    // The four-command "move" hint has no short form once one moves.
    const moved = km({ 'next-session': ['Mod+Y'] });
    expect(modeHints('grid-all', isMac, moved).map((h) => h.label)).toEqual([
      'focus',
    ]);
  });
  it('empty state', () => {
    const m = emptyStateModel({
      isMac,
      keymap: km({ 'new-session': ['Mod+Y'] }),
    });
    expect(m?.hint).toContain(NEW);
    expect(m?.hint).not.toContain(OLD);
    expect(m?.actions[0].label).toBe(`New session (${NEW})`);
    const none = emptyStateModel({ isMac, keymap: km({ 'new-session': [] }) });
    expect(none?.actions[0].label).toBe('New session');
  });
});
