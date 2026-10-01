// Spec 477: labels are derived from the binding data now. With no user
// keymap they must read exactly as the hand-written tables did — the
// fixture is that table's output, captured before the refactor.
import { describe, expect, it } from 'vitest';
import fixture from './fixtures/shortcuts-v0.json';
import { labelIn } from '../../src/lib/bindings.js';
import { paletteShortcuts, shortcutGroups } from '../../src/lib/shortcuts.js';

const EMPTY = {};
const platforms = [
  ['mac', true],
  ['other', false],
] as const;

describe('labels match the pre-477 tables with no keymap', () => {
  for (const [name, isMac] of platforms) {
    const v0 = fixture[name];
    it(`help overlay groups (${name})`, () => {
      expect(shortcutGroups({ isMac })).toEqual(v0.groups);
    });
    it(`palette label for every command (${name})`, () => {
      const ids = Object.keys(v0.palette);
      expect(ids.length).toBeGreaterThan(30);
      for (const id of ids) {
        // restart-session never had a key; move-* labelled the arrow
        // chords that now run them (D1).
        expect(labelIn(EMPTY, id, isMac), id).toBe(
          (v0.palette as Record<string, string>)[id],
        );
      }
    });
    it(`no command gains a palette label it lacked (${name})`, () => {
      const now = paletteShortcuts({ isMac });
      const v0p = v0.palette as Record<string, string>;
      for (const [id, label] of Object.entries(now)) {
        if (id in v0p) expect(label, id).toBe(v0p[id]);
      }
    });
  }
});
