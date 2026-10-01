// Spec 477: plugin chords resolve against core's LIVE chords (the user's
// keymap), the reserved terminal chords, and the user's plugin overrides.
import { describe, expect, it } from 'vitest';
import {
  effectiveFor,
  RESERVED_CHORDS,
  type Keymap,
} from '../../src/lib/bindings.js';
import { chordsFor } from '../../src/lib/chord.js';
import {
  resolveCommands,
  type PluginCommand,
} from '../../src/lib/plugin-api.js';

const cmd = (id: string, key?: string, shift = false): PluginCommand => ({
  id,
  title: id,
  run: () => {},
  keys: key ? { key, shift } : undefined,
});

function taken(keymap: Keymap, isMac: boolean): string[] {
  return [
    ...effectiveFor(keymap, isMac).bindings.flatMap((b) =>
      chordsFor(b.keys, isMac),
    ),
    ...RESERVED_CHORDS[isMac ? 'mac' : 'other'],
  ];
}

const resolve = (commands: PluginCommand[], keymap: Keymap, isMac: boolean) =>
  resolveCommands(
    [{ id: 'p', commands }],
    taken(keymap, isMac),
    isMac,
    () => {},
    (isMac ? keymap.mac : keymap.other) ?? {},
  );

describe('plugin chords under a keymap', () => {
  it('a core command rebound onto the plugin chord takes it from the plugin', () => {
    const out = resolve(
      [cmd('o', 'o', true)],
      { mac: { settings: ['Mod+Shift+O'] } },
      true,
    );
    expect(out[0].bound).toBe(false);
  });
  it('a core default the user moved away frees its chord for a plugin', () => {
    // ⌘T is new-session's default; the user moved new-session to ⌘Y.
    expect(resolve([cmd('t', 't')], {}, true)[0].bound).toBe(false);
    const out = resolve(
      [cmd('t', 't')],
      { mac: { 'new-session': ['Mod+Y'] } },
      true,
    );
    expect(out[0].bound).toBe(true);
    expect(out[0].shortcut).toBe('⌘T');
  });
  it('never takes the terminal copy chord off macOS', () => {
    // Mod+Shift+C is Ctrl+Shift+C there: xterm copy (app/session-term.ts).
    expect(resolve([cmd('c', 'c', true)], {}, false)[0].bound).toBe(false);
    expect(resolve([cmd('c', 'c', true)], {}, true)[0].bound).toBe(true);
  });
  it('never takes a reserved chord', () => {
    expect(resolve([cmd('z', 'z', true)], {}, true)[0].bound).toBe(false);
  });
  it('the user can rebind a plugin command', () => {
    const out = resolve(
      [cmd('o', 'o', true)],
      { mac: { 'plugin:p:o': ['Mod+Alt+O'] } },
      true,
    );
    expect(out[0].chords).toEqual(['Mod+Alt+O']);
    expect(out[0].shortcut).toBe('⌥⌘O');
  });
  it('the user can unbind a plugin command', () => {
    const out = resolve(
      [cmd('o', 'o', true)],
      { mac: { 'plugin:p:o': [] } },
      true,
    );
    expect(out[0].bound).toBe(false);
    expect(out[0].shortcut).toBe('');
  });
});

describe('a hand-edited keymap.json', () => {
  it('ignores a plugin override that is not a list', () => {
    const bad = { mac: { 'plugin:p:o': null } } as unknown as Keymap;
    const out = resolve([cmd('o', 'o', true)], bad, true);
    expect(out[0].chords).toEqual(['Mod+Shift+[KeyO]']);
  });
});
