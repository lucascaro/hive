// Settings › Shortcuts (spec 477): rebind any app or plugin command.
//
// Edits a draft keymap the Settings dialog owns and writes on Save, like
// the agent list. Nothing here touches the live bindings: dispatch, every
// label and the native menu follow the store, which Save updates.
//
// A key is captured by focusing a capture button and pressing it. While
// that button has focus the native menu's accelerators are suspended
// (keymap-sync setShortcutCapture) and the `shortcut-capture` key scope
// (app/key-scopes.ts) keeps the app's own chords from firing, so ⌘T
// arrives here as a keydown instead of opening the launcher.
//
// A captured key another command holds is not applied: both rows show the
// conflict until the user reassigns it (the other command loses only that
// key) or cancels, and Save waits for the answer (spec 477 criterion 3).
//
// Export… saves the draft (what the tab shows) to a file; Import… reads
// one into a preview that takes the list's place. The user chooses to
// replace their shortcuts or add to them, settles each conflict, and
// Confirm writes the result into the draft (criterion 9,
// lib/keymap-import.ts). Save waits while a preview is open.

import { useEffect, useMemo, useRef, useState } from 'react';
import type { main } from '../../../wailsjs/go/models';
import { listCommands } from '../../app/command-registry.js';
import { setShortcutCapture } from '../../app/keymap-sync.js';
import { resolvePluginCommands } from '../../app/plugin-host.js';
import { ExportKeymap, PickKeymapFile } from '../../bridge.js';
import {
  DEFAULT_APP_BINDINGS,
  EMPTY_KEYMAP,
  effectiveFor,
  keymapHalf,
  shortcutsIn,
  type Keymap,
} from '../../lib/bindings.js';
import { chordsFor, chordsOverlap } from '../../lib/chord.js';
import { chordLabel } from '../../lib/chord-label.js';
import {
  canonicalKeymap,
  captureChord,
  checkChord,
  reassign,
  resetHalf,
  withShortcuts,
  type Holder,
} from '../../lib/keymap-edit.js';
import {
  applyImport,
  type Catalog,
  type ImportMode,
  type ImportPreview,
  importBase,
  importConflicts,
  parseKeymapFile,
  previewImport,
  reassignInImport,
  skipInImport,
} from '../../lib/keymap-import.js';
import { isMac } from '../../lib/platform.js';
import { commandGroups, terminalShortcuts } from '../../lib/shortcuts.js';
import { useAppStore } from '../../store/store.js';
import { Button } from '../Button.js';
import { Icon } from '../Icon.js';
import { IconButton } from '../IconButton.js';
import { Kbd } from '../Kbd.js';

interface Row {
  id: string;
  label: string;
  /** Plugin rows: the chords the plugin asked for and did not get. */
  refused?: readonly string[];
}

interface Group {
  title: string;
  rows: Row[];
}

interface Pending {
  id: string;
  chord: string;
  holders: Holder[];
}

type Note = { id: string; kind: 'warn' | 'refused'; text: string };

/** An import being previewed: what the file said, how it lands, and its
 * keys as the user has settled them so far. */
interface Importing {
  preview: ImportPreview;
  mode: ImportMode | null;
  candidate: ImportPreview['candidate'];
}

/** Why Save must wait: a captured key's conflict, or an open import. */
export type ShortcutsBlock = null | 'conflict' | 'import';

export interface ShortcutsPanelProps {
  draft: Keymap;
  onChange: (next: Keymap) => void;
  /** keymap.json: editing is off until it is read, and stays off if it
   * cannot be. */
  status: 'loading' | 'failed' | 'ready';
  /** Called with the reason whenever Save must wait, null once it need not. */
  onBlockedChange: (reason: ShortcutsBlock) => void;
  /** Shows a message in the dialog's error slot. */
  onError: (msg: string) => void;
}

const label = (chord: string) => chordLabel(chord, isMac);

export function ShortcutsPanel({
  draft,
  onChange,
  status,
  onBlockedChange,
  onError,
}: ShortcutsPanelProps) {
  const disabled = status !== 'ready';
  const pluginUI = useAppStore((s) => s.pluginUI);
  const plugins = useAppStore((s) => s.plugins);
  const [query, setQuery] = useState('');
  const [capturing, setCapturing] = useState<string | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const [note, setNote] = useState<Note | null>(null);
  const [importing, setImporting] = useState<Importing | null>(null);
  // A file dialog is open (Export… or Import…).
  const [busy, setBusy] = useState(false);
  // The draft the last export wrote, for "Shortcuts exported."; the note
  // goes once the draft moves on (an edit, an import) or a new export or
  // import starts.
  const [exported, setExported] = useState<Keymap | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);

  // Where focus goes after an action removes or disables the control that
  // had it (the capture button, a remove or reset button, Reset all):
  // the row's + button, Reassign while a conflict waits, or the search
  // box. Applied after the render that changed the row. A capture that
  // ends because focus left it (Tab, a click elsewhere) sets nothing, so
  // focus is never pulled back.
  // State, not a ref: setting a target always renders, so the effect
  // below consumes it at once and it can never linger to move focus on
  // some later, unrelated render.
  const [focusNext, setFocusNext] = useState<
    { command: string; action: string } | { selector: string } | 'search' | null
  >(null);
  const focusRow = (command: string, action = 'add-shortcut') =>
    setFocusNext({ command, action });
  useEffect(() => {
    const t = focusNext;
    if (!t) return;
    setFocusNext(null);
    const root = rootRef.current;
    const row =
      t === 'search' || 'selector' in t
        ? undefined
        : [
            ...(root?.querySelectorAll<HTMLElement>('.hv-shortcut-row') ?? []),
          ].find((r) => r.dataset.command === t.command);
    const target =
      t === 'search'
        ? null
        : 'selector' in t
          ? root?.querySelector<HTMLElement>(t.selector)
          : row?.querySelector<HTMLElement>(
              `[data-action="${t.action}"]:not(:disabled)`,
            );
    // The row can be filtered out by the search it no longer matches.
    (
      target ?? root?.querySelector<HTMLElement>('#settings-shortcuts-search')
    )?.focus();
  }, [focusNext]);

  const blocked: ShortcutsBlock = pending
    ? 'conflict'
    : importing
      ? 'import'
      : null;
  useEffect(() => onBlockedChange(blocked), [blocked, onBlockedChange]);
  // The menu is suspended exactly while a capture button is on screen. Not
  // driven by focus events: removing a focused button fires no blur. The
  // button blurs (window switch, click away) into capturing = null.
  useEffect(() => setShortcutCapture(capturing !== null), [capturing]);
  useEffect(() => () => setShortcutCapture(false), []);

  // Unmemoized lookups with no warnings: the live pluginCommands memo
  // belongs to dispatch, and replacing it from here would repeat its
  // clash warnings on the next keystroke.
  const resolved = useMemo(
    () => resolvePluginCommands(pluginUI, draft),
    [pluginUI, draft],
  );
  const pluginDefaults = useMemo(
    () => resolvePluginCommands(pluginUI, EMPTY_KEYMAP),
    [pluginUI],
  );
  const eff = effectiveFor(draft, isMac);

  const groups = useMemo((): Group[] => {
    const core = commandGroups(isMac).map((g) => ({
      title: g.title,
      rows: g.items.map((r) => ({ id: r.command, label: r.label })),
    }));
    const listed = new Set(core.flatMap((g) => g.rows.map((r) => r.id)));
    const other = listCommands()
      .filter(
        (c) => !c.id.startsWith('plugin:') && c.title && !listed.has(c.id),
      )
      .map((c) => ({ id: c.id, label: c.title as string }));
    const byPlugin = new Map<string, Row[]>();
    for (const r of resolved) {
      const rows = byPlugin.get(r.pluginId) ?? [];
      rows.push({
        id: `plugin:${r.pluginId}:${r.command.id}`,
        label: r.command.title,
        refused: r.refused,
      });
      byPlugin.set(r.pluginId, rows);
    }
    const name = (id: string) => plugins.find((p) => p.id === id)?.name ?? id;
    return [
      ...core,
      ...(other.length ? [{ title: 'Other commands', rows: other }] : []),
      ...[...byPlugin].map(([id, rows]) => ({
        title: `Plugin: ${name(id)}`,
        rows,
      })),
    ];
  }, [resolved, plugins]);

  const titles = useMemo(
    () => new Map(groups.flatMap((g) => g.rows.map((r) => [r.id, r.label]))),
    [groups],
  );
  const titleOf = (id: string) => titles.get(id) ?? id;

  // What an import may name: exactly the commands listed here.
  const catalog = useMemo((): Catalog => {
    const plugin = new Map(
      pluginDefaults.map((r) => [
        `plugin:${r.pluginId}:${r.command.id}`,
        r.chords,
      ]),
    );
    return new Map(
      groups.flatMap((g) =>
        g.rows.map((r) => {
          const defaults = plugin.get(r.id);
          return [
            r.id,
            defaults
              ? { title: r.label, pluginDefaults: defaults }
              : { title: r.label },
          ] as const;
        }),
      ),
    );
  }, [groups, pluginDefaults]);

  // A command's shortcuts (one chord each) and every chord that fires it.
  function shortcutsOf(id: string): string[] {
    if (!id.startsWith('plugin:')) return shortcutsIn(draft, id, isMac);
    const r = resolved.find(
      (x) => `plugin:${x.pluginId}:${x.command.id}` === id,
    );
    return r ? [...r.chords] : [];
  }
  function holdersOf(chord: string, except: string): Holder[] {
    const out: Holder[] = [];
    const seen = new Set<string>();
    for (const b of eff.bindings) {
      if (b.command === null || b.command === except || seen.has(b.command))
        continue;
      if (
        chordsFor(b.keys, isMac).some((c) => chordsOverlap(c, chord, isMac))
      ) {
        seen.add(b.command);
        out.push({ id: b.command, shortcuts: shortcutsOf(b.command) });
      }
    }
    for (const r of resolved) {
      const id = `plugin:${r.pluginId}:${r.command.id}`;
      if (id === except) continue;
      if (r.chords.some((c) => chordsOverlap(c, chord, isMac)))
        out.push({ id, shortcuts: [...r.chords] });
    }
    return out;
  }
  const isOverridden = (id: string) => id in keymapHalf(draft, isMac);

  // Why a command with defaults has no shortcut: one of its defaults is
  // a key the user gave another command (spec 477 criterion 8).
  function displacedBy(id: string): { by: string; chord: string } | null {
    if (!eff.displaced.includes(id)) return null;
    const half = keymapHalf(draft, isMac);
    const defaults = DEFAULT_APP_BINDINGS.filter(
      (b) => b.command === id,
    ).flatMap((b) => chordsFor(b.keys, isMac));
    for (const [by, chords] of Object.entries(half)) {
      if (!Array.isArray(chords)) continue;
      const hit = defaults.find((d) =>
        chords.some((c) => chordsOverlap(c, d, isMac)),
      );
      if (hit) return { by, chord: hit };
    }
    return null;
  }

  function capture(id: string, e: React.KeyboardEvent<HTMLButtonElement>) {
    const plain = !e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey;
    // Tab and Shift+Tab leave the field; blur ends the capture.
    if (!e.metaKey && !e.ctrlKey && !e.altKey && e.key === 'Tab') return;
    e.preventDefault();
    e.stopPropagation();
    if (plain && e.key === 'Escape') {
      setCapturing(null);
      focusRow(id);
      return;
    }
    const got = captureChord(e.nativeEvent, isMac);
    if (!got) return; // a modifier on its own: keep listening
    setCapturing(null);
    focusRow(id);
    if (got.kind === 'refused') {
      setNote({ id, kind: 'refused', text: got.reason });
      return;
    }
    const { chord } = got;
    const check = checkChord(chord, isMac);
    if (check.kind === 'refused') {
      setNote({ id, kind: 'refused', text: check.reason });
      return;
    }
    const current = shortcutsOf(id);
    if (current.some((c) => chordsOverlap(c, chord, isMac))) {
      setNote(null);
      return; // it already has that key
    }
    setNote(
      check.kind === 'warn' ? { id, kind: 'warn', text: check.reason } : null,
    );
    const holders = holdersOf(chord, id);
    if (holders.length) {
      setPending({ id, chord, holders });
      focusRow(id, 'reassign');
      return;
    }
    onChange(withShortcuts(draft, isMac, id, [...current, chord]));
  }

  function resolvePending(accept: boolean) {
    if (!pending) return;
    if (accept) {
      onChange(
        reassign(
          draft,
          isMac,
          pending.id,
          shortcutsOf(pending.id),
          pending.chord,
          pending.holders,
        ),
      );
    }
    setPending(null);
    setNote(null);
    focusRow(pending.id);
  }

  // A plugin key core (or an earlier plugin) already holds: give it to the
  // plugin, or settle the plugin on the keys it has.
  function resolveRefused(id: string, chord: string, give: boolean) {
    const current = shortcutsOf(id);
    focusRow(id);
    onChange(
      give
        ? reassign(draft, isMac, id, current, chord, holdersOf(chord, id))
        : withShortcuts(draft, isMac, id, current),
    );
  }

  function remove(id: string, chord: string) {
    focusRow(id);
    onChange(
      withShortcuts(
        draft,
        isMac,
        id,
        shortcutsOf(id).filter((c) => c !== chord),
      ),
    );
  }

  // ---------- export / import ----------

  const errText = (e: unknown) =>
    String((e as { message?: string })?.message || e);

  async function exportKeymap() {
    setBusy(true);
    setExported(null);
    try {
      if (await ExportKeymap(canonicalKeymap(draft) as main.Keymap))
        setExported(draft);
    } catch (e) {
      onError(`Could not export your shortcuts. (${errText(e)})`);
    } finally {
      setBusy(false);
      // The button disabled itself for the dialog, which drops its focus.
      setFocusNext({ selector: '#settings-shortcuts-export' });
    }
  }

  async function startImport() {
    let opened = false;
    setBusy(true);
    setExported(null);
    try {
      const text = await PickKeymapFile();
      if (!text) return; // cancelled
      const parsed = parseKeymapFile(text);
      if (!parsed.ok) {
        onError(parsed.error);
        return;
      }
      setNote(null);
      setImporting({
        preview: previewImport(parsed, isMac, catalog),
        mode: null,
        candidate: {},
      });
      setFocusNext({ selector: 'input[name="shortcut-import-mode"]' });
      opened = true;
    } catch (e) {
      onError(`Could not read that file. (${errText(e)})`);
    } finally {
      setBusy(false);
      // Cancelled or refused: back to the button, which disabled itself
      // for the dialog and so lost focus.
      if (!opened) setFocusNext({ selector: '#settings-shortcuts-import' });
    }
  }

  const importBaseHalf =
    importing?.mode != null
      ? importBase(draft, isMac, importing.mode, catalog)
      : {};
  const conflicts =
    importing?.mode != null
      ? importConflicts(importing.candidate, importBaseHalf, isMac, catalog)
      : [];

  function chooseMode(mode: ImportMode) {
    // A new choice starts again from the file: settling depends on it.
    setImporting((cur) =>
      cur ? { ...cur, mode, candidate: cur.preview.candidate } : cur,
    );
  }

  function settleImport(id: string, chord: string, take: boolean) {
    const c = conflicts.find((x) => x.id === id && x.chord === chord);
    if (!importing || !c) return;
    setImporting({
      ...importing,
      candidate: take
        ? reassignInImport(importing.candidate, isMac, c)
        : skipInImport(importing.candidate, id, chord),
    });
    setFocusNext({
      selector:
        '[data-action="reassign-import"], [data-action="confirm-import"]:not(:disabled), [data-action="cancel-import"]',
    });
  }

  function endImport(confirm: boolean) {
    if (confirm && importing?.mode) {
      onChange(applyImport(draft, importing.candidate, importBaseHalf, isMac));
    }
    setImporting(null);
    setFocusNext({ selector: '#settings-shortcuts-import' });
  }

  const q = query.trim().toLowerCase();
  const matches = (r: Row) =>
    !q ||
    r.label.toLowerCase().includes(q) ||
    shortcutsOf(r.id).some((c) => label(c).toLowerCase().includes(q));
  const pendingHolder = (id: string) =>
    pending?.holders.some((h) => h.id === id) ?? false;
  const locked = disabled || pending !== null || importing !== null;
  const fileLocked = locked || busy || capturing !== null;

  return (
    <div ref={rootRef} className="hv-shortcuts">
      <p className="settings-hint">
        Select <Icon name="plus" size={12} /> on a row, then press the keys.
        Changes apply when you save.
      </p>
      {status === 'loading' ? (
        <p className="settings-hint" role="status">
          Loading your shortcuts…
        </p>
      ) : null}
      {status === 'failed' ? (
        <div className="hv-shortcut-note" data-kind="refused" role="alert">
          <Icon name="state-error" size={12} />
          <span>
            Shortcuts can’t be edited: keymap.json could not be read. Fix or
            move the file, then reopen Settings.
          </span>
        </div>
      ) : null}
      <div className="hv-shortcuts-toolbar">
        <input
          id="settings-shortcuts-search"
          className="hv-input"
          type="search"
          placeholder="Search shortcuts"
          aria-label="Search shortcuts"
          value={query}
          disabled={importing !== null}
          onChange={(e) => setQuery(e.target.value)}
        />
        <Button
          id="settings-shortcuts-export"
          label="Export…"
          disabled={fileLocked}
          onClick={() => void exportKeymap()}
        />
        <Button
          id="settings-shortcuts-import"
          label="Import…"
          disabled={fileLocked}
          onClick={() => void startImport()}
        />
        <Button
          id="settings-shortcuts-reset-all"
          label="Reset all"
          disabled={
            locked || Object.keys(keymapHalf(draft, isMac)).length === 0
          }
          onClick={() => {
            onChange(resetHalf(draft, isMac));
            setNote(null);
            // Reset all disables itself; the search box is next in order.
            setFocusNext('search');
          }}
        />
      </div>
      {exported === draft ? (
        <p
          className="settings-hint"
          role="status"
          id="settings-shortcuts-exported"
        >
          Shortcuts exported.
        </p>
      ) : null}
      {importing ? (
        <ImportPreviewView
          importing={importing}
          conflicts={conflicts}
          titleOf={titleOf}
          onMode={chooseMode}
          onSettle={settleImport}
          onEnd={endImport}
        />
      ) : (
        <>
          {groups.map((g) => {
            const rows = g.rows.filter(matches);
            if (!rows.length) return null;
            return (
              <section key={g.title} className="hv-shortcuts-group">
                <h4>{g.title}</h4>
                <ul className="hv-shortcuts-list">
                  {rows.map((r) => {
                    const keys = shortcutsOf(r.id);
                    const displaced = displacedBy(r.id);
                    const conflict =
                      pending?.id === r.id
                        ? 'source'
                        : pendingHolder(r.id)
                          ? 'holder'
                          : null;
                    return (
                      <li
                        key={r.id}
                        className="hv-shortcut-row"
                        data-command={r.id}
                        data-conflict={conflict ?? undefined}
                      >
                        <span className="hv-shortcut-label">{r.label}</span>
                        <span className="hv-shortcut-keys">
                          {keys.map((c) => (
                            <span key={c} className="hv-shortcut-key">
                              <Kbd>{label(c)}</Kbd>
                              <IconButton
                                icon="x"
                                size={22}
                                label={`Remove ${label(c)} from ${r.label}`}
                                action="remove-shortcut"
                                disabled={locked}
                                onClick={() => remove(r.id, c)}
                              />
                            </span>
                          ))}
                          {keys.length === 0 && capturing !== r.id ? (
                            <span className="hv-shortcut-none">
                              No shortcut
                            </span>
                          ) : null}
                          {capturing === r.id ? (
                            <button
                              type="button"
                              className="hv-shortcut-capture"
                              data-own-keys
                              aria-label={`Press a shortcut for ${r.label}; Escape cancels`}
                              // biome-ignore lint/a11y/noAutofocus: the user just asked to press a key here
                              autoFocus
                              onBlur={() => setCapturing(null)}
                              onKeyDown={(e) => capture(r.id, e)}
                            >
                              Press a shortcut…
                            </button>
                          ) : (
                            <IconButton
                              icon="plus"
                              size={22}
                              label={`Add a shortcut for ${r.label}`}
                              action="add-shortcut"
                              disabled={locked}
                              onClick={() => {
                                setNote(null);
                                setCapturing(r.id);
                              }}
                            />
                          )}
                          {isOverridden(r.id) ? (
                            <IconButton
                              icon="rotate"
                              size={22}
                              label={`Reset ${r.label} to its default`}
                              action="reset-shortcut"
                              disabled={locked}
                              onClick={() => {
                                focusRow(r.id);
                                onChange(
                                  withShortcuts(draft, isMac, r.id, undefined),
                                );
                              }}
                            />
                          ) : null}
                        </span>
                        {conflict === 'source' && pending ? (
                          <div
                            className="hv-shortcut-note"
                            data-kind="conflict"
                            role="alert"
                          >
                            <Icon name="state-error" size={12} />
                            <span>
                              {label(pending.chord)} conflicts with{' '}
                              {pending.holders
                                .map((h) => titleOf(h.id))
                                .join(', ')}
                              .
                            </span>
                            <Button
                              label="Reassign"
                              kind="primary"
                              extra={{ 'data-action': 'reassign' }}
                              onClick={() => resolvePending(true)}
                            />
                            <Button
                              label="Cancel"
                              extra={{ 'data-action': 'cancel-reassign' }}
                              onClick={() => resolvePending(false)}
                            />
                          </div>
                        ) : null}
                        {conflict === 'holder' && pending ? (
                          <div
                            className="hv-shortcut-note"
                            data-kind="conflict"
                          >
                            <Icon name="state-error" size={12} />
                            <span>
                              {label(pending.chord)} conflicts with{' '}
                              {titleOf(pending.id)}.
                            </span>
                          </div>
                        ) : null}
                        {(r.refused ?? []).map((c) => {
                          const by = holdersOf(c, r.id).map((h) =>
                            titleOf(h.id),
                          );
                          return (
                            <div
                              key={c}
                              className="hv-shortcut-note"
                              data-kind="conflict"
                            >
                              <Icon name="state-error" size={12} />
                              <span>
                                {label(c)} conflicts with{' '}
                                {by.length
                                  ? by.join(', ')
                                  : 'a key Hive reserves'}
                                , so this command does not get it.
                              </span>
                              {by.length ? (
                                <Button
                                  label="Give it to the plugin"
                                  extra={{ 'data-action': 'give-to-plugin' }}
                                  disabled={locked}
                                  onClick={() => resolveRefused(r.id, c, true)}
                                />
                              ) : null}
                              <Button
                                label="Drop it"
                                extra={{ 'data-action': 'drop-plugin-key' }}
                                disabled={locked}
                                onClick={() => resolveRefused(r.id, c, false)}
                              />
                            </div>
                          );
                        })}
                        {displaced ? (
                          <div
                            className="hv-shortcut-note"
                            data-kind="displaced"
                          >
                            <Icon name="state-attention" size={12} />
                            <span>
                              Unbound: your shortcut for {titleOf(displaced.by)}{' '}
                              now uses its default key, {label(displaced.chord)}
                              .
                            </span>
                          </div>
                        ) : null}
                        {note?.id === r.id ? (
                          <div
                            className="hv-shortcut-note"
                            data-kind={note.kind}
                            role={note.kind === 'refused' ? 'alert' : 'status'}
                          >
                            <Icon
                              name={
                                note.kind === 'refused'
                                  ? 'state-error'
                                  : 'state-attention'
                              }
                              size={12}
                            />
                            <span>{note.text}</span>
                          </div>
                        ) : null}
                      </li>
                    );
                  })}
                </ul>
              </section>
            );
          })}
          <section className="hv-shortcuts-group" data-readonly="true">
            <h4>Inside a terminal</h4>
            <p className="settings-hint">
              These belong to the terminal and cannot be changed.
            </p>
            <ul className="hv-shortcuts-list">
              {terminalShortcuts(isMac).map((s) => (
                <li
                  key={s.label}
                  className="hv-shortcut-row"
                  data-readonly="true"
                >
                  <span className="hv-shortcut-label">{s.label}</span>
                  <span className="hv-shortcut-keys">
                    <Kbd>{s.keys}</Kbd>
                  </span>
                </li>
              ))}
            </ul>
          </section>
        </>
      )}
    </div>
  );
}

type ImportConflictView = ReturnType<typeof importConflicts>[number];

const SKIPPED: Partial<Record<string, true>> = {
  unknown: true,
  reserved: true,
  invalid: true,
  malformed: true,
};

/** The import preview: how it lands, a row per entry and key, and what
 * each conflict needs. Owns its keys (Escape cancels the import, not
 * Settings) through data-own-keys and the shortcut-capture key scope. */
function ImportPreviewView({
  importing,
  conflicts,
  titleOf,
  onMode,
  onSettle,
  onEnd,
}: {
  importing: Importing;
  conflicts: readonly ImportConflictView[];
  titleOf: (id: string) => string;
  onMode: (mode: ImportMode) => void;
  onSettle: (id: string, chord: string, take: boolean) => void;
  onEnd: (confirm: boolean) => void;
}) {
  const { preview, mode, candidate } = importing;
  const fileIds = new Set(preview.rows.map((r) => r.id));
  // Commands the file does not name that a Reassign changed.
  const madeRoom = Object.keys(candidate).filter((id) => !fileIds.has(id));
  const conflictOf = (id: string, chord?: string) =>
    chord === undefined
      ? undefined
      : conflicts.find((c) => c.id === id && c.chord === chord);
  const inCandidate = (id: string, chord?: string) =>
    Object.hasOwn(candidate, id) &&
    (chord === undefined || candidate[id].includes(chord));
  return (
    <section
      className="hv-shortcuts-group hv-shortcuts-import"
      aria-label="Import shortcuts"
      data-own-keys
      onKeyDown={(e) => {
        if (
          e.key === 'Escape' &&
          !e.metaKey &&
          !e.ctrlKey &&
          !e.altKey &&
          !e.shiftKey
        ) {
          e.preventDefault();
          e.stopPropagation();
          onEnd(false);
        }
      }}
    >
      <h4>Import shortcuts</h4>
      <div
        className="hv-shortcuts-import-mode"
        role="radiogroup"
        aria-label="How to import"
      >
        <label className="settings-check">
          <input
            type="radio"
            name="shortcut-import-mode"
            value="replace"
            checked={mode === 'replace'}
            onChange={() => onMode('replace')}
          />
          <span>
            Replace my shortcuts: use the file’s, and put every other command
            back to its default
          </span>
        </label>
        <label className="settings-check">
          <input
            type="radio"
            name="shortcut-import-mode"
            value="add"
            checked={mode === 'add'}
            onChange={() => onMode('add')}
          />
          <span>
            Add to my shortcuts: change only the commands the file names
          </span>
        </label>
      </div>
      {mode === null ? (
        <p className="settings-hint">
          Choose how to import to see what changes.
        </p>
      ) : (
        <ul className="hv-shortcuts-list">
          {preview.rows.map((r) => {
            const c = conflictOf(r.id, r.chord);
            const skippedByUser =
              !SKIPPED[r.status] &&
              r.status !== 'unbound' &&
              !inCandidate(r.id, r.chord);
            const state = c
              ? 'conflict'
              : SKIPPED[r.status] || skippedByUser
                ? 'skipped'
                : r.status;
            return (
              <li
                key={`${r.id}\u0000${r.chord ?? ''}\u0000${r.status}`}
                className="hv-shortcut-row hv-import-row"
                data-command={r.id}
                data-chord={r.chord}
                data-state={state}
              >
                <span className="hv-shortcut-label">{titleOf(r.id)}</span>
                <span className="hv-shortcut-keys">
                  {r.chord ? <Kbd>{label(r.chord)}</Kbd> : null}
                  {r.status === 'unbound' ? (
                    <span className="hv-shortcut-none">No shortcut</span>
                  ) : null}
                </span>
                {c ? (
                  <div className="hv-shortcut-note" data-kind="conflict">
                    <Icon name="state-error" size={12} />
                    <span>
                      Conflicts with{' '}
                      {c.holders.map((h) => titleOf(h.id)).join(', ')}.
                    </span>
                    <Button
                      label="Reassign"
                      kind="primary"
                      extra={{ 'data-action': 'reassign-import' }}
                      onClick={() => onSettle(r.id, r.chord as string, true)}
                    />
                    <Button
                      label="Skip"
                      extra={{ 'data-action': 'skip-import' }}
                      onClick={() => onSettle(r.id, r.chord as string, false)}
                    />
                  </div>
                ) : state === 'skipped' ? (
                  <div className="hv-shortcut-note" data-kind="refused">
                    <Icon name="state-error" size={12} />
                    <span>Skipped{r.reason ? `: ${r.reason}` : '.'}</span>
                  </div>
                ) : r.status === 'warn' || r.status === 'kept' ? (
                  <div className="hv-shortcut-note" data-kind="warn">
                    <Icon name="state-attention" size={12} />
                    <span>{r.reason}</span>
                  </div>
                ) : null}
              </li>
            );
          })}
          {madeRoom.map((id) => (
            <li
              key={id}
              className="hv-shortcut-row hv-import-row"
              data-command={id}
              data-state="made-room"
            >
              <span className="hv-shortcut-label">{titleOf(id)}</span>
              <span className="hv-shortcut-keys">
                {candidate[id].length ? (
                  candidate[id].map((k) => <Kbd key={k}>{label(k)}</Kbd>)
                ) : (
                  <span className="hv-shortcut-none">No shortcut</span>
                )}
              </span>
              <div className="hv-shortcut-note" data-kind="displaced">
                <Icon name="state-attention" size={12} />
                <span>Gives up a key to the import.</span>
              </div>
            </li>
          ))}
        </ul>
      )}
      {mode !== null && preview.rows.length === 0 ? (
        <div className="hv-shortcut-note" data-kind="warn" role="status">
          <Icon name="state-attention" size={12} />
          <span>
            This file has no shortcuts for {isMac ? 'macOS' : 'this platform'}.
            {mode === 'replace'
              ? ' Replacing would put every command back to its default.'
              : ' Adding it changes nothing.'}
          </span>
        </div>
      ) : null}
      <div className="hv-shortcuts-import-actions">
        <span className="settings-hint">[esc] cancel import</span>
        <Button
          label="Confirm import"
          kind="primary"
          disabled={mode === null || conflicts.length > 0}
          extra={{ 'data-action': 'confirm-import' }}
          onClick={() => onEnd(true)}
        />
        <Button
          label="Cancel import"
          extra={{ 'data-action': 'cancel-import' }}
          onClick={() => onEnd(false)}
        />
      </div>
    </section>
  );
}
