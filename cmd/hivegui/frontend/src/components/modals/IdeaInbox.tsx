// ---------- idea inbox (⇧⌘I) ----------
//
// The open ideas captured for one project, newest first. Same modal
// shell and the same row shape as the worktree browser, because it is
// the same job: a list of things with a couple of actions each.
//
// Nothing here holds a copy of an idea. Every action calls the daemon
// and the row re-renders when the IDEA_EVENT lands, so two windows
// never disagree about what is still open.

import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import {
  beginInlineRename,
  cancelInlineRenameFor,
} from '../../app/inline-rename.js';
import {
  closeIdeaInbox,
  confirmAndDeleteIdea,
  editIdeaText,
  markIdeaDone,
} from '../../app/modals/idea-inbox.js';
import { switchTo } from '../../app/view.js';
import { flashStatus } from '../../app/dom.js';
import type { IdeaInfo } from '../../app/state.js';
import {
  ideaTextTooLong,
  MAX_IDEA_TEXT,
  relativeAge,
} from '../../lib/ideas.js';
import { isMac } from '../../lib/platform.js';
import { mod } from '../../lib/shortcuts.js';
import { openIdeasOf, useAppStore } from '../../store/store.js';
import { ModalShell } from './ModalShell.js';

// The validate predicate for an inline edit. Says why from here: the
// rename helper refuses the commit but has no status line of its own.
function tooLongForAnIdea(next: string): boolean {
  if (!ideaTextTooLong(next)) return false;
  flashStatus(`idea is too long (max ${MAX_IDEA_TEXT / 1024} KiB)`, true);
  return true;
}

export function IdeaInbox({ root }: { root: HTMLElement | null }): ReactNode {
  const entry = useAppStore((s) => s.modals.find((m) => m.id === 'idea-inbox'));

  // #idea-inbox sits outside React's tree — see Worktrees.tsx for why
  // this is a layout effect rather than a passive one.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  if (!entry || !root) return null;
  // Remounted per opening: an in-progress edit belongs to the panel
  // that started it, not to the next project the user opens.
  return (
    <IdeaInboxPanel
      key={entry.seq}
      root={root}
      projectId={entry.projectId}
      projectName={entry.projectName}
    />
  );
}

function IdeaInboxPanel({
  root,
  projectId,
  projectName,
}: {
  root: HTMLElement;
  projectId: string;
  projectName: string;
}): ReactNode {
  // Raw slice selected, filtered in render: see openIdeasOf's note.
  const ideas = openIdeasOf(
    useAppStore((s) => s.ideas),
    projectId,
  );
  // The id of the row being edited, if any. The row still renders — its
  // text area is left empty for the imperative input to mount into.
  const [editing, setEditing] = useState<string | null>(null);

  // Mount-only: this effect IS the open. The close button is where
  // focus lands, same as every other dialog.
  useEffect(() => {
    document.getElementById('idea-inbox-close')?.focus();
  }, []);

  return (
    <ModalShell
      id="idea-inbox"
      root={root}
      title="Ideas"
      titleSuffix={
        <span id="idea-inbox-project">
          {projectName ? `· ${projectName}` : ''}
        </span>
      }
      size="lg"
      onClose={closeIdeaInbox}
      // patterns.md › Keyboard hints: `[…]` for symbols, `(…)` for letters.
      hints={[
        { keys: '[esc]', label: 'close' },
        { keys: `[${mod(isMac, 'i')}]`, label: 'capture another' },
      ]}
    >
      <div className="idea-list" id="idea-inbox-list">
        {ideas.length === 0 ? (
          <p className="idea-empty" id="idea-inbox-empty">
            Nothing captured yet. Press {mod(isMac, 'I')} — or run{' '}
            <code>hived idea add …</code> inside a session — to file one.
          </p>
        ) : (
          ideas.map((idea) => (
            <IdeaRow
              key={idea.id}
              idea={idea}
              editing={editing === idea.id}
              onStartEdit={() => setEditing(idea.id)}
              onEndEdit={() => setEditing(null)}
            />
          ))
        )}
      </div>
    </ModalShell>
  );
}

function IdeaRow({
  idea,
  editing,
  onStartEdit,
  onEndEdit,
}: {
  idea: IdeaInfo;
  editing: boolean;
  onStartEdit: () => void;
  onEndEdit: () => void;
}): ReactNode {
  const mainRef = useRef<HTMLDivElement | null>(null);
  // The session this idea was filed from, if it is still around. A
  // breadcrumb only — the idea belongs to the project either way, so a
  // closed session leaves the row otherwise unchanged.
  const source = useAppStore((s) =>
    s.sessions.find((x) => x.id === idea.source_session_id),
  );
  // The session started FROM this idea (status=started). Distinct from
  // the source: one is where it was noticed, the other is where it is
  // being worked on.
  const started = useAppStore((s) =>
    s.sessions.find((x) => x.id === idea.session_id),
  );

  // The edit is the shared imperative helper for the two reasons
  // Worktrees.tsx names: keyboard.ts asks inlineRenameActive() FIRST,
  // so Escape cancels the edit instead of closing the panel; and the
  // commit/cancel/blur rules stay identical to every other rename in
  // the app. See that file's longer note — including why this effect is
  // keyed on `editing` alone and reads the rest through a ref.
  const opts = useRef({ idea, onEndEdit });
  opts.current = { idea, onEndEdit };
  useEffect(() => {
    if (!editing) return;
    const main = mainRef.current;
    if (!main) return;
    const { idea: row, onEndEdit: done } = opts.current;
    const input = beginInlineRename({
      value: row.text,
      className: 'idea-edit',
      mount: (input) => {
        input.setAttribute('aria-label', 'Idea text');
        main.appendChild(input);
      },
      unmount: (input) => input.remove(),
      // The daemon applies the same 4 KiB cap to the update path and
      // REJECTS rather than truncates, and nothing here awaits the
      // answer — so an over-long edit that was allowed to commit would
      // tear the editor down, revert the row to the stale text and be
      // gone. Refusing keeps the editor open on it instead.
      validate: (next) => !tooLongForAnIdea(next),
      onCommit: (next) => editIdeaText(row.id, next),
      onDone: done,
    });
    // A row that unmounts mid-edit would otherwise leave
    // inlineRenameActive() true forever — see Worktrees.tsx.
    return () => {
      cancelInlineRenameFor(input);
    };
  }, [editing]);

  return (
    <div className="idea-row" data-kind={idea.kind} data-id={idea.id}>
      <span className="idea-kind" data-kind={idea.kind}>
        {idea.kind}
      </span>
      <div className="idea-main" ref={mainRef}>
        {editing ? null : (
          <>
            <span className="idea-text">{idea.text}</span>
            <span className="idea-meta">
              <span className="idea-age">{relativeAge(idea.created)}</span>
              {source ? (
                <span className="idea-source">
                  from {source.name ?? 'session'}
                </span>
              ) : null}
              {started ? (
                // A link, not a label: the whole reason to record the
                // session is to be able to get back to it.
                <button
                  type="button"
                  className="idea-started"
                  onClick={() => {
                    closeIdeaInbox();
                    switchTo(started.id);
                  }}
                >
                  in {started.name ?? 'session'}
                </button>
              ) : null}
            </span>
          </>
        )}
      </div>
      <div className="idea-actions">
        <RowButton
          label="Edit"
          title="Edit the note"
          disabled={editing}
          onClick={onStartEdit}
        />
        <RowButton
          label="Done"
          title="Take it out of the inbox — the note is kept"
          onClick={() => markIdeaDone(idea)}
        />
        <RowButton
          label="Delete"
          title="Discard the note — this cannot be undone"
          danger
          onClick={() => void confirmAndDeleteIdea(idea)}
        />
      </div>
    </div>
  );
}

function RowButton({
  label,
  title,
  onClick,
  danger,
  disabled,
}: {
  label: string;
  title: string;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
}): ReactNode {
  return (
    <button
      type="button"
      title={title}
      className={danger ? 'danger' : undefined}
      disabled={disabled}
      onClick={onClick}
    >
      {label}
    </button>
  );
}
