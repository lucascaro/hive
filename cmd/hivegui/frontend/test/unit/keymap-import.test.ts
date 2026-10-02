// Spec 477 criterion 9: importing a keymap file — parse, preview, settle
// conflicts, apply. The preview must agree with the real resolver
// (lib/bindings.ts): whatever it calls conflict-free, the resolver must
// neither displace nor double-bind.
import { describe, expect, it } from 'vitest';
import {
  DEFAULT_APP_BINDINGS,
  effectiveFor,
  keymapHalf,
  type Keymap,
} from '../../src/lib/bindings.js';
import { chordsFor, chordsOverlap } from '../../src/lib/chord.js';
import {
  applyImport,
  type Catalog,
  type ImportMode,
  importBase,
  importConflicts,
  parseKeymapFile,
  previewImport,
  reassignInImport,
  skipInImport,
} from '../../src/lib/keymap-import.js';

const PLUGIN = 'plugin:notes:edit';

// The tab's list: every bound core command, a titled one with no default
// key (restart-session), and one loaded plugin command.
const catalog: Catalog = new Map([
  ...[
    ...new Set(
      DEFAULT_APP_BINDINGS.flatMap((b) => (b.command ? [b.command] : [])),
    ),
  ].map((id) => [id, { title: id }] as const),
  ['restart-session', { title: 'Restart session' }],
  [PLUGIN, { title: 'Edit note', pluginDefaults: ['Mod+Shift+U'] }],
]);

const file = (k: object) => {
  const p = parseKeymapFile(JSON.stringify(k));
  if (!p.ok) throw new Error(p.error);
  return p;
};

function preview(k: object, isMac: boolean, mode: ImportMode = 'replace', draft: Keymap = {}) {
  const p = previewImport(file(k), isMac, catalog);
  const base = importBase(draft, isMac, mode, catalog);
  return { ...p, base };
}

// The resolver's view: no command displaced, and no chord fired by two
// commands that the shipped defaults do not already share (nav-back's
// Ctrl+_ reads as overlapping nav-forward's Ctrl+Shift+- and ships that
// way). Core only; plugins resolve core-wins in the plugin host.
function overlappingPairs(km: Keymap, isMac: boolean): Set<string> {
  const chords = effectiveFor(km, isMac).bindings.flatMap((b) =>
    b.command
      ? chordsFor(b.keys, isMac).map((c) => [b.command as string, c] as const)
      : [],
  );
  const out = new Set<string>();
  for (const [i, [a, ca]] of chords.entries())
    for (const [b, cb] of chords.slice(i + 1))
      if (a !== b && chordsOverlap(ca, cb, isMac)) out.add(`${a} ${ca} / ${b} ${cb}`);
  return out;
}

function resolverAgrees(km: Keymap, isMac: boolean): void {
  expect(effectiveFor(km, isMac).displaced).toEqual([]);
  const shipped = overlappingPairs({}, isMac);
  expect([...overlappingPairs(km, isMac)].filter((p) => !shipped.has(p))).toEqual([]);
}

describe('parseKeymapFile', () => {
  it.each([
    ['not JSON', '{'],
    ['an array', '[]'],
    ['a string', '"x"'],
    ['a later version', '{"version":2,"mac":{}}'],
    ['a half that is not an object', '{"mac":[]}'],
  ])('refuses %s', (_, text) => {
    expect(parseKeymapFile(text).ok).toBe(false);
  });

  it('reads an exported keymap back as itself', () => {
    const k = {
      version: 1,
      mac: { 'new-session': ['Mod+Y'], worktrees: [] },
      other: { settings: ['Ctrl+Alt+S'] },
    };
    const p = parseKeymapFile(`${JSON.stringify(k, null, 2)}\n`);
    expect(p.ok && p.keymap).toEqual({ mac: k.mac, other: k.other });
  });
});

describe('previewImport', () => {
  it('uses only this platform’s half', () => {
    const p = preview({ mac: { 'new-session': ['Mod+Y'] } }, false);
    expect(p.rows).toEqual([]);
    expect(p.candidate).toEqual({});
  });

  it('marks every row, skipping what cannot be bound', () => {
    const p = previewImport(
      file({
        mac: {
          'new-session': ['Mod+Y'],
          'no-such-thing': ['Mod+U'],
          settings: ['Mod+Q'],
          worktrees: ['E'],
          'zoom-reset': ['Mod+Bogus+X'],
          'toggle-sidebar': ['Ctrl+Alt+B'],
          'new-project': [],
          'restart-session': ['Mod+Shift+R'],
          'plugin:absent:go': ['Mod+Shift+G', 'Mod+Q'],
          'open-os-terminal': null,
        },
      }),
      true,
      catalog,
    );
    const by = (id: string, chord?: string) =>
      p.rows.find((r) => r.id === id && r.chord === chord)?.status;
    expect(by('new-session', 'Mod+Y')).toBe('ok');
    expect(by('no-such-thing')).toBe('unknown');
    expect(by('settings', 'Mod+Q')).toBe('reserved');
    expect(by('worktrees', 'E')).toBe('reserved');
    expect(by('zoom-reset', 'Mod+Bogus+X')).toBe('invalid');
    expect(by('toggle-sidebar', 'Ctrl+Alt+B')).toBe('warn');
    expect(by('new-project')).toBe('unbound');
    expect(by('restart-session', 'Mod+Shift+R')).toBe('ok');
    expect(by('plugin:absent:go', 'Mod+Shift+G')).toBe('kept');
    expect(by('plugin:absent:go', 'Mod+Q')).toBe('reserved');
    expect(by('open-os-terminal')).toBe('malformed');
    expect(p.candidate).toEqual({
      'new-session': ['Mod+Y'],
      'toggle-sidebar': ['Ctrl+Alt+B'],
      'new-project': [],
      'restart-session': ['Mod+Shift+R'],
      'plugin:absent:go': ['Mod+Shift+G'],
    });
  });

  it('refuses the OS’s keys off macOS too', () => {
    const p = preview({ other: { settings: ['Alt+F4'] } }, false);
    expect(p.rows[0].status).toBe('reserved');
    expect(p.candidate).toEqual({});
  });

  it('never treats __proto__ as a command', () => {
    const p = previewImport(
      parseKeymapFile('{"mac":{"__proto__":["Mod+Y"]}}') as never,
      true,
      catalog,
    );
    expect(p.rows.map((r) => r.status)).toEqual(['unknown']);
    expect(Object.keys(p.candidate)).toEqual([]);
  });
});

describe('import conflicts', () => {
  it('a key a default holds conflicts; Reassign leaves the holder its other keys', () => {
    const p = preview({ mac: { 'new-session': ['Mod+N'] } }, true);
    const cs = importConflicts(p.candidate, p.base, true, catalog);
    expect(cs.map((c) => [c.id, c.chord, c.holders.map((h) => h.id)])).toEqual([
      ['new-session', 'Mod+N', ['new-project']],
    ]);
    const next = reassignInImport(p.candidate, true, cs[0]);
    expect(next['new-project']).toEqual([]);
    expect(importConflicts(next, p.base, true, catalog)).toEqual([]);
    resolverAgrees(applyImport({}, next, p.base, true), true);
  });

  it('Skip drops the key; a command with none left keeps what it has', () => {
    const p = preview({ mac: { 'new-session': ['Mod+N'] } }, true);
    const next = skipInImport(p.candidate, 'new-session', 'Mod+N');
    expect(next).toEqual({});
    expect(importConflicts(next, p.base, true, catalog)).toEqual([]);
    resolverAgrees(applyImport({}, next, p.base, true), true);
  });

  it('two imported commands on one key both conflict; one Reassign clears both', () => {
    const p = preview({ mac: { 'new-session': ['Mod+Y'], settings: ['Mod+Y'] } }, true);
    const cs = importConflicts(p.candidate, p.base, true, catalog);
    expect(cs.map((c) => c.id).sort()).toEqual(['new-session', 'settings']);
    const next = reassignInImport(p.candidate, true, cs[0]);
    expect(importConflicts(next, p.base, true, catalog)).toEqual([]);
    resolverAgrees(applyImport({}, next, p.base, true), true);
  });

  it('no conflict with a default the import itself moves away', () => {
    const p = preview(
      { mac: { 'new-session': ['Mod+N'], 'new-project': ['Mod+Shift+Y'] } },
      true,
    );
    expect(importConflicts(p.candidate, p.base, true, catalog)).toEqual([]);
    resolverAgrees(applyImport({}, p.candidate, p.base, true), true);
  });

  it('a clash on a second layout spelling is a conflict', () => {
    // zoom-in ships as ['Mod+Shift?+=', 'Mod+Shift?++'].
    const p = preview({ mac: { 'new-session': ['Mod+Shift?++'] } }, true);
    const cs = importConflicts(p.candidate, p.base, true, catalog);
    expect(cs.flatMap((c) => c.holders.map((h) => h.id))).toContain('zoom-in');
    let next = p.candidate;
    for (const c of importConflicts(next, p.base, true, catalog))
      next = reassignInImport(next, true, c);
    expect(importConflicts(next, p.base, true, catalog)).toEqual([]);
    resolverAgrees(applyImport({}, next, p.base, true), true);
  });

  it('a plugin key is a holder like any other', () => {
    const p = preview({ mac: { 'new-session': ['Mod+Shift+U'] } }, true);
    const cs = importConflicts(p.candidate, p.base, true, catalog);
    expect(cs[0].holders.map((h) => h.id)).toEqual([PLUGIN]);
    expect(reassignInImport(p.candidate, true, cs[0])[PLUGIN]).toEqual([]);
  });

  it('entries for a plugin that is not loaded are not checked for conflicts', () => {
    const p = preview({ mac: { 'plugin:absent:go': ['Mod+T'] } }, true);
    expect(importConflicts(p.candidate, p.base, true, catalog)).toEqual([]);
  });

  // Every Reassign and every Skip leaves a state the resolver agrees
  // with, on both platforms.
  it.each([true, false])('settling every conflict agrees with the resolver (mac=%s)', (isMac) => {
    const imports = isMac
      ? { mac: { 'new-session': ['Mod+N', 'Mod+Shift?++'], settings: ['Mod+N'], worktrees: ['Mod+T'] } }
      : { other: { 'new-session': ['Ctrl+N', 'Ctrl+Shift?++'], settings: ['Ctrl+N'], worktrees: ['Ctrl+T'] } };
    for (const how of ['reassign', 'skip'] as const) {
      const p = preview(imports, isMac);
      let next = p.candidate;
      for (let guard = 0; guard < 20; guard++) {
        const [c] = importConflicts(next, p.base, isMac, catalog);
        if (!c) break;
        next = how === 'reassign' ? reassignInImport(next, isMac, c) : skipInImport(next, c.id, c.chord);
      }
      expect(importConflicts(next, p.base, isMac, catalog)).toEqual([]);
      resolverAgrees(applyImport({}, next, p.base, isMac), isMac);
    }
  });
});

describe('modes', () => {
  const draft: Keymap = {
    mac: { settings: ['Mod+Shift+S'], 'plugin:absent:go': ['Mod+Shift+G'], [PLUGIN]: ['Mod+Shift+E'] },
    other: { worktrees: [] },
  };

  it('replace makes the file this platform’s keymap, keeping unloaded plugins', () => {
    const p = preview({ mac: { 'new-session': ['Mod+Y'] } }, true, 'replace', draft);
    const out = applyImport(draft, p.candidate, p.base, true);
    expect(out.mac).toEqual({ 'plugin:absent:go': ['Mod+Shift+G'], 'new-session': ['Mod+Y'] });
    expect(out.other).toBe(draft.other);
  });

  it('replace: an imported entry for an unloaded plugin wins over the draft’s', () => {
    const p = preview({ mac: { 'plugin:absent:go': ['Mod+Shift+H'] } }, true, 'replace', draft);
    expect(keymapHalf(applyImport(draft, p.candidate, p.base, true), true)['plugin:absent:go']).toEqual(['Mod+Shift+H']);
  });

  it('add keeps every other override', () => {
    const p = preview({ mac: { 'new-session': ['Mod+Y'] } }, true, 'add', draft);
    const out = applyImport(draft, p.candidate, p.base, true);
    expect(out.mac).toEqual({ ...draft.mac, 'new-session': ['Mod+Y'] });
  });

  it('add judges conflicts against the draft’s keys, not the defaults', () => {
    // The draft moved settings to ⇧⌘S, so importing ⇧⌘S for new-session
    // clashes under add but not under replace.
    const k = { mac: { 'new-session': ['Mod+Shift+S'] } };
    const add = preview(k, true, 'add', draft);
    expect(importConflicts(add.candidate, add.base, true, catalog)[0].holders.map((h) => h.id)).toEqual(['settings']);
    const replace = preview(k, true, 'replace', draft);
    expect(importConflicts(replace.candidate, replace.base, true, catalog)).toEqual([]);
  });
});
