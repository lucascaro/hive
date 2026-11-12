// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// ---------- command palette ----------
//
// React port of src/app/modals/command-palette.ts. Not a `hv-dialog`
// (no ModalShell): index.html gives it `#command-palette` bare, and this
// renders its two static children — the input and the results list —
// directly, since they now live inside React's tree instead of the DOM.

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useShortcutLabel } from '../../app/bindings.js';
import {
  activatePaletteSession,
  closeCommandPalette,
  type PaletteMode,
  paletteCommands,
} from '../../app/modals/command-palette.js';
import { finderRows } from '../../lib/session-finder.js';
import { sessionState, stateTooltip } from '../../lib/session-state.js';
import { useAppStore } from '../../store/store.js';
import { StateIcon } from '../Icon.js';
import { Kbd } from '../Kbd.js';

export function CommandPalette({
  root,
}: {
  root: HTMLElement | null;
}): ReactNode {
  const entry = useAppStore((s) =>
    s.modals.find((m) => m.id === 'command-palette'),
  );

  // #command-palette sits outside React's tree, so its open/closed class
  // is applied here — a passive effect would paint one stale frame with
  // the backdrop up before the class caught up.
  const mode = entry?.mode ?? 'commands';
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
    root?.setAttribute(
      'aria-label',
      mode === 'sessions' ? 'Go to session' : 'Command palette',
    );
  }, [root, entry, mode]);

  // The outside-click close is attached once and stays live even while
  // the palette is closed, exactly as initCommandPalette() attached it.
  // The open flag is read through a ref (same shape as Launcher's) so
  // the listener never has to be torn down and reattached per open.
  const openRef = useRef(!!entry);
  openRef.current = !!entry;
  useEffect(() => {
    if (!root) return;
    const el: HTMLElement = root;
    const onMouseDown = (e: MouseEvent) => {
      if (!openRef.current) return;
      if (!el.contains(e.target as Node)) closeCommandPalette();
    };
    document.addEventListener('mousedown', onMouseDown);
    return () => document.removeEventListener('mousedown', onMouseDown);
  }, [root]);

  if (!root) return null;
  // Rendered whether or not the palette is open: the search box and the
  // list were static children of #command-palette in index.html, and
  // a11y assertions (ux-polish.spec.ts) read the input's aria-label at
  // boot. Opening only drops the root's `hidden` class — exactly what
  // openCommandPalette() did.
  //
  // The key is the opening's generation, so a remount is what resets the
  // query and the selection — the per-open state the imperative version
  // got by clearing paletteInput.value and paletteState.selected.
  return (
    <CommandPaletteBody
      key={entry?.seq ?? 0}
      root={root}
      open={!!entry}
      mode={entry?.mode ?? 'commands'}
    />
  );
}

// One row either list renders; `run` is what Enter and a click do.
interface PaletteRow {
  key: string;
  run: () => void;
  content: ReactNode;
}

function CommandPaletteBody({
  root,
  open,
  mode,
}: {
  root: HTMLElement;
  open: boolean;
  mode: PaletteMode;
}): ReactNode {
  const sessions = useAppStore((s) => s.sessions);
  const projects = useAppStore((s) => s.projects);
  const back = useAppStore((s) => s.nav.back);
  const activeId = useAppStore((s) => s.activeId);
  const otherChord = useShortcutLabel(
    mode === 'commands' ? 'session-finder' : 'command-palette',
  );
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);

  // Focus only when this mount IS an opening. A passive effect, like the
  // other modals': the root's `hidden` class comes off in the parent's
  // layout effect, and focusing a display:none input does nothing.
  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  const filtered: PaletteRow[] = useMemo(() => {
    if (mode === 'sessions') {
      const byId = new Map(sessions.map((s) => [s.id, s]));
      return finderRows({ sessions, projects, back, activeId, query }).map(
        (r) => {
          const s = byId.get(r.id);
          const state = s ? sessionState(s) : undefined;
          return {
            key: r.id,
            run: () => activatePaletteSession(r.id),
            content: (
              <>
                {state && s ? (
                  <StateIcon
                    state={state}
                    className="palette-state"
                    detail={stateTooltip(s, state)}
                  />
                ) : null}
                <span className="palette-name">
                  {r.name || r.title || r.id}
                </span>
                {r.id === activeId ? (
                  <span className="palette-current">current</span>
                ) : null}
                {r.title && r.title !== r.name ? (
                  <span className="palette-title">{r.title}</span>
                ) : null}
                {r.agent ? (
                  <span className="palette-agent">{r.agent}</span>
                ) : null}
                <span className="palette-meta">
                  {r.project}
                  {r.branch ? ` · ${r.branch}` : ''}
                </span>
              </>
            ),
          };
        },
      );
    }
    const q = query.trim().toLowerCase();
    return paletteCommands()
      .filter(
        (c) =>
          !q ||
          c.name.toLowerCase().includes(q) ||
          c.shortcut.toLowerCase().includes(q),
      )
      .map((c) => ({
        key: c.id,
        run: () => c.run(),
        content: (
          <>
            <span className="palette-name">{c.name}</span>
            <span className="palette-shortcut">
              {c.shortcut ? <Kbd>{c.shortcut}</Kbd> : null}
            </span>
          </>
        ),
      }));
  }, [mode, query, sessions, projects, back, activeId]);

  // Derived rather than chased with another effect: whatever `selected`
  // drifted to (a query narrowing the list is the only way it can),
  // this is the "reset to 0" the legacy renderPalette() did inline.
  const activeIndex = selected >= filtered.length ? 0 : selected;

  function activate(i: number) {
    const c = filtered[i];
    if (!c) return;
    // A session row closes and jumps itself (activatePaletteSession); a
    // command closes first and runs deferred, because some commands open
    // another modal that owns focus and running before the close finishes
    // lets this one steal it right back.
    if (mode === 'sessions') return c.run();
    closeCommandPalette();
    setTimeout(() => c.run(), 0);
  }

  // Focus stays in the input, so the browser never scrolls to the selected
  // row on its own. `query` re-runs it: a new query can reorder the list so
  // the same key sits elsewhere.
  const selectedKey = filtered[activeIndex]?.key;
  // biome-ignore lint/correctness/useExhaustiveDependencies: see above
  useEffect(() => {
    if (selectedKey === undefined) return;
    listRef.current
      ?.querySelector('[data-selected]')
      ?.scrollIntoView?.({ block: 'nearest' });
  }, [selectedKey, query]);

  // Re-attached every render, same as Settings' Enter handler, so it
  // closes over the current filtered list and activeIndex rather than a
  // stale one from mount.
  //
  // Escape is NOT here. It belongs to the palette's scope in KEY_SCOPES (app/key-scopes.ts), which reads
  // the store rather than the focus location: this listener sits on
  // #command-palette and only fires for keys typed inside it, so once
  // anything moves focus out of the search box the palette would have no
  // way to close (that shipped as a CI failure). The ladder's handler is
  // capture-phase on window and stops propagation, so a branch here
  // could never run for Escape anyway.
  useEffect(() => {
    if (!open) return;
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey)) {
        e.preventDefault();
        e.stopPropagation();
        if (filtered.length === 0) return;
        setSelected((activeIndex + 1) % filtered.length);
        return;
      }
      if (e.key === 'ArrowUp' || (e.key === 'Tab' && e.shiftKey)) {
        e.preventDefault();
        e.stopPropagation();
        if (filtered.length === 0) return;
        setSelected((activeIndex - 1 + filtered.length) % filtered.length);
        return;
      }
      if (e.key === 'Enter') {
        e.preventDefault();
        e.stopPropagation();
        activate(activeIndex);
      }
    }
    root.addEventListener('keydown', onKeyDown);
    return () => root.removeEventListener('keydown', onKeyDown);
  });

  return (
    <>
      <input
        id="command-palette-input"
        ref={inputRef}
        type="text"
        role="combobox"
        aria-expanded={true}
        aria-controls="command-palette-list"
        aria-autocomplete="list"
        aria-activedescendant={
          filtered[activeIndex]
            ? `palette-opt-${filtered[activeIndex].key}`
            : undefined
        }
        placeholder={
          (mode === 'commands'
            ? 'Search actions, shortcuts…'
            : 'Search name, title, branch or project…') +
          (otherChord
            ? ` (${otherChord} for ${mode === 'commands' ? 'sessions' : 'commands'})`
            : '')
        }
        aria-label={mode === 'commands' ? 'Search commands' : 'Search sessions'}
        autoComplete="off"
        value={query}
        // Only the query moves. The selection is left alone and clamped
        // on render — renderPalette() reset it to 0 only when the
        // narrowed list no longer reached it, and a row the user had
        // already picked must survive a keystroke that still matches it.
        onChange={(e) => setQuery(e.target.value)}
      />
      <div
        id="command-palette-list"
        role="listbox"
        aria-label={mode === 'commands' ? 'Commands' : 'Sessions'}
        ref={listRef}
      >
        {filtered.length === 0 && mode === 'sessions' ? (
          <p className="palette-empty" id="session-finder-nomatch">
            {query ? `No session matches “${query}”.` : 'No sessions.'}
          </p>
        ) : null}
        {filtered.map((c, i) => (
          // Keys for the whole list are handled on the palette root (a
          // row is never focused, the selection is a data attribute), so
          // per-row key handlers would double-fire the activation.
          // biome-ignore lint/a11y/noStaticElementInteractions: see above
          // biome-ignore lint/a11y/useKeyWithClickEvents: see above
          <div
            key={c.key}
            id={`palette-opt-${c.key}`}
            className="palette-item"
            role="option"
            tabIndex={-1}
            aria-selected={i === activeIndex}
            data-sid={mode === 'sessions' ? c.key : undefined}
            aria-current={
              mode === 'sessions' && c.key === activeId ? 'true' : undefined
            }
            data-selected={i === activeIndex ? '' : undefined}
            onMouseEnter={() => setSelected(i)}
            onClick={() => activate(i)}
          >
            {c.content}
          </div>
        ))}
      </div>
    </>
  );
}
