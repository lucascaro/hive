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

import { useEffect, useMemo, useRef, useState } from 'react';
import { listCommands } from '../../app/command-registry.js';
import { setShortcutCapture } from '../../app/keymap-sync.js';
import { pluginCommands } from '../../app/plugin-host.js';
import {
  DEFAULT_APP_BINDINGS,
  effectiveFor,
  keymapHalf,
  shortcutsIn,
  type Keymap,
} from '../../lib/bindings.js';
import { chordsFor, chordsOverlap } from '../../lib/chord.js';
import { chordLabel } from '../../lib/chord-label.js';
import {
  captureChord,
  checkChord,
  reassign,
  resetHalf,
  withShortcuts,
  type Holder,
} from '../../lib/keymap-edit.js';
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

export interface ShortcutsPanelProps {
  draft: Keymap;
  onChange: (next: Keymap) => void;
  /** Editing is off while keymap.json is loading or failed to load. */
  disabled: boolean;
  /** Called with true while a conflict waits for an answer. */
  onBlockedChange: (blocked: boolean) => void;
}

const label = (chord: string) => chordLabel(chord, isMac);

export function ShortcutsPanel({
  draft,
  onChange,
  disabled,
  onBlockedChange,
}: ShortcutsPanelProps) {
  const pluginUI = useAppStore((s) => s.pluginUI);
  const plugins = useAppStore((s) => s.plugins);
  const [query, setQuery] = useState('');
  const [capturing, setCapturing] = useState<string | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const [note, setNote] = useState<Note | null>(null);
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
    { command: string; action: string } | 'search' | null
  >(null);
  const focusRow = (command: string, action = 'add-shortcut') =>
    setFocusNext({ command, action });
  useEffect(() => {
    const t = focusNext;
    if (!t) return;
    setFocusNext(null);
    const root = rootRef.current;
    const row =
      t === 'search'
        ? undefined
        : [
            ...(root?.querySelectorAll<HTMLElement>('.hv-shortcut-row') ?? []),
          ].find((r) => r.dataset.command === t.command);
    const target =
      t === 'search'
        ? null
        : row?.querySelector<HTMLElement>(
            `[data-action="${t.action}"]:not(:disabled)`,
          );
    // The row can be filtered out by the search it no longer matches.
    (
      target ?? root?.querySelector<HTMLElement>('#settings-shortcuts-search')
    )?.focus();
  }, [focusNext]);

  useEffect(
    () => onBlockedChange(pending !== null),
    [pending, onBlockedChange],
  );
  // The menu is suspended exactly while a capture button is on screen. Not
  // driven by focus events: removing a focused button fires no blur. The
  // button blurs (window switch, click away) into capturing = null.
  useEffect(() => setShortcutCapture(capturing !== null), [capturing]);
  useEffect(() => () => setShortcutCapture(false), []);

  const resolved = useMemo(
    () => pluginCommands(pluginUI, draft),
    [pluginUI, draft],
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

  const q = query.trim().toLowerCase();
  const matches = (r: Row) =>
    !q ||
    r.label.toLowerCase().includes(q) ||
    shortcutsOf(r.id).some((c) => label(c).toLowerCase().includes(q));
  const pendingHolder = (id: string) =>
    pending?.holders.some((h) => h.id === id) ?? false;
  const locked = disabled || pending !== null;

  return (
    <div ref={rootRef} className="hv-shortcuts">
      <p className="settings-hint">
        Select <Icon name="plus" size={12} /> on a row, then press the keys.
        Changes apply when you save.
      </p>
      <div className="hv-shortcuts-toolbar">
        <input
          id="settings-shortcuts-search"
          className="hv-input"
          type="search"
          placeholder="Search shortcuts"
          aria-label="Search shortcuts"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
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
                        <span className="hv-shortcut-none">No shortcut</span>
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
                          {pending.holders.map((h) => titleOf(h.id)).join(', ')}
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
                      <div className="hv-shortcut-note" data-kind="conflict">
                        <Icon name="state-error" size={12} />
                        <span>
                          {label(pending.chord)} conflicts with{' '}
                          {titleOf(pending.id)}.
                        </span>
                      </div>
                    ) : null}
                    {(r.refused ?? []).map((c) => {
                      const by = holdersOf(c, r.id).map((h) => titleOf(h.id));
                      return (
                        <div
                          key={c}
                          className="hv-shortcut-note"
                          data-kind="conflict"
                        >
                          <Icon name="state-error" size={12} />
                          <span>
                            {label(c)} conflicts with{' '}
                            {by.length ? by.join(', ') : 'a key Hive reserves'},
                            so this command does not get it.
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
                      <div className="hv-shortcut-note" data-kind="displaced">
                        <Icon name="state-attention" size={12} />
                        <span>
                          Unbound: your shortcut for {titleOf(displaced.by)} now
                          uses its default key, {label(displaced.chord)}.
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
            <li key={s.label} className="hv-shortcut-row" data-readonly="true">
              <span className="hv-shortcut-label">{s.label}</span>
              <span className="hv-shortcut-keys">
                <Kbd>{s.keys}</Kbd>
              </span>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
