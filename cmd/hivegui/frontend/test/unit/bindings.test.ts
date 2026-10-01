// Spec 477: the user's keymap over the shipped bindings.
import { describe, expect, it } from 'vitest';
import {
  DEFAULT_APP_BINDINGS,
  defaultMenuAccelerators,
  effectiveBindings,
  effectiveFor,
  isDefaultIn,
  labelIn,
  menuAcceleratorOverrides,
  shortcutsIn,
  type Keymap,
} from '../../src/lib/bindings.js';
import { chordMatches, chordsFor, parseChord } from '../../src/lib/chord.js';
// Shared with menu_darwin_test.go: the menu's hard-coded defaults.
import menuDefaults from '../../../testdata/menu-default-accelerators.json';

const key = (k: string, o: Partial<KeyboardEvent> = {}) => ({
  key: k,
  code: '',
  metaKey: false,
  ctrlKey: false,
  altKey: false,
  shiftKey: false,
  ...o,
});

/** The command a key runs under a keymap, first match wins. */
function runs(keymap: Keymap, e: ReturnType<typeof key>, isMac: boolean) {
  const b = effectiveFor(keymap, isMac).bindings.find((x) =>
    chordsFor(x.keys, isMac).some((c) => chordMatches(parseChord(c, isMac), e)),
  );
  return b?.command;
}

describe('effectiveBindings', () => {
  it('an override replaces the default: new key runs it, old key does not', () => {
    const km: Keymap = { mac: { 'new-session': ['Mod+Y'] } };
    expect(runs(km, key('y', { metaKey: true }), true)).toBe('new-session');
    expect(runs(km, key('t', { metaKey: true }), true)).toBeUndefined();
    expect(labelIn(km, 'new-session', true)).toBe('⌘Y');
  });
  it('[] leaves a command with no shortcut', () => {
    const km: Keymap = { mac: { 'new-session': [] } };
    expect(runs(km, key('t', { metaKey: true }), true)).toBeUndefined();
    expect(shortcutsIn(km, 'new-session', true)).toEqual([]);
    expect(labelIn(km, 'new-session', true)).toBe('');
  });
  it('several shortcuts all run it, and all show', () => {
    const km: Keymap = { other: { settings: ['Ctrl+Alt+S', 'Ctrl+;'] } };
    expect(runs(km, key('s', { ctrlKey: true, altKey: true }), false)).toBe(
      'settings',
    );
    expect(runs(km, key(';', { ctrlKey: true }), false)).toBe('settings');
    expect(labelIn(km, 'settings', false)).toBe('Ctrl+Alt+S / Ctrl+;');
  });
  it('only the current platform half applies', () => {
    const km: Keymap = { mac: { 'new-session': ['Mod+Y'] } };
    expect(runs(km, key('t', { ctrlKey: true }), false)).toBe('new-session');
    expect(isDefaultIn(km, 'new-session', false)).toBe(true);
    expect(isDefaultIn(km, 'new-session', true)).toBe(false);
  });
  it('a default overlapping a user chord leaves its command unbound — every spelling', () => {
    // mac: nav-back's defaults are ⌃- / ⌃_ / ⌃[Minus]; taking ⌃[Minus]
    // for something else must not leave ⌃- live on the same key.
    const mac = effectiveBindings(
      DEFAULT_APP_BINDINGS,
      { 'toggle-sidebar': ['Ctrl+[Minus]'] },
      true,
    );
    expect(mac.displaced).toEqual(['nav-back']);
    expect(mac.bindings.some((b) => b.command === 'nav-back')).toBe(false);
    // Off macOS Ctrl IS Mod, so the same chord collides with zoom-out.
    const other = effectiveBindings(
      DEFAULT_APP_BINDINGS,
      { 'toggle-sidebar': ['Ctrl+[Minus]'] },
      false,
    );
    expect(other.displaced).toEqual(['zoom-out']);
  });
  it('an upgrade default colliding with a custom binding ships unbound, the custom one stays', () => {
    // The user put new-project on ⌘T; ⌘T is new-session's default.
    const km: Keymap = { mac: { 'new-project': ['Mod+T'] } };
    expect(runs(km, key('t', { metaKey: true }), true)).toBe('new-project');
    expect(effectiveFor(km, true).displaced).toEqual(['new-session']);
    expect(isDefaultIn(km, 'new-session', true)).toBe(false);
    expect(labelIn(km, 'new-session', true)).toBe('');
  });
  it('ignores and reports chords that do not parse, and leaves plugin ids to the plugin host', () => {
    const eff = effectiveBindings(
      DEFAULT_APP_BINDINGS,
      { 'new-session': ['Hyper+T', 'Mod+Y'], 'plugin:x:y': ['Mod+T'] },
      true,
    );
    expect(eff.invalid).toEqual(['new-session: Hyper+T']);
    expect(eff.bindings.filter((b) => b.command === 'new-session')).toEqual([
      { keys: 'Mod+Y', command: 'new-session' },
    ]);
    // The plugin override is not an app binding and displaces nothing.
    expect(eff.displaced).toEqual([]);
  });
  it('keeps the reserved chords', () => {
    const eff = effectiveFor({}, true);
    expect(
      eff.bindings.filter((b) => b.command === null).map((b) => b.keys),
    ).toEqual(['Mod+Shift+Enter', 'Mod+Shift+Z']);
  });
  it('the empty keymap is the shipped table', () => {
    expect(effectiveFor({}, true).bindings).toEqual(DEFAULT_APP_BINDINGS);
    expect(effectiveFor({}, false).bindings).toEqual(DEFAULT_APP_BINDINGS);
  });
});

describe('native menu accelerators', () => {
  const ids = Object.keys(menuDefaults);
  it('the derived defaults equal what menu_darwin.go hard-codes', () => {
    const fixture = menuDefaults;
    // Items with no accelerator in Go (Reload GUI…) have no ⌘ binding.
    expect(defaultMenuAccelerators(ids)).toEqual(fixture);
  });
  it('an empty keymap changes no item', () => {
    expect(menuAcceleratorOverrides(ids, {})).toEqual({});
  });
  it('a rebound command sends its new accelerator', () => {
    expect(
      menuAcceleratorOverrides(ids, { mac: { 'new-session': ['Mod+Y'] } }),
    ).toEqual({ 'new-session': 'cmdorctrl+y' });
  });
  it('a ⌘ chord taken from another item clears that item', () => {
    expect(
      menuAcceleratorOverrides(ids, { mac: { 'new-project': ['Mod+T'] } }),
    ).toEqual({ 'new-project': 'cmdorctrl+t', 'new-session': '' });
  });
  it('a command moved to a non-⌘ chord loses its accelerator', () => {
    expect(
      menuAcceleratorOverrides(ids, { mac: { 'new-session': ['Ctrl+T'] } }),
    ).toEqual({ 'new-session': '' });
  });
  it('the Windows/Linux half never touches the menu', () => {
    expect(
      menuAcceleratorOverrides(ids, { other: { 'new-session': ['Mod+Y'] } }),
    ).toEqual({});
  });
});
