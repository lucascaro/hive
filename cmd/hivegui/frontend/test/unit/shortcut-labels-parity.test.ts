// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Spec 477: labels are derived from the binding data now. With no user
// keymap they must read exactly as the hand-written tables did — the
// fixture is that table's output, captured before the refactor.
import { describe, expect, it } from 'vitest';
import fixture from './fixtures/shortcuts-v0.json';
import { labelIn } from '../../src/lib/bindings.js';
import { paletteShortcuts, shortcutGroups } from '../../src/lib/shortcuts.js';

const EMPTY = {};

// Spec 544 re-keys three commands the pre-477 tables knew and adds the
// session finder, so with no keymap the labels differ from the frozen
// fixture in exactly these places. Anything else drifting still fails.
const REKEYED_544 = {
  mac: {
    'duplicate-session': '⌘K',
    'duplicate-session-choose-tool': '⇧⌘K',
    'command-palette': '⇧⌘P',
  },
  other: {
    'duplicate-session': 'Ctrl+K',
    'duplicate-session-choose-tool': 'Ctrl+Shift+K',
    'command-palette': 'Ctrl+Shift+P',
  },
} as const;
const FINDER_ROW_544 = {
  mac: { keys: '⌘P', label: 'Go to session' },
  other: { keys: 'Ctrl+P', label: 'Go to session' },
} as const;
type Row = { keys: string; label: string };
type Group = { title: string; items: Row[] };
// The fixture with spec 544's deltas applied: re-keyed rows take their new
// keys and the finder's row follows the command palette's.
function expectedGroups(name: 'mac' | 'other', groups: Group[]): Group[] {
  const byLabel: Record<string, string> = {
    'Duplicate session': REKEYED_544[name]['duplicate-session'],
    'Duplicate session (choose tool)':
      REKEYED_544[name]['duplicate-session-choose-tool'],
    'Command palette': REKEYED_544[name]['command-palette'],
  };
  return groups.map((g) => ({
    ...g,
    items: g.items.flatMap((r) => {
      const keys = byLabel[r.label] ?? r.keys;
      return r.label === 'Command palette'
        ? [{ ...r, keys }, FINDER_ROW_544[name]]
        : [{ ...r, keys }];
    }),
  }));
}
const platforms = [
  ['mac', true],
  ['other', false],
] as const;

describe('labels match the pre-477 tables with no keymap', () => {
  for (const [name, isMac] of platforms) {
    const v0 = fixture[name];
    it(`help overlay groups (${name})`, () => {
      expect(shortcutGroups({ isMac })).toEqual(
        expectedGroups(name, v0.groups as Group[]),
      );
    });
    it(`palette label for every command (${name})`, () => {
      const ids = Object.keys(v0.palette);
      expect(ids.length).toBeGreaterThan(30);
      for (const id of ids) {
        // restart-session never had a key; move-* labelled the arrow
        // chords that now run them (D1).
        expect(labelIn(EMPTY, id, isMac), id).toBe(
          (REKEYED_544[name] as Record<string, string>)[id] ??
            (v0.palette as Record<string, string>)[id],
        );
      }
    });
    it(`no command gains a palette label it lacked (${name})`, () => {
      const now = paletteShortcuts({ isMac });
      const v0p = v0.palette as Record<string, string>;
      for (const [id, label] of Object.entries(now)) {
        if (id in v0p)
          expect(label, id).toBe(
            (REKEYED_544[name] as Record<string, string>)[id] ?? v0p[id],
          );
      }
    });
  }
});
