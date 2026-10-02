// Importing a keymap file (spec 477, criterion 9): read it, preview what
// it would change, settle its conflicts, and apply it to the Settings
// draft — nothing changes before the user confirms.
//
// Only the current platform's half of the file is used; an export
// carries both halves (decision log 2026-09-30). The user picks how it
// lands: `replace` makes it the keymap for this platform (overrides for
// plugins that are not loaded are kept), `add` lays it over the
// overrides the draft already has.
//
// Conflicts use the tab's rule: a key another command holds, where that
// command's keys are its imported ones, or else what it has now (its
// defaults under `replace`, its draft keys under `add`). Reassign takes
// the key from the holder, which keeps its other keys (keymap-edit
// reassign); Skip drops it from the import.
//
// Pure module: no DOM, no store.

import {
  chordsOfIn,
  keymapHalf,
  shortcutsIn,
  type Keymap,
} from './bindings.js';
import { chordsOverlap, parseChord } from './chord.js';
import { checkChord, keymapFromJSON, type Holder } from './keymap-edit.js';

export type ImportMode = 'replace' | 'add';

/** The commands an import may name: the ones Settings › Shortcuts lists.
 * Plugin entries carry their default chords (core defaults come from the
 * binding table). */
export type Catalog = ReadonlyMap<
  string,
  { title: string; pluginDefaults?: readonly string[] }
>;

type Half = Readonly<Record<string, readonly string[]>>;

export type RowStatus =
  | 'ok'
  | 'warn'
  | 'unbound'
  | 'kept'
  | 'unknown'
  | 'reserved'
  | 'invalid'
  | 'malformed';

/** One line of the preview: an entry, or one key of an entry. */
export interface ImportRow {
  id: string;
  chord?: string;
  status: RowStatus;
  reason?: string;
}

export interface ImportPreview {
  rows: ImportRow[];
  /** What the import would set, per command: the keys that survived the
   * checks. Conflicts are settled by editing this. */
  candidate: Half;
}

export interface ImportConflict {
  id: string;
  chord: string;
  holders: Holder[];
}

const has = (o: object, k: string) => Object.hasOwn(o, k);

/** Where a `plugin:<plugin>:<command>` id comes from, or null for core. */
function pluginOf(id: string): string | null {
  const m = /^plugin:([^:]+):/.exec(id);
  return m ? m[1] : null;
}

function loadedPlugins(catalog: Catalog): Set<string> {
  const out = new Set<string>();
  for (const id of catalog.keys()) {
    const p = pluginOf(id);
    if (p) out.add(p);
  }
  return out;
}

/** An id the catalog cannot check: a plugin that is not loaded. Its
 * entries are carried as they are, as SaveKeymap does. */
function isUnloadedPlugin(id: string, loaded: Set<string>): boolean {
  const p = pluginOf(id);
  return p !== null && !loaded.has(p);
}

// ---------- reading ----------

export type ParsedFile =
  | { ok: true; keymap: Keymap; malformed: string[] }
  | { ok: false; error: string };

/** A keymap file's text, read the way keymap.json is: anything malformed
 * is dropped on its own. Refused outright when it is not a keymap. */
export function parseKeymapFile(text: string): ParsedFile {
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch {
    return {
      ok: false,
      error: 'That file is not JSON, so it is not a Hive keymap.',
    };
  }
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) {
    return { ok: false, error: 'That file is not a Hive keymap.' };
  }
  const r = raw as Record<string, unknown>;
  if (r.version !== undefined && r.version !== 1) {
    return {
      ok: false,
      error: `That keymap is version ${String(r.version)}; this Hive reads version 1.`,
    };
  }
  for (const half of ['mac', 'other']) {
    const v = r[half];
    if (
      v !== undefined &&
      (typeof v !== 'object' || v === null || Array.isArray(v))
    )
      return { ok: false, error: 'That file is not a Hive keymap.' };
  }
  return { ok: true, ...keymapFromJSON(raw) };
}

// ---------- previewing ----------

/** The rows to show and the keys that would be set, for this platform's
 * half of a parsed file. */
export function previewImport(
  file: { keymap: Keymap; malformed: readonly string[] },
  isMac: boolean,
  catalog: Catalog,
): ImportPreview {
  const half = isMac ? 'mac' : 'other';
  const loaded = loadedPlugins(catalog);
  const rows: ImportRow[] = [];
  const candidate: [string, string[]][] = [];
  for (const m of file.malformed) {
    // `half: id`; an id may itself contain ': '.
    const at = m.indexOf(': ');
    const h = m.slice(0, at);
    const id = m.slice(at + 2);
    if (at > 0 && h === half)
      rows.push({ id, status: 'malformed', reason: 'Not a list of keys.' });
  }
  for (const [id, chords] of Object.entries(keymapHalf(file.keymap, isMac))) {
    const unloaded = isUnloadedPlugin(id, loaded);
    if (!unloaded && !catalog.has(id)) {
      rows.push({
        id,
        status: 'unknown',
        reason: 'No such command in this Hive.',
      });
      continue;
    }
    if (chords.length === 0) {
      rows.push({ id, status: 'unbound' });
      candidate.push([id, []]);
      continue;
    }
    const kept: string[] = [];
    for (const chord of new Set(chords)) {
      try {
        parseChord(chord, isMac);
      } catch {
        rows.push({
          id,
          chord,
          status: 'invalid',
          reason: 'Not a key Hive can read.',
        });
        continue;
      }
      const check = checkChord(chord, isMac);
      if (check.kind === 'refused') {
        rows.push({ id, chord, status: 'reserved', reason: check.reason });
        continue;
      }
      kept.push(chord);
      if (unloaded) {
        rows.push({
          id,
          chord,
          status: 'kept',
          reason: 'Plugin not loaded; checked when it is.',
        });
      } else if (check.kind === 'warn') {
        rows.push({ id, chord, status: 'warn', reason: check.reason });
      } else {
        rows.push({ id, chord, status: 'ok' });
      }
    }
    // Every key skipped: the command keeps what it has, it is not unbound.
    if (kept.length) candidate.push([id, kept]);
  }
  return { rows, candidate: Object.fromEntries(candidate) };
}

// ---------- what the result would be ----------

/** The overrides the import lands on: under `replace`, only the draft's
 * entries for plugins that are not loaded; under `add`, all of them. */
export function importBase(
  draft: Keymap,
  isMac: boolean,
  mode: ImportMode,
  catalog: Catalog,
): Half {
  const half = keymapHalf(draft, isMac);
  if (mode === 'add') return half;
  const loaded = loadedPlugins(catalog);
  return Object.fromEntries(
    Object.entries(half).filter(([id]) => isUnloadedPlugin(id, loaded)),
  );
}

/** The keymap the import would leave: the base with the candidate on top,
 * the other platform's half untouched. */
export function applyImport(
  draft: Keymap,
  candidate: Half,
  base: Half,
  isMac: boolean,
): Keymap {
  const entries = [...Object.entries(base), ...Object.entries(candidate)];
  return {
    ...draft,
    [isMac ? 'mac' : 'other']: Object.fromEntries(
      entries.map(([id, c]) => [id, [...c]]),
    ),
  };
}

// The keys a command would have, if the import left it alone: `all` is
// every chord that fires it (layout spellings included), for finding
// clashes; `shortcuts` one chord per key, for editing.
function keysOf(
  id: string,
  candidate: Half,
  base: Half,
  baseKeymap: Keymap,
  catalog: Catalog,
  isMac: boolean,
): { all: readonly string[]; shortcuts: readonly string[] } {
  if (has(candidate, id))
    return { all: candidate[id], shortcuts: candidate[id] };
  const plugin = catalog.get(id)?.pluginDefaults;
  if (plugin) {
    const k = has(base, id) ? base[id] : plugin;
    return { all: k, shortcuts: k };
  }
  return {
    all: chordsOfIn(baseKeymap, id, isMac),
    shortcuts: shortcutsIn(baseKeymap, id, isMac),
  };
}

function baseKeymapFor(base: Half, candidate: Half, isMac: boolean): Keymap {
  // The commands the import names are judged by their imported keys, so
  // they leave the base; what remains resolves as it would today
  // (displacements included).
  const rest = Object.fromEntries(
    Object.entries(base).filter(([id]) => !has(candidate, id)),
  );
  return { [isMac ? 'mac' : 'other']: rest };
}

/** The keys in the candidate another command would also hold. A pair of
 * imported commands that clash shows from both sides. Entries for plugins
 * that are not loaded are not checked: the plugin host resolves them
 * (core wins) when the plugin loads. */
export function importConflicts(
  candidate: Half,
  base: Half,
  isMac: boolean,
  catalog: Catalog,
): ImportConflict[] {
  const baseKeymap = baseKeymapFor(base, candidate, isMac);
  const out: ImportConflict[] = [];
  for (const [id, chords] of Object.entries(candidate)) {
    if (!catalog.has(id)) continue;
    for (const chord of chords) {
      const holders: Holder[] = [];
      for (const other of catalog.keys()) {
        if (other === id) continue;
        const k = keysOf(other, candidate, base, baseKeymap, catalog, isMac);
        if (k.all.some((c) => chordsOverlap(c, chord, isMac)))
          holders.push({ id: other, shortcuts: k.shortcuts });
      }
      if (holders.length) out.push({ id, chord, holders });
    }
  }
  return out;
}

/** Gives `chord` to `id`: every other command that holds it loses that
 * key and keeps the rest, as an imported entry of its own. */
export function reassignInImport(
  candidate: Half,
  isMac: boolean,
  conflict: ImportConflict,
): Half {
  const next: Record<string, readonly string[]> = { ...candidate };
  for (const h of conflict.holders) {
    next[h.id] = h.shortcuts.filter(
      (s) => !chordsOverlap(s, conflict.chord, isMac),
    );
  }
  return next;
}

/** Drops `chord` from the import's keys for `id`. A command left with no
 * imported key keeps what it has, rather than being unbound. */
export function skipInImport(candidate: Half, id: string, chord: string): Half {
  const next: Record<string, readonly string[]> = { ...candidate };
  const left = (next[id] ?? []).filter((c) => c !== chord);
  if (left.length) next[id] = left;
  else delete next[id];
  return next;
}
