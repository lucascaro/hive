// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Spec 553: a keymap chord → the macOS key code and Carbon modifier mask
// the menu bar registers as the system-wide dictation hotkey.
import { describe, expect, it } from 'vitest';
import { DEFAULT_APP_BINDINGS, chordsOfIn } from '../../src/lib/bindings.js';
import {
  CARBON_CMD,
  CARBON_CONTROL,
  CARBON_OPTION,
  CARBON_SHIFT,
  DISABLED_HOTKEY,
  carbonHotkey,
  desiredHotkey,
  sameHotkey,
} from '../../src/lib/carbon-keys.js';

describe('carbonHotkey', () => {
  it('maps the default ⌃⌥Space to kVK_Space with control|option', () => {
    expect(carbonHotkey('Ctrl+Alt+[Space]')).toEqual({
      keyCode: 49,
      modifiers: CARBON_CONTROL | CARBON_OPTION,
    });
  });

  it('treats Mod as ⌘ and maps character and code spellings alike', () => {
    expect(carbonHotkey('Mod+Shift+d')).toEqual({
      keyCode: 0x02,
      modifiers: CARBON_CMD | CARBON_SHIFT,
    });
    expect(carbonHotkey('Mod+Shift+[KeyD]')).toEqual(
      carbonHotkey('Mod+Shift+d'),
    );
    expect(carbonHotkey('Ctrl+-')?.keyCode).toBe(0x1b);
    expect(carbonHotkey('Alt+[Digit5]')?.keyCode).toBe(0x17);
    expect(carbonHotkey('Ctrl+F5')?.keyCode).toBe(0x60);
  });

  it('refuses what Carbon cannot register exactly', () => {
    expect(carbonHotkey('[Space]')).toBeNull(); // no modifier
    expect(carbonHotkey('Mod+Shift?+d')).toBeNull(); // optional modifier
    expect(carbonHotkey('Any+d')).toBeNull();
    expect(carbonHotkey('Mod+[IntlBackslash]')).toBeNull(); // unknown code
    expect(carbonHotkey('Mod+é')).toBeNull();
    expect(carbonHotkey('Bogus+x')).toBeNull(); // unparseable
  });
});

describe('desiredHotkey / sameHotkey', () => {
  it('takes the first registrable chord, or disables', () => {
    expect(desiredHotkey(['Mod+Shift?+x', 'Ctrl+Alt+[Space]'])).toEqual({
      key_code: 49,
      modifiers: CARBON_CONTROL | CARBON_OPTION,
      enabled: true,
    });
    expect(desiredHotkey([])).toEqual(DISABLED_HOTKEY);
  });

  it('compares two disabled hotkeys as equal whatever their stale fields', () => {
    expect(
      sameHotkey(DISABLED_HOTKEY, {
        key_code: 49,
        modifiers: 0x1800,
        enabled: false,
      }),
    ).toBe(true);
    expect(
      sameHotkey(DISABLED_HOTKEY, {
        key_code: 0,
        modifiers: 0,
        enabled: true,
      }),
    ).toBe(false);
  });
});

describe('the dictate-hud binding', () => {
  it('ships ⌃⌥Space on macOS and Ctrl+Shift+Space elsewhere', () => {
    expect(chordsOfIn({}, 'dictate-hud', true)).toEqual(['Ctrl+Alt+[Space]']);
    // Spec 575: Ctrl+Alt is AltGr on many PC layouts.
    expect(chordsOfIn({}, 'dictate-hud', false)).toEqual([
      'Ctrl+Shift+[Space]',
    ]);
    expect(
      DEFAULT_APP_BINDINGS.filter((b) => b.command === 'dictate-hud'),
    ).toHaveLength(1);
  });

  it('reads PC chords with mac=false: Ctrl is Control, Mod is Ctrl', () => {
    expect(desiredHotkey(chordsOfIn({}, 'dictate-hud', false), false)).toEqual({
      key_code: 0x31,
      modifiers: CARBON_CONTROL | CARBON_SHIFT,
      enabled: true,
    });
    expect(carbonHotkey('Mod+Alt+[KeyD]', false)).toEqual({
      keyCode: 0x02,
      modifiers: CARBON_CONTROL | CARBON_OPTION,
    });
  });

  it('follows a keymap override, and an unbind disables the hotkey', () => {
    const rebound = { mac: { 'dictate-hud': ['Mod+Shift+[KeyD]'] } };
    expect(desiredHotkey(chordsOfIn(rebound, 'dictate-hud', true))).toEqual({
      key_code: 0x02,
      modifiers: CARBON_CMD | CARBON_SHIFT,
      enabled: true,
    });
    const unbound = { mac: { 'dictate-hud': [] } };
    expect(desiredHotkey(chordsOfIn(unbound, 'dictate-hud', true))).toEqual(
      DISABLED_HOTKEY,
    );
  });
});
