// ---------- archived sessions (spec 518) ----------
//
// Every session the user has put away, grouped by project, with a filter
// box. Enter unarchives the highlighted row; ↑/↓ move it. Nothing here
// holds a copy of an archived session — the list is the daemon's
// (archived:list), so two windows never disagree about what is archived.

import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import {
  confirmAndDeleteArchived,
  unarchiveSession,
} from '../../app/archive.js';
import { closeArchivedSessions } from '../../app/modals/archived-sessions.js';
import type { ArchivedSessionInfo } from '../../app/state.js';
import { filterArchived, groupArchived } from '../../lib/archived.js';
import { relativeAge } from '../../lib/ideas.js';
import { useAppStore } from '../../store/store.js';
import { ModalShell } from './ModalShell.js';

export function ArchivedSessions({
  root,
}: {
  root: HTMLElement | null;
}): ReactNode {
  const entry = useAppStore((s) =>
    s.modals.find((m) => m.id === 'archived-sessions'),
  );

  // #archived-sessions sits outside React's tree — see Worktrees.tsx for
  // why this is a layout effect rather than a passive one.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  if (!entry || !root) return null;
  return <ArchivedPanel key={entry.seq} root={root} />;
}

function ArchivedPanel({ root }: { root: HTMLElement }): ReactNode {
  const archived = useAppStore((s) => s.archived);
  const projects = useAppStore((s) => s.projects);
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  // The snapshot taken at archive time wins: it is what the user saw when
  // they put the session away, and it survives the project's deletion.
  const projectName = (a: ArchivedSessionInfo): string =>
    a.project_name ||
    projects.find((p) => p.id === a.project_id)?.name ||
    'No project';

  // Dozens of rows at most, so this is recomputed per render rather than
  // memoised around a closure over `projects`.
  const filtered = filterArchived(archived, query, projectName);
  const groups = groupArchived(filtered, projectName);
  // Selection walks the rows in the order they are drawn (grouped), not
  // the daemon's flat order.
  const flat = groups.flatMap((g) => g.items);
  const active = selected >= flat.length ? 0 : selected;

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
        if (flat.length) setSelected((active + 1) % flat.length);
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        e.stopPropagation();
        if (flat.length) setSelected((active - 1 + flat.length) % flat.length);
      } else if (e.key === 'Enter') {
        // A button has its own Enter.
        if ((e.target as Element | null)?.closest?.('button')) return;
        e.preventDefault();
        e.stopPropagation();
        const a = flat[active];
        if (a) restore(a);
      }
    }
    root.addEventListener('keydown', onKeyDown);
    return () => root.removeEventListener('keydown', onKeyDown);
  });

  function restore(a: ArchivedSessionInfo) {
    closeArchivedSessions();
    unarchiveSession(a.session_id);
  }

  let rowIndex = -1;
  return (
    <ModalShell
      id="archived-sessions"
      root={root}
      title="Archived sessions"
      size="lg"
      onClose={closeArchivedSessions}
      hints={[
        { keys: '[↑↓]', label: 'select' },
        { keys: '[enter]', label: 'unarchive' },
        { keys: '[esc]', label: 'close' },
      ]}
    >
      <input
        id="archived-sessions-input"
        ref={inputRef}
        type="text"
        className="hv-input"
        placeholder="Filter by name, project, agent or branch…"
        aria-label="Filter archived sessions"
        autoComplete="off"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      <div className="archived-list" id="archived-sessions-list">
        {archived.length === 0 ? (
          <p className="archived-empty" id="archived-sessions-empty">
            Nothing archived. Run “Archive Session” from the command palette to
            put the focused session away and keep it for later.
          </p>
        ) : flat.length === 0 ? (
          <p className="archived-empty" id="archived-sessions-nomatch">
            No archived session matches “{query}”.
          </p>
        ) : (
          groups.map((g) => (
            <section
              key={g.key}
              className="archived-group"
              data-project={g.key}
            >
              <h4 className="archived-group-name">{g.name}</h4>
              {g.items.map((a) => {
                rowIndex += 1;
                const i = rowIndex;
                return (
                  <ArchivedRow
                    key={a.session_id}
                    a={a}
                    selected={i === active}
                    onHover={() => setSelected(i)}
                    onRestore={() => restore(a)}
                  />
                );
              })}
            </section>
          ))
        )}
      </div>
    </ModalShell>
  );
}

function ArchivedRow({
  a,
  selected,
  onHover,
  onRestore,
}: {
  a: ArchivedSessionInfo;
  selected: boolean;
  onHover: () => void;
  onRestore: () => void;
}): ReactNode {
  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: the row only tracks hover; its actions are the buttons
    <div
      className="archived-row"
      data-id={a.session_id}
      data-selected={selected ? '' : undefined}
      onMouseEnter={onHover}
    >
      <div className="archived-main">
        <span className="archived-name">{a.name}</span>
        <span className="archived-meta">
          {a.agent ? <span className="archived-agent">{a.agent}</span> : null}
          {a.worktree_branch ? (
            <span className="archived-branch">{a.worktree_branch}</span>
          ) : null}
          <span className="archived-age" title={a.archived_at}>
            archived {relativeAge(a.archived_at) || 'recently'}
          </span>
        </span>
      </div>
      <div className="archived-actions">
        <button
          type="button"
          title="Bring this session back"
          onClick={onRestore}
        >
          Unarchive
        </button>
        <button
          type="button"
          className="danger"
          title="Discard this archived session for good"
          onClick={() => void confirmAndDeleteArchived(a)}
        >
          Delete
        </button>
      </div>
    </div>
  );
}
