// Spec 477, Settings › Shortcuts: lib/keymap-edit.ts — the key-press to
// chord capture, the refusal / warning rules, and the keymap edits.
import { describe, expect, it } from 'vitest';
import {
  canonicalKeymap,
  captureChord,
  checkChord,
  reassign,
  resetHalf,
  sameKeymap,
  withShortcuts,
} from '../../src/lib/keymap-edit.js';
import { shortcutsIn } from '../../src/lib/bindings.js';

const press = (
  key: string,
  code: string,
  mods: Partial<{
    meta: boolean;
    ctrl: boolean;
    alt: boolean;
    shift: boolean;
  }> = {},
) => ({
  key,
  code,
  metaKey: !!mods.meta,
  ctrlKey: !!mods.ctrl,
  altKey: !!mods.alt,
  shiftKey: !!mods.shift,
});

const chord = (r: ReturnType<typeof captureChord>) =>
  r?.kind === 'chord' ? r.chord : r;

describe('captureChord', () => {
  it('ignores a modifier pressed on its own', () => {
    for (const key of ['Meta', 'Control', 'Alt', 'Shift']) {
      expect(
        captureChord(press(key, `${key}Left`, { meta: true }), true),
      ).toBeNull();
    }
  });

  it('writes ⌘ as Mod and ⌃ as Ctrl on macOS, in a fixed order', () => {
    expect(chord(captureChord(press('y', 'KeyY', { meta: true }), true))).toBe(
      'Mod+Y',
    );
    expect(
      chord(
        captureChord(
          press('Y', 'KeyY', { meta: true, ctrl: true, shift: true }),
          true,
        ),
      ),
    ).toBe('Mod+Ctrl+Shift+Y');
  });

  it('writes Ctrl as Mod off macOS and refuses the Windows key', () => {
    expect(chord(captureChord(press('y', 'KeyY', { ctrl: true }), false))).toBe(
      'Mod+Y',
    );
    expect(captureChord(press('y', 'KeyY', { meta: true }), false)?.kind).toBe(
      'refused',
    );
  });

  it('falls back to the physical key when ⌥ or the layout rewrites it', () => {
    // ⌥⌘K types '˚' on a US Mac; a Russian layout types 'л' on KeyK.
    expect(
      chord(captureChord(press('˚', 'KeyK', { meta: true, alt: true }), true)),
    ).toBe('Mod+Alt+[KeyK]');
    expect(chord(captureChord(press('л', 'KeyK', { meta: true }), true))).toBe(
      'Mod+[KeyK]',
    );
    // AZERTY types 'a' on KeyQ: the letter typed wins.
    expect(chord(captureChord(press('a', 'KeyQ', { meta: true }), true))).toBe(
      'Mod+A',
    );
    // ⇧1 types '!': the digit's key.
    expect(
      chord(
        captureChord(press('!', 'Digit1', { meta: true, shift: true }), true),
      ),
    ).toBe('Mod+Shift+[Digit1]');
  });

  it('captures punctuation by its physical key and named keys by name', () => {
    expect(
      chord(
        captureChord(press('?', 'Slash', { meta: true, shift: true }), true),
      ),
    ).toBe('Mod+Shift+[Slash]');
    expect(
      chord(captureChord(press('ArrowUp', 'ArrowUp', { meta: true }), true)),
    ).toBe('Mod+ArrowUp');
    expect(chord(captureChord(press('F5', 'F5'), true))).toBe('F5');
  });
});

describe('checkChord', () => {
  const kind = (c: string, mac: boolean) => checkChord(c, mac).kind;

  it('refuses OS-reserved chords', () => {
    for (const c of [
      'Mod+Q',
      'Mod+H',
      'Mod+C',
      'Mod+V',
      'Mod+[Backquote]',
      'Mod+Tab',
      'Ctrl+C',
    ])
      expect(kind(c, true), c).toBe('refused');
    for (const c of ['Mod+C', 'Alt+F4', 'Alt+Tab'])
      expect(kind(c, false), c).toBe('refused');
    expect(checkChord('Mod+Q', true)).toMatchObject({
      reason: expect.stringContaining('macOS'),
    });
  });

  it('refuses the terminal’s own chords, on both platforms', () => {
    for (const c of [
      'Shift+Enter',
      'Mod+Backspace',
      'Mod+ArrowLeft',
      'Ctrl+Shift+C',
      'Mod+Shift+Enter',
      'Mod+Shift+Z',
    ])
      expect(kind(c, true), c).toBe('refused');
    for (const c of ['Ctrl+Shift+V', 'Shift+Enter'])
      expect(kind(c, false), c).toBe('refused');
    expect(checkChord('Ctrl+Shift+C', true)).toMatchObject({
      reason: expect.stringContaining('terminal'),
    });
  });

  it('refuses a key with no modifier, except F-keys', () => {
    expect(kind('Escape', true)).toBe('refused');
    expect(kind('Enter', false)).toBe('refused');
    expect(kind('Tab', true)).toBe('refused');
    expect(kind('Shift+K', true)).toBe('refused');
    expect(kind('F5', true)).toBe('ok');
  });

  it('warns about chords programs in a session receive', () => {
    expect(kind('Ctrl+K', true)).toBe('warn');
    expect(kind('Alt+K', true)).toBe('warn');
    expect(kind('Mod+K', false)).toBe('warn'); // Ctrl+K off macOS
    expect(kind('Alt+K', false)).toBe('warn');
  });

  it('accepts ordinary app chords', () => {
    expect(kind('Mod+Y', true)).toBe('ok');
    expect(kind('Mod+Alt+[KeyK]', true)).toBe('ok');
    expect(kind('Mod+Shift+Y', false)).toBe('ok');
    expect(kind('Mod+Alt+Y', false)).toBe('ok');
  });
});

describe('editing', () => {
  const km = {
    mac: { 'new-session': ['Mod+Y'], 'plugin:gone:x': ['Mod+Shift+X'] },
    other: { worktrees: [] },
  };

  it('changes one command on one platform only', () => {
    const next = withShortcuts(km, true, 'settings', ['Mod+;']);
    expect(next.mac).toEqual({ ...km.mac, settings: ['Mod+;'] });
    expect(next.other).toBe(km.other);
    // undefined resets the command to its defaults.
    expect(withShortcuts(next, true, 'new-session', undefined).mac).toEqual({
      'plugin:gone:x': ['Mod+Shift+X'],
      settings: ['Mod+;'],
    });
  });

  it('resets one platform, keeping the other', () => {
    expect(resetHalf(km, true)).toEqual({ mac: {}, other: { worktrees: [] } });
  });

  it('reassign takes only the overlapping key from each holder', () => {
    const before = { mac: { 'zoom-in': ['Mod+Shift?+=', 'Mod+Y'] } };
    const next = reassign(before, true, 'new-session', ['Mod+T'], 'Mod+Y', [
      { id: 'zoom-in', shortcuts: ['Mod+Shift?+=', 'Mod+Y'] },
    ]);
    expect(next.mac).toEqual({
      'zoom-in': ['Mod+Shift?+='],
      'new-session': ['Mod+T', 'Mod+Y'],
    });
    expect(shortcutsIn(next, 'new-session', true)).toEqual(['Mod+T', 'Mod+Y']);
  });

  it('compares keymaps by meaning, not spelling', () => {
    expect(
      sameKeymap(
        { version: 1, other: {}, mac: { b: ['Mod+B'], a: [] } },
        { mac: { a: [], b: ['Mod+B'] } },
      ),
    ).toBe(true);
    expect(sameKeymap({ mac: { a: [] } }, {})).toBe(false);
    expect(JSON.stringify(canonicalKeymap({ mac: { b: [], a: [] } }))).toBe(
      '{"mac":{"a":[],"b":[]}}',
    );
  });
});
