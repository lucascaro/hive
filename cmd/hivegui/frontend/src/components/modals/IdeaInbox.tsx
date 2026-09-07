// ---------- idea inbox (⇧⌘I) ----------
//
// The open ideas captured for one project, newest first. Same modal
// shell and the same row shape as the worktree browser, because it is
// the same job: a list of things with a couple of actions each.
//
// Nothing here holds a copy of an idea. Every action calls the daemon
// and the row re-renders when the IDEA_EVENT lands, so two windows
// never disagree about what is still open.

import { useEffect, useLayoutEffect } from 'react';
import type { ReactNode } from 'react';
import {
  closeIdeaInbox,
  confirmAndDeleteIdea,
  editIdea,
  markIdeaDone,
  startSessionFromIdea,
} from '../../app/modals/idea-inbox.js';
import { switchTo } from '../../app/view.js';
import type { IdeaInfo } from '../../app/state.js';
import { relativeAge } from '../../lib/ideas.js';
import { isMac } from '../../lib/platform.js';
import { mod } from '../../lib/shortcuts.js';
import { openIdeasOf, useAppStore } from '../../store/store.js';
import { ModalShell } from './ModalShell.js';

export function IdeaInbox({ root }: { root: HTMLElement | null }): ReactNode {
  const entry = useAppStore((s) => s.modals.find((m) => m.id === 'idea-inbox'));

  // #idea-inbox sits outside React's tree — see Worktrees.tsx for why
  // this is a layout effect rather than a passive one.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  if (!entry || !root) return null;
  // Remounted per opening: the panel's per-open state belongs to the
  // opening that made it, not to the next project the user opens.
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
          ideas.map((idea) => <IdeaRow key={idea.id} idea={idea} />)
        )}
      </div>
    </ModalShell>
  );
}

function IdeaRow({ idea }: { idea: IdeaInfo }): ReactNode {
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

  return (
    <div className="idea-row" data-kind={idea.kind} data-id={idea.id}>
      <span className="idea-kind" data-kind={idea.kind}>
        {idea.kind}
      </span>
      <div className="idea-main">
        <span className="idea-text">{idea.text}</span>
        <span className="idea-meta">
          <span className="idea-age">{relativeAge(idea.created)}</span>
          {source ? (
            <span className="idea-source">from {source.name ?? 'session'}</span>
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
      </div>
      <div className="idea-actions">
        {/* First, and the only non-secondary action here: the inbox
            exists so a note can become work. Offered once — an idea
            already being worked on has its session link above. */}
        {started ? null : (
          <RowButton
            label="Start session"
            title="Open the launcher with this note as the opening prompt"
            // The launcher closes itself on any document click outside
            // it, and this click is still travelling when it opens.
            // Openers opt out of that by name (Launcher.tsx).
            opensLauncher
            onClick={() => startSessionFromIdea(idea)}
          />
        )}
        <RowButton
          label="Edit"
          title="Correct the note, its kind, or the project it is filed under"
          onClick={() => editIdea(idea)}
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
  opensLauncher,
}: {
  label: string;
  title: string;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
  opensLauncher?: boolean;
}): ReactNode {
  return (
    <button
      type="button"
      title={title}
      className={danger ? 'danger' : undefined}
      disabled={disabled}
      data-opens-launcher={opensLauncher ? '' : undefined}
      onClick={onClick}
    >
      {label}
    </button>
  );
}
