// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// ---------- session finder (spec 544) ----------
//
// "Go to session": a filter box over every session in every project.
// Matching and ordering are lib/session-finder.ts; this is the view. The
// rows are derived from the store on every render, so a rename or a
// changed branch shows its new value with nothing to refresh. Enter jumps
// (app/modals/session-finder.ts); Escape and the opening chord's mirror
// are the key scope's (app/key-scopes.ts).

import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useShortcutLabel } from '../../app/bindings.js';
import {
  activateFinderSession,
  closeSessionFinder,
} from '../../app/modals/session-finder.js';
import { finderRows } from '../../lib/session-finder.js';
import { sessionState, stateTooltip } from '../../lib/session-state.js';
import { useAppStore } from '../../store/store.js';
import { StateIcon } from '../Icon.js';
import { ModalShell } from './ModalShell.js';

export function SessionFinder({
  root,
}: {
  root: HTMLElement | null;
}): ReactNode {
  const entry = useAppStore((s) =>
    s.modals.find((m) => m.id === 'session-finder'),
  );

  // #session-finder sits outside React's tree — see Worktrees.tsx for why
  // this is a layout effect rather than a passive one.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  if (!entry || !root) return null;
  return <FinderPanel key={entry.seq} root={root} />;
}

function FinderPanel({ root }: { root: HTMLElement }): ReactNode {
  const sessions = useAppStore((s) => s.sessions);
  const projects = useAppStore((s) => s.projects);
  const back = useAppStore((s) => s.nav.back);
  const activeId = useAppStore((s) => s.activeId);
  const chord = useShortcutLabel('session-finder');
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  const rows = finderRows({ sessions, projects, back, activeId, query });
  const byId = new Map(sessions.map((s) => [s.id, s]));
  const active = selected >= rows.length ? 0 : selected;

  // Mount-only: this effect IS the open.
  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Re-attached every render so it closes over the current list.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.isComposing) return;
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        e.stopPropagation();
        if (rows.length) setSelected((active + 1) % rows.length);
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        e.stopPropagation();
        if (rows.length) setSelected((active - 1 + rows.length) % rows.length);
      } else if (e.key === 'Enter') {
        e.preventDefault();
        e.stopPropagation();
        const r = rows[active];
        if (r) activateFinderSession(r.id);
      }
    }
    root.addEventListener('keydown', onKeyDown);
    return () => root.removeEventListener('keydown', onKeyDown);
  });

  return (
    <ModalShell
      id="session-finder"
      root={root}
      title="Go to session"
      size="lg"
      onClose={closeSessionFinder}
      hints={[
        { keys: '[↑↓]', label: 'select' },
        { keys: '[enter]', label: 'go' },
        { keys: '[esc]', label: 'cancel' },
        ...(chord ? [{ keys: `(${chord})`, label: 'toggle' }] : []),
      ]}
    >
      <input
        id="session-finder-input"
        ref={inputRef}
        type="text"
        className="hv-input"
        placeholder="Search name, title, branch or project…"
        aria-label="Search sessions"
        autoComplete="off"
        value={query}
        onChange={(e) => {
          setQuery(e.target.value);
          setSelected(0);
        }}
      />
      <div className="finder-list" id="session-finder-list" role="listbox">
        {rows.length === 0 ? (
          <p className="finder-empty" id="session-finder-nomatch">
            {query ? `No session matches “${query}”.` : 'No sessions.'}
          </p>
        ) : (
          rows.map((r, i) => {
            const s = byId.get(r.id);
            const state = s ? sessionState(s) : undefined;
            return (
              // Keys for the list are handled on the dialog root, not per
              // row (a row is never focused), so a row handler would
              // double-fire the jump.
              // biome-ignore lint/a11y/useKeyWithClickEvents: see above
              <div
                key={r.id}
                className="finder-row"
                role="option"
                tabIndex={-1}
                aria-selected={i === active}
                data-sid={r.id}
                data-selected={i === active ? '' : undefined}
                onMouseEnter={() => setSelected(i)}
                onClick={() => activateFinderSession(r.id)}
              >
                {state && s ? (
                  <StateIcon
                    state={state}
                    className="finder-row__state"
                    detail={stateTooltip(s, state)}
                  />
                ) : null}
                <span className="finder-row__name">
                  {r.name || r.title || r.id}
                </span>
                {r.agent ? (
                  <span className="finder-row__agent">{r.agent}</span>
                ) : null}
                <span className="finder-row__meta">
                  {r.project}
                  {r.branch ? ` · ${r.branch}` : ''}
                </span>
              </div>
            );
          })
        )}
      </div>
    </ModalShell>
  );
}
