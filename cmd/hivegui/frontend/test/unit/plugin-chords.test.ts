import { describe, expect, it } from 'vitest';
import {
  checkContributions,
  chordLabel,
  pluginChord,
  resolveCommands,
  type PluginCommand,
} from '../../src/lib/plugin-api.js';
import { chordMatches, chordsFor, parseChord } from '../../src/lib/chord.js';
import {
  DEFAULT_APP_BINDINGS,
  RESERVED_CHORDS,
} from '../../src/lib/bindings.js';

const run = () => {};
const cmd = (id: string, key?: string, shift = false): PluginCommand => ({
  id,
  title: id,
  run,
  keys: key ? { key, shift } : undefined,
});

// What the host passes: core's chords plus the reserved ones (spec 477
// compares chords; it used to compare display labels).
function coreChords(isMac: boolean): string[] {
  return [
    ...DEFAULT_APP_BINDINGS.flatMap((b) => chordsFor(b.keys, isMac)),
    ...RESERVED_CHORDS[isMac ? 'mac' : 'other'],
  ];
}

describe('chordLabel', () => {
  it('uses the core label style on each platform', () => {
    expect(chordLabel({ key: 'o', shift: true }, true)).toBe('⇧⌘O');
    expect(chordLabel({ key: 'o', shift: true }, false)).toBe('Ctrl+Shift+O');
    expect(chordLabel({ key: '7' }, true)).toBe('⌘7');
  });
  it('refuses anything but one letter or digit', () => {
    for (const key of ['', 'ab', '/', 'Enter', ' ']) {
      expect(chordLabel({ key }, true)).toBeNull();
    }
  });
});

describe('pluginChord', () => {
  // A plugin chord is dispatched only after core had the key, as ⌘/Ctrl
  // plus the key; `matches` runs it through the same matcher as a core
  // binding.
  const matches = (
    keys: { key: string; shift?: boolean },
    e: { code: string; shiftKey: boolean; altKey: boolean },
  ) => {
    const s = pluginChord(keys);
    return (
      s !== null &&
      chordMatches(parseChord(s, false), {
        key: '',
        metaKey: false,
        ctrlKey: true,
        ...e,
      })
    );
  };
  it('matches on e.code and shift, never on alt', () => {
    const k = { key: 'o', shift: true };
    expect(matches(k, { code: 'KeyO', shiftKey: true, altKey: false })).toBe(
      true,
    );
    expect(matches(k, { code: 'KeyO', shiftKey: false, altKey: false })).toBe(
      false,
    );
    expect(matches(k, { code: 'KeyO', shiftKey: true, altKey: true })).toBe(
      false,
    );
    expect(
      matches({ key: '3' }, { code: 'Digit3', shiftKey: false, altKey: false }),
    ).toBe(true);
  });
  it('is the platform modifier plus the physical key', () => {
    expect(pluginChord({ key: 'o', shift: true })).toBe('Mod+Shift+[KeyO]');
    expect(pluginChord({ key: '3' })).toBe('Mod+[Digit3]');
  });
  it('refuses anything but one letter or digit', () => {
    for (const key of ['', 'ab', '/', 'Enter', ' ']) {
      expect(pluginChord({ key })).toBeNull();
    }
  });
});

describe('resolveCommands', () => {
  for (const isMac of [true, false]) {
    it(`refuses a core chord (${isMac ? 'mac' : 'other'})`, () => {
      const warnings: string[] = [];
      const out = resolveCommands(
        [{ id: 'a', commands: [cmd('steal-t', 't'), cmd('free', 'o', true)] }],
        coreChords(isMac),
        isMac,
        (m) => warnings.push(m),
      );
      expect(out.map((r) => [r.command.id, r.bound])).toEqual([
        ['steal-t', false],
        ['free', true],
      ]);
      expect(out[0].shortcut).toBe('');
      expect(warnings.join()).toMatch(/already taken/);
    });
  }
  it('lets the first plugin keep a chord two plugins want', () => {
    const out = resolveCommands(
      [
        { id: 'a', commands: [cmd('a1', 'o', true)] },
        { id: 'b', commands: [cmd('b1', 'o', true)] },
      ],
      [],
      true,
    );
    expect(out.map((r) => [r.pluginId, r.bound, r.shortcut])).toEqual([
      ['a', true, '⇧⌘O'],
      ['b', false, ''],
    ]);
  });
  it('keeps keyless commands in the palette', () => {
    const out = resolveCommands([{ id: 'a', commands: [cmd('x')] }], [], true);
    expect(out).toHaveLength(1);
    expect(out[0].bound).toBe(false);
  });
});

describe('checkContributions', () => {
  it('accepts nothing at all', () => {
    expect(checkContributions(undefined)).toEqual({});
  });
  it('names the first problem', () => {
    expect(() => checkContributions(3)).toThrow(/object/);
    expect(() => checkContributions({ commands: [{ id: 'x' }] })).toThrow(
      /run function/,
    );
    expect(() => checkContributions({ badge: 'x' })).toThrow(/badge/);
    expect(() =>
      checkContributions({ sessionView: { modal: { title: 't' } } }),
    ).toThrow(/component/);
  });
});

// Spec 477 criterion 4: Settings › Shortcuts shows a plugin key core
// already holds as a conflict, so the resolver says which ones it refused.
describe('refused chords', () => {
  it('lists the keys a plugin asked for and did not get', () => {
    const [palette, free] = resolveCommands(
      [{ id: 'p', commands: [cmd('pal', 'k', true), cmd('free', 'y')] }],
      coreChords(true),
      true,
    );
    expect(palette.refused).toEqual(['Mod+Shift+[KeyK]']);
    expect(palette.bound).toBe(false);
    expect(free.refused).toEqual([]);
    expect(free.chords).toEqual(['Mod+[KeyY]']);
  });
});
